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
	if got.QuestionsSoundID != "" || got.NextSoundID != "" {
		t.Fatalf("explicitly empty cue settings changed: %+v", got)
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

func TestNormalizeInvalidReminder(t *testing.T) {
	value := Default()
	value.ReminderMinutes = 0
	value.ReminderSeconds = 0

	got := normalize(value, Default())
	if got.ReminderMinutes != 2 || got.ReminderSeconds != 0 {
		t.Fatalf("unexpected normalized reminder: %+v", got)
	}
}
