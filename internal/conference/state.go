package conference

import (
	"errors"
	"sync"
	"time"
)

type Phase string

const (
	PhaseIdle             Phase = "idle"
	PhaseOpening          Phase = "opening"
	PhaseConnecting       Phase = "connecting"
	PhaseWaitingAdmission Phase = "waitingAdmission"
	PhaseJoined           Phase = "joined"
	PhasePlaying          Phase = "playing"
	PhaseLeft             Phase = "left"
	PhaseError            Phase = "error"
)

var (
	ErrAlreadyRunning = errors.New("участник ВКС уже запущен")
	ErrNotJoined      = errors.New("участник таймера ещё не подключён к ВКС")
	ErrSoundNotTested = errors.New("сначала выполните тест звука для ВКС")
)

type State struct {
	Phase          Phase  `json:"phase"`
	Platform       string `json:"platform"`
	DisplayURL     string `json:"displayUrl"`
	Message        string `json:"message"`
	Tested         bool   `json:"tested"`
	BrowserVisible bool   `json:"browserVisible"`
	UpdatedAt      int64  `json:"updatedAt"`
}

func newState() State {
	return State{Phase: PhaseIdle, UpdatedAt: time.Now().UnixMilli()}
}

type stateStore struct {
	mu       sync.RWMutex
	state    State
	onChange func(State)
}

func newStateStore(onChange func(State)) *stateStore {
	return &stateStore{state: newState(), onChange: onChange}
}

func (s *stateStore) get() State {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.state
}

func (s *stateStore) update(change func(*State)) {
	s.mu.Lock()
	change(&s.state)
	s.state.UpdatedAt = time.Now().UnixMilli()
	next := s.state
	callback := s.onChange
	s.mu.Unlock()

	if callback != nil {
		callback(next)
	}
}
