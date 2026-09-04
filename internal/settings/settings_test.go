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
