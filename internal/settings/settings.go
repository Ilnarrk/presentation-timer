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
	SoundID          string  `json:"soundId"`
	DeviceID         string  `json:"deviceId"`
	Volume           float64 `json:"volume"`
}

func Default() Settings {
	return Settings{
		TalkMinutes:      10,
		TalkSeconds:      0,
		QuestionsMinutes: 5,
		QuestionsSeconds: 0,
		SoundID:          "chime",
		DeviceID:         "default",
		Volume:           0.85,
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

func NewStore() (*Store, error) {
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
		settings: Default(),
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
	s.settings = settings
	return s.saveLocked()
}

func (s *Store) load() error {
	data, err := os.ReadFile(s.path)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, &s.settings)
}

func (s *Store) saveLocked() error {
	data, err := json.MarshalIndent(s.settings, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.path, data, 0o644)
}
