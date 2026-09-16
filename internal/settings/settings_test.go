package settings

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadOldJSONKeepsReminderDefault(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	if err := os.WriteFile(path, []byte(`{"talkMinutes":12,"soundId":"bell","questionsSoundId":"","nextSoundId":""}`), 0o600); err != nil {
		t.Fatal(err)
	}

	store := &Store{path: path, settings: Default()}
	if err := store.load(); err != nil {
		t.Fatal(err)
	}

	got := store.Get()
	if got.ReminderMinutes != 2 || got.ReminderSeconds != 0 {
		t.Fatalf("old settings did not receive reminder default: %+v", got)
	}
	if !got.MuteConferenceReceive {
		t.Fatalf("old settings did not receive muteConferenceReceive default: %+v", got)
	}
	if got.QuestionsSoundID != "" || got.NextSoundID != "" {
		t.Fatalf("explicitly empty cue settings changed: %+v", got)
	}
	if got.TimerScalePercent != DefaultTimerScalePercent {
		t.Fatalf("old settings did not receive timerScalePercent default: %+v", got)
	}
	if got.TimerDisplayMode != TimerDisplayModeRing || got.TimerFont != TimerFontSystem {
		t.Fatalf("old settings did not receive timer display defaults: %+v", got)
	}
	if got.WidgetPlacement != WidgetPlacementTopRight {
		t.Fatalf("old settings did not receive widgetPlacement default: %+v", got)
	}
}

func TestConferenceCameraEnabledRoundTrip(t *testing.T) {
	store := &Store{
		path:     filepath.Join(t.TempDir(), "settings.json"),
		settings: Default(),
	}
	input := Default()
	input.ConferenceCameraEnabled = true
	if err := store.Save(input); err != nil {
		t.Fatal(err)
	}
	got := store.Get()
	if !got.ConferenceCameraEnabled {
		t.Fatalf("expected conferenceCameraEnabled=true, got %+v", got)
	}
}

func TestMuteConferenceReceiveRoundTrip(t *testing.T) {
	store := &Store{
		path:     filepath.Join(t.TempDir(), "settings.json"),
		settings: Default(),
	}
	input := Default()
	input.MuteConferenceReceive = false
	if err := store.Save(input); err != nil {
		t.Fatal(err)
	}
	got := store.Get()
	if got.MuteConferenceReceive {
		t.Fatalf("expected muteConferenceReceive=false, got %+v", got)
	}
}

func TestLoadUsesProjectDefaultsOnlyForMissingFields(t *testing.T) {
	defaults := Default()
	defaults.SoundID = "embedded:alert.wav"
	defaults.QuestionsSoundID = "embedded:questions.wav"
	defaults.NextSoundID = "embedded:next.wav"

	tests := []struct {
		name string
		json string
		want Settings
	}{
		{
			name: "missing",
			json: `{"talkMinutes":10}`,
			want: defaults,
		},
		{
			name: "saved empty cues",
			json: `{"questionsSoundId":"","nextSoundId":""}`,
			want: func() Settings {
				s := defaults
				s.QuestionsSoundID = ""
				s.NextSoundID = ""
				return s
			}(),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "settings.json")
			if err := os.WriteFile(path, []byte(tt.json), 0o600); err != nil {
				t.Fatal(err)
			}
			store := &Store{path: path, settings: defaults}
			if err := store.load(); err != nil {
				t.Fatal(err)
			}
			got := store.Get()
			if got.SoundID != tt.want.SoundID ||
				got.QuestionsSoundID != tt.want.QuestionsSoundID ||
				got.NextSoundID != tt.want.NextSoundID {
				t.Fatalf("unexpected defaults: got %+v want %+v", got, tt.want)
			}
		})
	}
}

func TestKeepSessionPreservesTemplate(t *testing.T) {
	stored := Default()
	stored.SessionTotalMinutes = 60
	stored.SessionSpeakerCount = 3
	stored.SessionSpeakerNames = []string{"Иван", "Мария"}
	stored.SessionTalkMinutes = 8
	stored.SessionUseDefaultTalk = false
	stored.SessionUseDefaultQuestions = true
	stored.WidgetFreeX = 420
	stored.WidgetFreeY = 64
	stored.WidgetFreeWidth = 560
	stored.WidgetFreeHeight = 160

	input := Default()
	input.TalkMinutes = 12
	got := KeepSession(input, stored)
	if got.SessionTotalMinutes != 60 || got.SessionSpeakerCount != 3 || len(got.SessionSpeakerNames) != 2 {
		t.Fatalf("session template was not preserved: %+v", got)
	}
	if got.SessionTalkMinutes != 8 || got.SessionUseDefaultTalk || !got.SessionUseDefaultQuestions {
		t.Fatalf("session duration template was not preserved: %+v", got)
	}
	if got.TalkMinutes != 12 {
		t.Fatalf("non-session fields should stay: %+v", got)
	}
	if got.WidgetFreeX != 420 || got.WidgetFreeY != 64 || got.WidgetFreeWidth != 560 || got.WidgetFreeHeight != 160 {
		t.Fatalf("saved free widget bounds should stay: %+v", got)
	}
}

func TestLoadSessionTemplateFromJSON(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	payload := `{"talkMinutes":10,"sessionTotalMinutes":45,"sessionSpeakerCount":2,"sessionSpeakerNames":["Анна"]}`
	if err := os.WriteFile(path, []byte(payload), 0o600); err != nil {
		t.Fatal(err)
	}
	store := &Store{path: path, settings: Default()}
	if err := store.load(); err != nil {
		t.Fatal(err)
	}
	got := store.Get()
	if got.SessionTotalMinutes != 45 || got.SessionSpeakerCount != 2 || got.SessionSpeakerNames[0] != "Анна" {
		t.Fatalf("session template not loaded: %+v", got)
	}
}

func TestNormalizeInvalidReminder(t *testing.T) {
	value := Default()
	value.ReminderMinutes = 0
	value.ReminderSeconds = 0

	got := normalize(value, Default())
	if got.ReminderMinutes != 2 || got.ReminderSeconds != 0 {
		t.Fatalf("unexpected normalized reminder: %+v", got)
	}
}

func TestTimerScaleNormalization(t *testing.T) {
	value := Default()
	value.TimerScalePercent = 200
	got := normalize(value, Default())
	if got.TimerScalePercent != MaxTimerScalePercent {
		t.Fatalf("expected clamp to max, got %d", got.TimerScalePercent)
	}

	value.TimerScalePercent = 10
	got = normalize(value, Default())
	if got.TimerScalePercent != MinTimerScalePercent {
		t.Fatalf("expected clamp to min, got %d", got.TimerScalePercent)
	}
}

func TestWidgetPlacementRoundTrip(t *testing.T) {
	store := &Store{
		path:     filepath.Join(t.TempDir(), "settings.json"),
		settings: Default(),
	}
	input := Default()
	input.WidgetPlacement = WidgetPlacementTopLeft
	input.WidgetFreeX = 120
	input.WidgetFreeY = 80
	input.WidgetFreeWidth = 520
	input.WidgetFreeHeight = 156
	if err := store.Save(input); err != nil {
		t.Fatal(err)
	}
	got := store.Get()
	if got.WidgetPlacement != WidgetPlacementTopLeft || got.WidgetFreeX != 120 || got.WidgetFreeY != 80 || got.WidgetFreeWidth != 520 || got.WidgetFreeHeight != 156 {
		t.Fatalf("widget placement not saved: %+v", got)
	}
}

func TestWidgetAppearanceRoundTrip(t *testing.T) {
	store := &Store{
		path:     filepath.Join(t.TempDir(), "settings.json"),
		settings: Default(),
	}
	input := Default()
	input.WidgetTheme = WidgetThemeLight
	input.WidgetShape = WidgetShapeRectangular
	input.TimerDisplayMode = TimerDisplayModeDigital
	input.TimerFont = TimerFontDigital
	if err := store.Save(input); err != nil {
		t.Fatal(err)
	}
	got := store.Get()
	if got.WidgetTheme != WidgetThemeLight || got.WidgetShape != WidgetShapeRectangular || got.TimerDisplayMode != TimerDisplayModeDigital || got.TimerFont != TimerFontDigital {
		t.Fatalf("widget appearance not saved: %+v", got)
	}
}

func TestNormalizeWidgetAppearance(t *testing.T) {
	value := Default()
	value.WidgetTheme = "invalid"
	value.WidgetShape = "invalid"
	got := normalize(value, Default())
	if got.WidgetTheme != WidgetThemeDark || got.WidgetShape != WidgetShapeRounded {
		t.Fatalf("invalid widget appearance was not normalized: %+v", got)
	}

	value.WidgetShape = "square"
	if got = normalize(value, Default()); got.WidgetShape != WidgetShapeRectangular {
		t.Fatalf("legacy square shape was not migrated: %+v", got)
	}
}

func TestNormalizeLegacyVioletTheme(t *testing.T) {
	value := Default()
	value.WidgetTheme = "violet"
	got := normalize(value, Default())
	if got.WidgetTheme != WidgetThemeTransparent {
		t.Fatalf("legacy violet theme was not migrated: %+v", got)
	}
}

func TestNormalizeWidgetPlacement(t *testing.T) {
	if NormalizeWidgetPlacement("invalid") != WidgetPlacementTopRight {
		t.Fatal("invalid placement should default to topRight")
	}
	if NormalizeWidgetPlacement(WidgetPlacementFree) != WidgetPlacementFree {
		t.Fatal("free placement should stay free")
	}
	if NormalizeWidgetPlacement(WidgetPlacementTopCenter) != WidgetPlacementTopCenter {
		t.Fatal("topCenter placement should stay topCenter")
	}
}
