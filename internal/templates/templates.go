package templates

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/google/uuid"

	"timer/internal/session"
)

const MaxEntries = 10

var (
	ErrLimitReached = errors.New("можно сохранить не больше 10 шаблонов")
	ErrNotFound     = errors.New("шаблон не найден")
)

type Entry struct {
	ID       string           `json:"id"`
	Name     string           `json:"name"`
	Template session.Template `json:"template"`
	SavedAt  time.Time        `json:"savedAt"`
}

type Store struct {
	mu      sync.Mutex
	path    string
	entries []Entry
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
	store := &Store{path: filepath.Join(appDir, "session-templates.json")}
	if err := store.load(); err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	return store, nil
}

func NewMemoryStore() *Store {
	return &Store{}
}

func AutoName(tmpl session.Template) string {
	n := tmpl.Normalize()
	budget := n.BudgetSeconds()
	hours := budget / 3600
	minutes := (budget % 3600) / 60
	if hours > 0 {
		return fmt.Sprintf("%d ч %d мин · %d докл.", hours, minutes, n.SpeakerCount)
	}
	return fmt.Sprintf("%d мин · %d докл.", minutes, n.SpeakerCount)
}

func (s *Store) List() []Entry {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]Entry(nil), s.entries...)
}

func (s *Store) Save(tmpl session.Template) (Entry, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	tmpl = tmpl.Normalize()
	if !tmpl.Valid() {
		return Entry{}, session.ErrInvalidTemplate
	}

	now := time.Now().UTC()
	for i, entry := range s.entries {
		if templatesEqual(entry.Template, tmpl) {
			s.entries[i].SavedAt = now
			s.entries[i].Name = AutoName(tmpl)
			s.entries[i].Template = tmpl
			if err := s.saveLocked(); err != nil {
				return Entry{}, err
			}
			return s.entries[i], nil
		}
	}

	if len(s.entries) >= MaxEntries {
		return Entry{}, ErrLimitReached
	}

	entry := Entry{
		ID:       uuid.NewString(),
		Name:     AutoName(tmpl),
		Template: tmpl,
		SavedAt:  now,
	}
	s.entries = append(s.entries, entry)
	if err := s.saveLocked(); err != nil {
		return Entry{}, err
	}
	return entry, nil
}

func (s *Store) Delete(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	for i, entry := range s.entries {
		if entry.ID == id {
			s.entries = append(s.entries[:i], s.entries[i+1:]...)
			return s.saveLocked()
		}
	}
	return ErrNotFound
}

func (s *Store) Latest() (session.Template, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.entries) == 0 {
		return session.Template{}, false
	}
	latestIdx := 0
	for i, entry := range s.entries {
		if !entry.SavedAt.Before(s.entries[latestIdx].SavedAt) {
			latestIdx = i
		}
	}
	return s.entries[latestIdx].Template, true
}

func (s *Store) MigrateFromSettings(tmpl session.Template) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if len(s.entries) > 0 {
		return nil
	}
	tmpl = tmpl.Normalize()
	if !tmpl.Valid() {
		return nil
	}

	s.entries = []Entry{{
		ID:       uuid.NewString(),
		Name:     AutoName(tmpl),
		Template: tmpl,
		SavedAt:  time.Now().UTC(),
	}}
	return s.saveLocked()
}

func (s *Store) load() error {
	data, err := os.ReadFile(s.path)
	if err != nil {
		return err
	}
	var entries []Entry
	if err := json.Unmarshal(data, &entries); err != nil {
		return err
	}
	s.entries = entries
	return nil
}

func (s *Store) saveLocked() error {
	if s.path == "" {
		return nil
	}
	data, err := json.MarshalIndent(s.entries, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.path, data, 0o644)
}

func templatesEqual(a, b session.Template) bool {
	a = a.Normalize()
	b = b.Normalize()
	if a.TotalMinutes != b.TotalMinutes ||
		a.TotalSeconds != b.TotalSeconds ||
		a.SpeakerCount != b.SpeakerCount ||
		a.TalkMinutes != b.TalkMinutes ||
		a.TalkSeconds != b.TalkSeconds ||
		a.QuestionsMinutes != b.QuestionsMinutes ||
		a.QuestionsSeconds != b.QuestionsSeconds ||
		a.UseDefaultTalk != b.UseDefaultTalk ||
		a.UseDefaultQuestions != b.UseDefaultQuestions {
		return false
	}
	if len(a.SpeakerNames) != len(b.SpeakerNames) {
		return false
	}
	for i := range a.SpeakerNames {
		if a.SpeakerNames[i] != b.SpeakerNames[i] {
			return false
		}
	}
	return true
}
