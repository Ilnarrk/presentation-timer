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
	"timer/internal/session"
	"timer/internal/settings"
	"timer/internal/templates"
	"timer/internal/timer"
)

type App struct {
	ctx context.Context

	mu         sync.Mutex
	settings   *settings.Store
	templates  *templates.Store
	catalog    *audio.Catalog
	audio      *audio.Player
	engine     *timer.Engine
	session    *session.Tracker
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

	templateStore, err := templates.NewStore()
	if err != nil {
		runtime.LogErrorf(ctx, "session templates init failed: %v", err)
		templateStore = templates.NewMemoryStore()
	}
	if err := templateStore.MigrateFromSettings(sessionTemplateFromSettings(store.Get())); err != nil {
		runtime.LogErrorf(ctx, "session templates migration failed: %v", err)
	}
	a.templates = templateStore

	conference.SetMainWindowRaiseHandler(func() {
		runtime.WindowSetAlwaysOnTop(a.ctx, true)
	})
	a.conference = conference.NewController(func(state conference.State) {
		runtime.EventsEmit(a.ctx, "conference:state", state)
	})
	a.session = session.NewTracker()
	a.session.SetOnChange(func(state session.State) {
		runtime.EventsEmit(a.ctx, "session:state", state)
	})

	cfg := a.timerConfigFromSettings(store.Get())
	a.engine = timer.NewEngine(cfg)
	a.applyAudioSettings(store.Get())

	a.engine.SetCallbacks(
		func(snapshot timer.Snapshot) {
			runtime.EventsEmit(a.ctx, "timer:state", snapshot)
			if a.session != nil {
				a.session.Tick()
			}
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

	if a.settings != nil {
		input = settings.KeepSession(input, a.settings.Get())
	}
	if err := a.settings.Save(input); err != nil {
		return err
	}

	a.applyAudioSettings(input)
	if a.engine != nil && (a.session == nil || !a.session.Active()) {
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
	if visible {
		runtime.WindowSetAlwaysOnTop(a.ctx, true)
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

func (a *App) GetSessionTemplate() session.Template {
	if a.templates != nil {
		if tmpl, ok := a.templates.Latest(); ok {
			return tmpl
		}
	}
	s := a.GetSettings()
	return sessionTemplateFromSettings(s)
}

func (a *App) ListSessionTemplates() []templates.Entry {
	if a.templates == nil {
		return nil
	}
	return a.templates.List()
}

func (a *App) SaveSessionTemplate(tmpl session.Template) (templates.Entry, error) {
	tmpl = tmpl.Normalize()
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.templates == nil {
		return templates.Entry{}, nil
	}
	entry, err := a.templates.Save(tmpl)
	if err != nil {
		return templates.Entry{}, err
	}
	if a.settings != nil {
		current := a.settings.Get()
		applySessionTemplateToSettings(&current, tmpl)
		if err := a.settings.Save(current); err != nil {
			return templates.Entry{}, err
		}
	}
	return entry, nil
}

func (a *App) DeleteSessionTemplate(id string) error {
	if a.templates == nil {
		return templates.ErrNotFound
	}
	return a.templates.Delete(id)
}

func (a *App) GetSessionState() session.State {
	if a.session == nil {
		return session.State{}
	}
	return a.session.State()
}

func (a *App) CreateSession(tmpl session.Template) (session.State, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.session == nil {
		a.session = session.NewTracker()
	}
	if err := a.session.Create(tmpl); err != nil {
		return session.State{}, err
	}
	a.applySessionTimerConfig(tmpl)
	if a.engine != nil {
		a.engine.Reset()
	}
	return a.session.State(), nil
}

func (a *App) ResetSession() (session.State, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.session == nil {
		return session.State{}, session.ErrSessionInactive
	}
	if err := a.session.Reset(); err != nil {
		return a.session.State(), err
	}
	a.applySessionTimerConfig(a.session.Template())
	if a.engine != nil {
		a.engine.Reset()
	}
	return a.session.State(), nil
}

func (a *App) EndSession() (session.State, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.session == nil || !a.session.Active() {
		return session.State{}, session.ErrSessionInactive
	}
	a.session.Close()
	if a.engine != nil {
		a.engine.Reset()
		if a.settings != nil {
			a.engine.UpdateConfig(a.timerConfigFromSettings(a.settings.Get()))
		}
	}
	return a.session.State(), nil
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
	snap := a.engine.Snapshot()
	if err := a.engine.Start(); err != nil {
		return err
	}
	if a.session != nil && a.session.Active() {
		if snap.IsPaused {
			a.session.Resume()
		} else if snap.Phase == timer.PhaseIdle || snap.Phase == timer.PhaseCompleted {
			a.session.BeginTalk()
		}
	}
	return nil
}

func (a *App) Pause() {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.engine.Pause()
	if a.session != nil {
		a.session.Pause()
	}
}

func (a *App) Reset() {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.engine.Reset()
	if a.session != nil {
		a.session.StopSegment()
	}
}

func (a *App) GoToQuestions() error {
	a.mu.Lock()
	err := a.engine.GoToQuestions()
	if err != nil {
		a.mu.Unlock()
		return err
	}
	if a.session != nil {
		a.session.BeginQuestions()
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
	if a.session != nil {
		a.session.AdvanceSpeaker()
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
}

func (a *App) timerConfigFromSettings(s settings.Settings) timer.Config {
	return timer.Config{
		TalkDuration:      durationFromParts(s.TalkMinutes, s.TalkSeconds),
		QuestionsDuration: durationFromParts(s.QuestionsMinutes, s.QuestionsSeconds),
		ReminderInterval:  durationFromParts(s.ReminderMinutes, s.ReminderSeconds),
	}
}

func (a *App) applySessionTimerConfig(tmpl session.Template) {
	if a.engine == nil || a.settings == nil {
		return
	}
	s := a.settings.Get()
	talkMin, talkSec, qMin, qSec := tmpl.ResolveDurations(s)
	a.engine.UpdateConfig(timer.Config{
		TalkDuration:      durationFromParts(talkMin, talkSec),
		QuestionsDuration: durationFromParts(qMin, qSec),
		ReminderInterval:  durationFromParts(s.ReminderMinutes, s.ReminderSeconds),
	})
}

func sessionTemplateFromSettings(s settings.Settings) session.Template {
	return session.Template{
		TotalMinutes:        s.SessionTotalMinutes,
		TotalSeconds:        s.SessionTotalSeconds,
		SpeakerCount:        s.SessionSpeakerCount,
		SpeakerNames:        append([]string(nil), s.SessionSpeakerNames...),
		TalkMinutes:         s.SessionTalkMinutes,
		TalkSeconds:         s.SessionTalkSeconds,
		QuestionsMinutes:    s.SessionQuestionsMinutes,
		QuestionsSeconds:    s.SessionQuestionsSeconds,
		UseDefaultTalk:      s.SessionUseDefaultTalk,
		UseDefaultQuestions: s.SessionUseDefaultQuestions,
	}.Normalize()
}

func applySessionTemplateToSettings(s *settings.Settings, tmpl session.Template) {
	tmpl = tmpl.Normalize()
	s.SessionTotalMinutes = tmpl.TotalMinutes
	s.SessionTotalSeconds = tmpl.TotalSeconds
	s.SessionSpeakerCount = tmpl.SpeakerCount
	s.SessionSpeakerNames = tmpl.SpeakerNames
	s.SessionTalkMinutes = tmpl.TalkMinutes
	s.SessionTalkSeconds = tmpl.TalkSeconds
	s.SessionQuestionsMinutes = tmpl.QuestionsMinutes
	s.SessionQuestionsSeconds = tmpl.QuestionsSeconds
	s.SessionUseDefaultTalk = tmpl.UseDefaultTalk
	s.SessionUseDefaultQuestions = tmpl.UseDefaultQuestions
}

func durationFromParts(minutes, seconds int) time.Duration {
	return time.Duration(minutes)*time.Minute + time.Duration(seconds)*time.Second
}

func (a *App) applyAudioSettings(s settings.Settings) {
	a.audio.SetDevice(s.DeviceID)
	a.audio.SetVolume(s.Volume)
}

func (a *App) handleAlert(event timer.AlertEvent) {
	a.mu.Lock()
	s := a.settings.Get()
	soundID := s.SoundID
	if event.Repeated && s.ReminderSoundID != "" {
		soundID = s.ReminderSoundID
	}
	player := a.audio
	conferenceController := a.conference
	volume := s.Volume
	conferenceConnected := conferenceController != nil && conferenceController.IsConnected()
	playLocal, playConference := alertPlaybackTargets(s, conferenceConnected)
	a.mu.Unlock()

	runtime.LogInfof(a.ctx, "alert: playLocal=%v mute=%v conference=%v sound=%s",
		playLocal, s.MuteConferenceSound, conferenceConnected, soundID)

	if playLocal {
		if err := player.Play(soundID); err != nil {
			runtime.LogErrorf(a.ctx, "local alert playback failed: %v", err)
			runtime.EventsEmit(a.ctx, "audio:error", err.Error())
		}
	} else if s.MuteConferenceSound {
		runtime.EventsEmit(a.ctx, "audio:muted", "Локальный звук отключён в настройках")
	}

	if playConference {
		go func() {
			wav, err := a.catalog.Render(soundID, volume)
			if err != nil {
				runtime.LogErrorf(a.ctx, "conference sound rendering failed: %v", err)
				return
			}
			if err := conferenceController.PlaySound(wav); err != nil {
				runtime.LogErrorf(a.ctx, "conference audio playback failed: %v", err)
			}
		}()
	}

	if runtime.WindowIsMinimised(a.ctx) {
		runtime.WindowUnminimise(a.ctx)
	}
	runtime.WindowSetAlwaysOnTop(a.ctx, true)
}

func (a *App) playConferenceCue(soundID string) {
	if soundID == "" {
		return
	}

	s := a.settings.Get()
	conferenceConnected := a.conference != nil && a.conference.IsConnected()
	playLocal, playConference := alertPlaybackTargets(s, conferenceConnected)

	if playLocal {
		if err := a.audio.Play(soundID); err != nil {
			runtime.LogErrorf(a.ctx, "local cue playback failed: %v", err)
			runtime.EventsEmit(a.ctx, "audio:error", err.Error())
		}
	}

	if playConference {
		go func() {
			wav, err := a.catalog.Render(soundID, s.Volume)
			if err != nil {
				runtime.LogErrorf(a.ctx, "conference cue rendering failed: %v", err)
				return
			}
			if err := a.conference.PlaySound(wav); err != nil {
				runtime.LogErrorf(a.ctx, "conference cue playback failed: %v", err)
			}
		}()
	}
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
