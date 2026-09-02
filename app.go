package main

import (
	"context"
	"sync"
	"time"

	"github.com/wailsapp/wails/v2/pkg/runtime"

	"timer/internal/audio"
	"timer/internal/conference"
	"timer/internal/settings"
	"timer/internal/timer"
)

type App struct {
	ctx context.Context

	mu         sync.Mutex
	settings   *settings.Store
	audio      *audio.Player
	engine     *timer.Engine
	conference *conference.Controller
}

func NewApp() *App {
	return &App{
		audio: audio.NewPlayer(),
	}
}

func (a *App) startup(ctx context.Context) {
	a.ctx = ctx

	store, err := settings.NewStore()
	if err != nil {
		runtime.LogErrorf(ctx, "settings init failed: %v", err)
		store = settings.NewMemoryStore()
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
			a.handleAlert()
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
	return audio.ListSounds()
}

func (a *App) PreviewSound(soundID string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.audio.Preview(soundID)
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

func (a *App) TestConferenceSound(soundID string) error {
	if a.conference == nil {
		return conference.ErrNotJoined
	}
	s := a.settings.Get()
	if soundID == "" {
		soundID = s.SoundID
	}
	wav, err := audio.RenderSound(soundID, s.Volume)
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
	defer a.mu.Unlock()
	return a.engine.GoToQuestions()
}

func (a *App) NextSpeaker() error {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.engine.NextSpeaker()
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
	}
}

func durationFromParts(minutes, seconds int) time.Duration {
	return time.Duration(minutes)*time.Minute + time.Duration(seconds)*time.Second
}

func (a *App) applyAudioSettings(s settings.Settings) {
	a.audio.SetDevice(s.DeviceID)
	a.audio.SetVolume(s.Volume)
}

func (a *App) handleAlert() {
	go func() {
		a.mu.Lock()
		settings := a.settings.Get()
		soundID := settings.SoundID
		player := a.audio
		conferenceController := a.conference
		a.mu.Unlock()

		if conferenceController != nil && conferenceController.IsConnected() {
			wav, err := audio.RenderSound(soundID, settings.Volume)
			if err != nil {
				runtime.LogErrorf(a.ctx, "conference sound rendering failed: %v", err)
			} else if err := conferenceController.PlaySound(wav); err != nil {
				runtime.LogErrorf(a.ctx, "conference audio playback failed: %v", err)
			}
		}
		if err := player.Play(soundID); err != nil {
			runtime.LogErrorf(a.ctx, "audio playback failed: %v", err)
		}
	}()

	runtime.WindowUnminimise(a.ctx)
	runtime.WindowShow(a.ctx)
	runtime.WindowSetAlwaysOnTop(a.ctx, true)
}
