package main

import (
	"context"
	"io/fs"
	"sync"
	"time"

	"github.com/wailsapp/wails/v2/pkg/runtime"

	"timer/internal/audio"
	"timer/internal/buildinfo"
	"timer/internal/conference"
	"timer/internal/settings"
	"timer/internal/timer"
)

type App struct {
	ctx context.Context

	mu         sync.Mutex
	settings   *settings.Store
	catalog    *audio.Catalog
	audio      *audio.Player
	engine     *timer.Engine
	conference *conference.Controller
	projectFS  fs.FS
}

func NewApp(projectSounds ...fs.FS) *App {
	app := &App{}
	if len(projectSounds) > 0 {
		app.projectFS = projectSounds[0]
	}
	return app
}

func (a *App) startup(ctx context.Context) {
	a.ctx = ctx

	catalog, err := audio.NewCatalog(a.projectFS)
	if err != nil {
		runtime.LogErrorf(ctx, "sound catalog init failed: %v", err)
		catalog = audio.NewMemoryCatalog(a.projectFS)
	}
	a.catalog = catalog
	a.audio = audio.NewPlayer(catalog)

	defaults := settings.Default()
	soundDefaults := catalog.Defaults()
	if soundDefaults.AlertID != "" {
		defaults.SoundID = soundDefaults.AlertID
	}
	defaults.QuestionsSoundID = soundDefaults.QuestionsID
	defaults.NextSoundID = soundDefaults.NextID

	store, err := settings.NewStoreWithDefaults(defaults)
	if err != nil {
		runtime.LogErrorf(ctx, "settings init failed: %v", err)
		store = settings.NewMemoryStoreWithDefaults(defaults)
	}
	a.settings = store
	a.conference = conference.NewController(func(state conference.State) {
		runtime.EventsEmit(a.ctx, "conference:state", state)
	})

	cfg := a.timerConfigFromSettings(store.Get())
	a.engine = timer.NewEngine(cfg)
	a.applyAudioSettings(store.Get())

	a.engine.SetCallbacks(
		func(snapshot timer.Snapshot) {
			runtime.EventsEmit(a.ctx, "timer:state", snapshot)
		},
		func(event timer.AlertEvent) {
			runtime.EventsEmit(a.ctx, "timer:alert", event)
			a.handleAlert(event)
		},
	)

	runtime.EventsEmit(ctx, "timer:state", a.engine.Snapshot())
}

func (a *App) shutdown(ctx context.Context) {
	if a.engine != nil {
		a.engine.Stop()
	}
	if a.conference != nil {
		a.conference.Disconnect()
	}
}

func (a *App) GetAppInfo() buildinfo.Info {
	return buildinfo.Get()
}

func (a *App) GetSettings() settings.Settings {
	if a.settings == nil {
		return settings.Default()
	}
	return a.settings.Get()
}

func (a *App) SaveSettings(input settings.Settings) error {
	if input.TalkMinutes < 0 || input.TalkSeconds < 0 || input.QuestionsMinutes < 0 || input.QuestionsSeconds < 0 {
		return timer.ErrInvalidDuration
	}
	if input.ReminderMinutes < 0 || input.ReminderSeconds < 0 ||
		input.ReminderMinutes == 0 && input.ReminderSeconds == 0 {
		return timer.ErrInvalidDuration
	}
	if input.Volume < 0 {
		input.Volume = 0
	}
	if input.Volume > 1 {
		input.Volume = 1
	}
	if input.SoundID == "" {
		input.SoundID = "chime"
	}
	if input.DeviceID == "" {
		input.DeviceID = "default"
	}

	a.mu.Lock()
	defer a.mu.Unlock()

	if err := a.settings.Save(input); err != nil {
		return err
	}

	a.applyAudioSettings(input)
	if a.engine != nil {
		a.engine.UpdateConfig(a.timerConfigFromSettings(input))
	}
	return nil
}

func (a *App) GetAudioDevices() ([]audio.Device, error) {
	return audio.ListDevices()
}

func (a *App) GetSounds() []audio.Sound {
	if a.catalog == nil {
		return audio.NewMemoryCatalog(a.projectFS).ListSounds()
	}
	return a.catalog.ListSounds()
}

func (a *App) PreviewSound(soundID string) error {
	return a.audio.Preview(soundID)
}

func (a *App) ImportSound() (audio.Sound, error) {
	path, err := runtime.OpenFileDialog(a.ctx, runtime.OpenDialogOptions{
		Title: "Импорт звука",
		Filters: []runtime.FileFilter{
			{DisplayName: "Аудиофайлы WAV, MP3, OGG", Pattern: "*.wav;*.mp3;*.ogg"},
		},
	})
	if err != nil || path == "" {
		return audio.Sound{}, err
	}
	return a.catalog.ImportFile(path)
}

func (a *App) GetState() timer.Snapshot {
	if a.engine == nil {
		return timer.Snapshot{Phase: timer.PhaseIdle}
	}
	return a.engine.Snapshot()
}

func (a *App) GetConferenceState() conference.State {
	if a.conference == nil {
		return conference.State{Phase: conference.PhaseIdle}
	}
	return a.conference.GetState()
}

func (a *App) GetConferencePlatforms() []conference.Platform {
	return conference.SupportedPlatforms()
}

func (a *App) ConnectConference(url, displayName string) (conference.State, error) {
	if a.conference == nil {
		return conference.State{}, context.Canceled
	}
	return a.conference.Connect(url, displayName)
}

func (a *App) DisconnectConference() {
	if a.conference != nil {
		a.conference.Disconnect()
	}
}

func (a *App) ConfirmConferenceJoined() error {
	if a.conference == nil {
		return conference.ErrNotJoined
	}
	return a.conference.ConfirmJoined()
}

func (a *App) SetConferenceBrowserVisible(visible bool) (conference.State, error) {
	if a.conference == nil {
		return conference.State{Phase: conference.PhaseIdle}, conference.ErrNotJoined
	}
	if err := a.conference.SetBrowserWindowVisible(visible); err != nil {
		return a.conference.GetState(), err
	}
	return a.conference.GetState(), nil
}

func (a *App) GetConferenceDiagnostics() (string, error) {
	if a.conference == nil {
		return "", conference.ErrNotJoined
	}
	return a.conference.GetDiagnostics()
}

func (a *App) TestConferenceSound(soundID string) error {
	if a.conference == nil {
		return conference.ErrNotJoined
	}
	s := a.settings.Get()
	if soundID == "" {
		soundID = s.SoundID
	}
	wav, err := a.catalog.Render(soundID, s.Volume)
	if err != nil {
		return err
	}
	return a.conference.TestSound(wav)
}

func (a *App) Start() error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.conference != nil {
		state := a.conference.GetState()
		active := state.Phase == conference.PhaseOpening ||
			state.Phase == conference.PhaseConnecting ||
			state.Phase == conference.PhaseWaitingAdmission ||
			state.Phase == conference.PhaseJoined ||
			state.Phase == conference.PhasePlaying
		if active && !a.conference.IsReady() {
			return conference.ErrSoundNotTested
		}
	}
	return a.engine.Start()
}

func (a *App) Pause() {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.engine.Pause()
}

func (a *App) Reset() {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.engine.Reset()
}

func (a *App) GoToQuestions() error {
	a.mu.Lock()
	err := a.engine.GoToQuestions()
	if err != nil {
		a.mu.Unlock()
		return err
	}
	soundID := a.settings.Get().QuestionsSoundID
	a.mu.Unlock()
	a.playConferenceCue(soundID)
	return nil
}

func (a *App) NextSpeaker() error {
	a.mu.Lock()
	err := a.engine.NextSpeaker()
	if err != nil {
		a.mu.Unlock()
		return err
	}
	soundID := a.settings.Get().NextSoundID
	a.mu.Unlock()
	a.playConferenceCue(soundID)
	return nil
}

func (a *App) DismissAlert() {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.engine.DismissAlert()
	runtime.WindowSetAlwaysOnTop(a.ctx, false)
}

func (a *App) timerConfigFromSettings(s settings.Settings) timer.Config {
	return timer.Config{
		TalkDuration:      durationFromParts(s.TalkMinutes, s.TalkSeconds),
		QuestionsDuration: durationFromParts(s.QuestionsMinutes, s.QuestionsSeconds),
		ReminderInterval:  durationFromParts(s.ReminderMinutes, s.ReminderSeconds),
	}
}

func durationFromParts(minutes, seconds int) time.Duration {
	return time.Duration(minutes)*time.Minute + time.Duration(seconds)*time.Second
}

func (a *App) applyAudioSettings(s settings.Settings) {
	a.audio.SetDevice(s.DeviceID)
	a.audio.SetVolume(s.Volume)
}

func (a *App) handleAlert(event timer.AlertEvent) {
	go func() {
		a.mu.Lock()
		s := a.settings.Get()
		soundID := s.SoundID
		if event.Repeated && s.ReminderSoundID != "" {
			soundID = s.ReminderSoundID
		}
		player := a.audio
		conferenceController := a.conference
		conferenceConnected := conferenceController != nil && conferenceController.IsConnected()
		playLocal, playConference := alertPlaybackTargets(s, conferenceConnected)
		a.mu.Unlock()

		if playConference {
			wav, err := a.catalog.Render(soundID, s.Volume)
			if err != nil {
				runtime.LogErrorf(a.ctx, "conference sound rendering failed: %v", err)
			} else if err := conferenceController.PlaySound(wav); err != nil {
				runtime.LogErrorf(a.ctx, "conference audio playback failed: %v", err)
			}
		}
		if playLocal {
			if err := player.Play(soundID); err != nil {
				runtime.LogErrorf(a.ctx, "audio playback failed: %v", err)
			}
		}
	}()

	runtime.WindowUnminimise(a.ctx)
	runtime.WindowShow(a.ctx)
	runtime.WindowSetAlwaysOnTop(a.ctx, true)
}

func (a *App) playConferenceCue(soundID string) {
	if soundID == "" || a.conference == nil || !a.conference.IsConnected() {
		return
	}
	s := a.settings.Get()
	wav, err := a.catalog.Render(soundID, s.Volume)
	if err != nil {
		runtime.LogErrorf(a.ctx, "conference cue rendering failed: %v", err)
		return
	}
	go func() {
		if err := a.conference.PlaySound(wav); err != nil {
			runtime.LogErrorf(a.ctx, "conference cue playback failed: %v", err)
		}
	}()
}

// alertPlaybackTargets decides where an alert should play.
// MuteConferenceSound silences the local speakers so the moderator does not
// hear a duplicate of the conference participant's audio.
func alertPlaybackTargets(s settings.Settings, conferenceConnected bool) (playLocal, playConference bool) {
	return shouldPlayLocalSound(s), conferenceConnected
}

func shouldPlayLocalSound(s settings.Settings) bool {
	return !s.MuteConferenceSound
}
