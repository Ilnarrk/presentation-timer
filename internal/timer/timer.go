package timer

import (
	"sync"
	"time"
)

const DefaultReminderInterval = 2 * time.Minute

type Phase string

const (
	PhaseIdle              Phase = "idle"
	PhaseTalk              Phase = "talk"
	PhaseTalkOvertime      Phase = "talkOvertime"
	PhaseQuestions         Phase = "questions"
	PhaseQuestionsOvertime Phase = "questionsOvertime"
	PhaseCompleted         Phase = "completed"
)

type Config struct {
	TalkDuration      time.Duration
	QuestionsDuration time.Duration
	ReminderInterval  time.Duration
}

type Snapshot struct {
	Phase            Phase `json:"phase"`
	IsRunning        bool  `json:"isRunning"`
	IsPaused         bool  `json:"isPaused"`
	RemainingSeconds int   `json:"remainingSeconds"`
	OvertimeSeconds  int   `json:"overtimeSeconds"`
	TalkSeconds      int   `json:"talkSeconds"`
	QuestionsSeconds int   `json:"questionsSeconds"`
	NextReminderIn   int   `json:"nextReminderIn"`
	AlertActive      bool  `json:"alertActive"`
}

type AlertEvent struct {
	Phase    Phase `json:"phase"`
	Repeated bool  `json:"repeated"`
}

type Engine struct {
	mu sync.Mutex

	clock Clock
	cfg   Config

	phase         Phase
	isRunning     bool
	isPaused      bool
	deadline      time.Time
	pausedLeft    time.Duration
	lastAlertAt   time.Time
	alertActive   bool
	reminderDueAt time.Time

	onStateChange func(Snapshot)
	onAlert       func(AlertEvent)

	stopCh   chan struct{}
	ticker   *time.Ticker
	tickerMu sync.Mutex
}

func NewEngine(cfg Config) *Engine {
	return &Engine{
		clock:  realClock{},
		cfg:    normalizeConfig(cfg),
		phase:  PhaseIdle,
		stopCh: make(chan struct{}),
	}
}

func NewEngineWithClock(cfg Config, clock Clock) *Engine {
	e := NewEngine(cfg)
	e.clock = clock
	return e
}

func (e *Engine) SetCallbacks(onState func(Snapshot), onAlert func(AlertEvent)) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.onStateChange = onState
	e.onAlert = onAlert
}

func (e *Engine) UpdateConfig(cfg Config) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.cfg = normalizeConfig(cfg)
	if e.isOvertimePhaseLocked() && !e.lastAlertAt.IsZero() {
		e.reminderDueAt = e.lastAlertAt.Add(e.cfg.ReminderInterval)
	}
	e.emitLocked()
}

func (e *Engine) Snapshot() Snapshot {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.snapshotLocked()
}

func (e *Engine) Start() error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.cfg.TalkDuration <= 0 {
		return ErrInvalidDuration
	}
	if e.isRunning && !e.isPaused {
		return nil
	}

	if e.isPaused {
		e.isPaused = false
		if e.pausedLeft > 0 {
			e.deadline = e.clock.Now().Add(e.pausedLeft)
		}
		e.pausedLeft = 0
		e.ensureTickerLocked()
		e.emitLocked()
		return nil
	}

	e.phase = PhaseTalk
	e.isRunning = true
	e.isPaused = false
	e.deadline = e.clock.Now().Add(e.cfg.TalkDuration)
	e.lastAlertAt = time.Time{}
	e.alertActive = false
	e.reminderDueAt = time.Time{}
	e.ensureTickerLocked()
	e.emitLocked()
	return nil
}

func (e *Engine) Pause() {
	e.mu.Lock()
	defer e.mu.Unlock()

	if !e.isRunning || e.isPaused {
		return
	}

	now := e.clock.Now()
	if e.isOvertimePhaseLocked() {
		e.pausedLeft = 0
	} else if !e.deadline.IsZero() {
		e.pausedLeft = e.deadline.Sub(now)
		if e.pausedLeft < 0 {
			e.pausedLeft = 0
		}
	}
	e.isPaused = true
	e.stopTickerLocked()
	e.emitLocked()
}

func (e *Engine) Reset() {
	e.mu.Lock()
	defer e.mu.Unlock()

	e.phase = PhaseIdle
	e.isRunning = false
	e.isPaused = false
	e.deadline = time.Time{}
	e.pausedLeft = 0
	e.lastAlertAt = time.Time{}
	e.alertActive = false
	e.reminderDueAt = time.Time{}
	e.stopTickerLocked()
	e.emitLocked()
}

func (e *Engine) GoToQuestions() error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.phase != PhaseTalk && e.phase != PhaseTalkOvertime {
		return ErrInvalidTransition
	}
	if e.cfg.QuestionsDuration <= 0 {
		return ErrInvalidDuration
	}

	e.phase = PhaseQuestions
	e.isRunning = true
	e.isPaused = false
	e.deadline = e.clock.Now().Add(e.cfg.QuestionsDuration)
	e.lastAlertAt = time.Time{}
	e.alertActive = false
	e.reminderDueAt = time.Time{}
	e.ensureTickerLocked()
	e.emitLocked()
	return nil
}

func (e *Engine) NextSpeaker() error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.phase != PhaseQuestions && e.phase != PhaseQuestionsOvertime {
		return ErrInvalidTransition
	}
	if e.cfg.TalkDuration <= 0 {
		return ErrInvalidDuration
	}

	e.phase = PhaseTalk
	e.isRunning = true
	e.isPaused = false
	e.deadline = e.clock.Now().Add(e.cfg.TalkDuration)
	e.lastAlertAt = time.Time{}
	e.alertActive = false
	e.reminderDueAt = time.Time{}
	e.ensureTickerLocked()
	e.emitLocked()
	return nil
}

func (e *Engine) DismissAlert() {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.alertActive = false
	e.emitLocked()
}

func (e *Engine) Stop() {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.stopTickerLocked()
	close(e.stopCh)
}

func (e *Engine) tick() {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.evaluateLocked()
	e.emitLocked()
}

func (e *Engine) evaluateLocked() {
	if !e.isRunning || e.isPaused {
		return
	}

	now := e.clock.Now()

	switch e.phase {
	case PhaseTalk:
		if !e.deadline.IsZero() && !now.Before(e.deadline) {
			e.enterOvertimeLocked(PhaseTalkOvertime, now)
		}
	case PhaseQuestions:
		if !e.deadline.IsZero() && !now.Before(e.deadline) {
			e.enterOvertimeLocked(PhaseQuestionsOvertime, now)
		}
	case PhaseTalkOvertime, PhaseQuestionsOvertime:
		if e.reminderDueAt.IsZero() {
			return
		}
		if !now.Before(e.reminderDueAt) {
			e.fireAlertLocked(now, true)
		}
	}
}

func (e *Engine) enterOvertimeLocked(overtime Phase, now time.Time) {
	e.phase = overtime
	e.fireAlertLocked(now, false)
}

func (e *Engine) fireAlertLocked(now time.Time, repeated bool) {
	e.lastAlertAt = now
	e.reminderDueAt = now.Add(e.cfg.ReminderInterval)
	e.alertActive = true
	if e.onAlert != nil {
		event := AlertEvent{Phase: e.phase, Repeated: repeated}
		e.mu.Unlock()
		e.onAlert(event)
		e.mu.Lock()
	}
}

func normalizeConfig(cfg Config) Config {
	if cfg.ReminderInterval <= 0 {
		cfg.ReminderInterval = DefaultReminderInterval
	}
	return cfg
}

func (e *Engine) isOvertimePhaseLocked() bool {
	return e.phase == PhaseTalkOvertime || e.phase == PhaseQuestionsOvertime
}

func (e *Engine) snapshotLocked() Snapshot {
	remaining, overtime := e.computeTimesLocked()
	nextReminder := 0
	if e.isOvertimePhaseLocked() && !e.reminderDueAt.IsZero() && e.isRunning && !e.isPaused {
		delta := e.reminderDueAt.Sub(e.clock.Now())
		if delta > 0 {
			nextReminder = int(delta.Round(time.Second) / time.Second)
		}
	}

	return Snapshot{
		Phase:            e.phase,
		IsRunning:        e.isRunning,
		IsPaused:         e.isPaused,
		RemainingSeconds: remaining,
		OvertimeSeconds:  overtime,
		TalkSeconds:      int(e.cfg.TalkDuration / time.Second),
		QuestionsSeconds: int(e.cfg.QuestionsDuration / time.Second),
		NextReminderIn:   nextReminder,
		AlertActive:      e.alertActive,
	}
}

func (e *Engine) computeTimesLocked() (remaining int, overtime int) {
	if !e.isRunning {
		return int(e.cfg.TalkDuration / time.Second), 0
	}

	if e.isPaused {
		if e.isOvertimePhaseLocked() {
			return 0, 0
		}
		if e.pausedLeft > 0 {
			return int(e.pausedLeft.Round(time.Second) / time.Second), 0
		}
		return 0, 0
	}

	now := e.clock.Now()
	if e.isOvertimePhaseLocked() {
		if e.lastAlertAt.IsZero() {
			return 0, 0
		}
		elapsed := now.Sub(e.lastAlertAt)
		if elapsed < 0 {
			elapsed = 0
		}
		return 0, int(elapsed.Round(time.Second) / time.Second)
	}

	if e.deadline.IsZero() {
		return 0, 0
	}

	delta := e.deadline.Sub(now)
	seconds := int(delta.Round(time.Second) / time.Second)
	if seconds >= 0 {
		return seconds, 0
	}
	return 0, -seconds
}

func (e *Engine) emitLocked() {
	if e.onStateChange != nil {
		snap := e.snapshotLocked()
		e.mu.Unlock()
		e.onStateChange(snap)
		e.mu.Lock()
	}
}

func (e *Engine) ensureTickerLocked() {
	e.tickerMu.Lock()
	defer e.tickerMu.Unlock()

	if e.ticker != nil {
		return
	}

	e.ticker = time.NewTicker(200 * time.Millisecond)
	go func() {
		for {
			select {
			case <-e.stopCh:
				return
			case <-e.ticker.C:
				e.tick()
			}
		}
	}()
}

func (e *Engine) stopTickerLocked() {
	e.tickerMu.Lock()
	defer e.tickerMu.Unlock()
	if e.ticker != nil {
		e.ticker.Stop()
		e.ticker = nil
	}
}
