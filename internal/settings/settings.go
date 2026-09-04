package settings

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
)

type Settings struct {
	TalkMinutes      int     `json:"talkMinutes"`
	TalkSeconds      int     `json:"talkSeconds"`
	QuestionsMinutes int     `json:"questionsMinutes"`
	QuestionsSeconds int     `json:"questionsSeconds"`
	ReminderMinutes  int     `json:"reminderMinutes"`
	ReminderSeconds  int     `json:"reminderSeconds"`
	SoundID             string  `json:"soundId"`
	ReminderSoundID     string  `json:"reminderSoundId"`
	QuestionsSoundID    string  `json:"questionsSoundId"`
	NextSoundID         string  `json:"nextSoundId"`
	DeviceID            string  `json:"deviceId"`
	Volume              float64 `json:"volume"`
	MuteConferenceSound bool    `json:"muteConferenceSound"`
}

func Default() Settings {
	return Settings{
		TalkMinutes:      10,
		TalkSeconds:      0,
		QuestionsMinutes: 5,
		QuestionsSeconds: 0,
		ReminderMinutes:  2,
		ReminderSeconds:  0,
		SoundID:             "chime",
		ReminderSoundID:     "",
		QuestionsSoundID:    "",
		NextSoundID:         "",
		DeviceID:            "default",
		Volume:              0.85,
		MuteConferenceSound: false,
	}
}

type Store struct {
	mu       sync.RWMutex
	path     string
	settings Settings
}

func NewMemoryStore() *Store {
	return &Store{settings: Default()}
}

func NewMemoryStoreWithDefaults(defaults Settings) *Store {
	return &Store{settings: normalize(defaults, defaults)}
}

func NewStore() (*Store, error) {
	return NewStoreWithDefaults(Default())
}

// NewStoreWithDefaults loads saved values over defaults. Unmarshalling into
// the pre-populated value is intentional: fields absent from older JSON files
// retain current defaults, while explicitly saved empty sound IDs stay empty.
func NewStoreWithDefaults(defaults Settings) (*Store, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return nil, err
	}

	appDir := filepath.Join(dir, "presentation-timer")
	if err := os.MkdirAll(appDir, 0o755); err != nil {
		return nil, err
	}

	store := &Store{
		path:     filepath.Join(appDir, "settings.json"),
		settings: normalize(defaults, Default()),
	}

	if err := store.load(); err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}

	return store, nil
}

func (s *Store) Get() Settings {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.settings
}

func (s *Store) Save(settings Settings) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.settings = normalize(settings, Default())
	return s.saveLocked()
}

func (s *Store) load() error {
	data, err := os.ReadFile(s.path)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(data, &s.settings); err != nil {
		return err
	}
	s.settings = normalize(s.settings, Default())
	return nil
}

func normalize(value, fallback Settings) Settings {
	if value.ReminderMinutes < 0 || value.ReminderSeconds < 0 ||
		value.ReminderMinutes == 0 && value.ReminderSeconds == 0 {
		value.ReminderMinutes = fallback.ReminderMinutes
		value.ReminderSeconds = fallback.ReminderSeconds
		if value.ReminderMinutes == 0 && value.ReminderSeconds == 0 {
			value.ReminderMinutes = Default().ReminderMinutes
			value.ReminderSeconds = Default().ReminderSeconds
		}
	}
	if value.DeviceID == "" {
		value.DeviceID = "default"
	}
	if value.SoundID == "" {
		value.SoundID = fallback.SoundID
		if value.SoundID == "" {
			value.SoundID = "chime"
		}
	}
	if value.Volume < 0 {
		value.Volume = 0
	}
	if value.Volume > 1 {
		value.Volume = 1
	}
	return value
}

func (s *Store) saveLocked() error {
	data, err := json.MarshalIndent(s.settings, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.path, data, 0o644)
}
