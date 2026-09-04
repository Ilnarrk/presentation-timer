package session

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"timer/internal/settings"
)

const (
	MaxSpeakerCount = 50
	MaxNameLength   = 80
)

type SpeakerStatus string

const (
	StatusPending SpeakerStatus = "pending"
	StatusActive  SpeakerStatus = "active"
	StatusDone    SpeakerStatus = "done"
)

type segment int

const (
	segmentNone segment = iota
	segmentTalk
	segmentQuestions
)

var (
	ErrInvalidTemplate = errors.New("укажите общее время и количество докладчиков")
	ErrSessionInactive = errors.New("сессия не создана")
)

type Template struct {
	TotalMinutes        int      `json:"totalMinutes"`
	TotalSeconds        int      `json:"totalSeconds"`
	SpeakerCount        int      `json:"speakerCount"`
	SpeakerNames        []string `json:"speakerNames"`
	TalkMinutes         int      `json:"talkMinutes"`
	TalkSeconds         int      `json:"talkSeconds"`
	QuestionsMinutes    int      `json:"questionsMinutes"`
	QuestionsSeconds    int      `json:"questionsSeconds"`
	UseDefaultTalk      bool     `json:"useDefaultTalk"`
	UseDefaultQuestions bool     `json:"useDefaultQuestions"`
}

func (t Template) BudgetSeconds() int {
	return t.TotalMinutes*60 + t.TotalSeconds
}

func (t Template) Normalize() Template {
	if t.TotalMinutes < 0 {
		t.TotalMinutes = 0
	}
	if t.TotalSeconds < 0 {
		t.TotalSeconds = 0
	}
	if t.TotalSeconds > 59 {
		t.TotalMinutes += t.TotalSeconds / 60
		t.TotalSeconds %= 60
	}
	if t.SpeakerCount < 0 {
		t.SpeakerCount = 0
	}
	if t.SpeakerCount > MaxSpeakerCount {
		t.SpeakerCount = MaxSpeakerCount
	}
	names := make([]string, 0, t.SpeakerCount)
	for i := 0; i < t.SpeakerCount; i++ {
		name := ""
		if i < len(t.SpeakerNames) {
			name = sanitizeName(t.SpeakerNames[i])
		}
		names = append(names, name)
	}
	t.SpeakerNames = names
	if t.UseDefaultTalk {
		t.TalkMinutes = 0
		t.TalkSeconds = 0
	} else {
		if t.TalkMinutes < 0 {
			t.TalkMinutes = 0
		}
		if t.TalkSeconds < 0 {
			t.TalkSeconds = 0
		}
		if t.TalkSeconds > 59 {
			t.TalkMinutes += t.TalkSeconds / 60
			t.TalkSeconds %= 60
		}
	}
	if t.UseDefaultQuestions {
		t.QuestionsMinutes = 0
		t.QuestionsSeconds = 0
	} else {
		if t.QuestionsMinutes < 0 {
			t.QuestionsMinutes = 0
		}
		if t.QuestionsSeconds < 0 {
			t.QuestionsSeconds = 0
		}
		if t.QuestionsSeconds > 59 {
			t.QuestionsMinutes += t.QuestionsSeconds / 60
			t.QuestionsSeconds %= 60
		}
	}
	if !t.UseDefaultTalk && t.TalkMinutes == 0 && t.TalkSeconds == 0 {
		t.UseDefaultTalk = true
	}
	if !t.UseDefaultQuestions && t.QuestionsMinutes == 0 && t.QuestionsSeconds == 0 {
		t.UseDefaultQuestions = true
	}
	return t
}

func (t Template) ResolveDurations(s settings.Settings) (talkMin, talkSec, qMin, qSec int) {
	n := t.Normalize()
	if n.UseDefaultTalk {
		talkMin, talkSec = s.TalkMinutes, s.TalkSeconds
	} else {
		talkMin, talkSec = n.TalkMinutes, n.TalkSeconds
	}
	if n.UseDefaultQuestions {
		qMin, qSec = s.QuestionsMinutes, s.QuestionsSeconds
	} else {
		qMin, qSec = n.QuestionsMinutes, n.QuestionsSeconds
	}
	return talkMin, talkSec, qMin, qSec
}

func (t Template) Valid() bool {
	n := t.Normalize()
	return n.BudgetSeconds() > 0 && n.SpeakerCount >= 1
}

type SpeakerRecord struct {
	Index            int           `json:"index"`
	Name             string        `json:"name"`
	TalkSeconds      int           `json:"talkSeconds"`
	QuestionsSeconds int           `json:"questionsSeconds"`
	Status           SpeakerStatus `json:"status"`
}

type State struct {
	Active             bool            `json:"active"`
	TotalBudgetSeconds int             `json:"totalBudgetSeconds"`
	UsedSeconds        int             `json:"usedSeconds"`
	RemainingSeconds   int             `json:"remainingSeconds"`
	CurrentIndex       int             `json:"currentIndex"`
	Speakers           []SpeakerRecord `json:"speakers"`
}

type Clock interface {
	Now() time.Time
}

type realClock struct{}

func (realClock) Now() time.Time { return time.Now() }

type Tracker struct {
	mu       sync.Mutex
	clock    Clock
	onChange func(State)

	active           bool
	tmpl             Template
	speakers         []SpeakerRecord
	currentIndex     int
	overflowSeconds  int
	segment          segment
	segmentStartedAt time.Time
}

func NewTracker() *Tracker {
	return NewTrackerWithClock(realClock{})
}

func NewTrackerWithClock(clock Clock) *Tracker {
	if clock == nil {
		clock = realClock{}
	}
	return &Tracker{clock: clock}
}

func (t *Tracker) SetOnChange(fn func(State)) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.onChange = fn
}

func (t *Tracker) Active() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.active
}

func (t *Tracker) State() State {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.stateLocked()
}

func (t *Tracker) Template() Template {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.tmpl
}

func (t *Tracker) Create(tmpl Template) error {
	tmpl = tmpl.Normalize()
	if !tmpl.Valid() {
		return ErrInvalidTemplate
	}

	t.mu.Lock()
	defer t.mu.Unlock()
	t.initLocked(tmpl)
	t.emitLocked()
	return nil
}

func (t *Tracker) Reset() error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if !t.active {
		return ErrSessionInactive
	}
	t.initLocked(t.tmpl)
	t.emitLocked()
	return nil
}

func (t *Tracker) Close() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.active = false
	t.tmpl = Template{}
	t.speakers = nil
	t.currentIndex = 0
	t.overflowSeconds = 0
	t.clearSegmentLocked()
	t.emitLocked()
}

func (t *Tracker) BeginTalk() {
	t.mu.Lock()
	defer t.mu.Unlock()
	if !t.active {
		return
	}
	t.flushSegmentLocked()
	t.openSegmentLocked(segmentTalk)
	t.emitLocked()
}

func (t *Tracker) BeginQuestions() {
	t.mu.Lock()
	defer t.mu.Unlock()
	if !t.active {
		return
	}
	t.flushSegmentLocked()
	t.openSegmentLocked(segmentQuestions)
	t.emitLocked()
}

func (t *Tracker) Pause() {
	t.mu.Lock()
	defer t.mu.Unlock()
	if !t.active {
		return
	}
	t.flushSegmentLocked()
	t.emitLocked()
}

func (t *Tracker) Resume() {
	t.mu.Lock()
	defer t.mu.Unlock()
	if !t.active || t.segment == segmentNone {
		return
	}
	if t.segmentStartedAt.IsZero() {
		t.segmentStartedAt = t.clock.Now()
	}
	t.emitLocked()
}

func (t *Tracker) StopSegment() {
	t.mu.Lock()
	defer t.mu.Unlock()
	if !t.active {
		return
	}
	t.flushSegmentLocked()
	t.segment = segmentNone
	t.emitLocked()
}

func (t *Tracker) AdvanceSpeaker() {
	t.mu.Lock()
	defer t.mu.Unlock()
	if !t.active {
		return
	}
	t.flushSegmentLocked()
	if t.currentIndex < len(t.speakers) {
		t.speakers[t.currentIndex].Status = StatusDone
		t.currentIndex++
	}
	if t.currentIndex < len(t.speakers) {
		t.speakers[t.currentIndex].Status = StatusActive
	}
	t.openSegmentLocked(segmentTalk)
	t.emitLocked()
}

func (t *Tracker) Tick() {
	t.mu.Lock()
	defer t.mu.Unlock()
	if !t.active {
		return
	}
	t.emitLocked()
}

func (t *Tracker) initLocked(tmpl Template) {
	speakers := make([]SpeakerRecord, tmpl.SpeakerCount)
	for i := 0; i < tmpl.SpeakerCount; i++ {
		status := StatusPending
		if i == 0 {
			status = StatusActive
		}
		speakers[i] = SpeakerRecord{
			Index:  i,
			Name:   displayName(tmpl.SpeakerNames, i),
			Status: status,
		}
	}
	t.active = true
	t.tmpl = tmpl
	t.speakers = speakers
	t.currentIndex = 0
	t.overflowSeconds = 0
	t.clearSegmentLocked()
}

func (t *Tracker) openSegmentLocked(seg segment) {
	t.segment = seg
	t.segmentStartedAt = t.clock.Now()
}

func (t *Tracker) clearSegmentLocked() {
	t.segment = segmentNone
	t.segmentStartedAt = time.Time{}
}

func (t *Tracker) flushSegmentLocked() {
	seconds := t.liveSecondsLocked()
	if seconds > 0 {
		t.applySecondsLocked(seconds)
	}
	t.segmentStartedAt = time.Time{}
}

func (t *Tracker) liveSecondsLocked() int {
	if t.segment == segmentNone || t.segmentStartedAt.IsZero() {
		return 0
	}
	elapsed := t.clock.Now().Sub(t.segmentStartedAt)
	if elapsed < 0 {
		return 0
	}
	return int(elapsed / time.Second)
}

func (t *Tracker) applySecondsLocked(seconds int) {
	if seconds <= 0 {
		return
	}
	if t.currentIndex >= len(t.speakers) {
		t.overflowSeconds += seconds
		return
	}
	switch t.segment {
	case segmentTalk:
		t.speakers[t.currentIndex].TalkSeconds += seconds
	case segmentQuestions:
		t.speakers[t.currentIndex].QuestionsSeconds += seconds
	}
}

func (t *Tracker) stateLocked() State {
	speakers := make([]SpeakerRecord, len(t.speakers))
	copy(speakers, t.speakers)

	used := t.overflowSeconds
	for i := range speakers {
		used += speakers[i].TalkSeconds + speakers[i].QuestionsSeconds
	}

	if live := t.liveSecondsLocked(); live > 0 {
		used += live
		if t.currentIndex >= 0 && t.currentIndex < len(speakers) {
			switch t.segment {
			case segmentTalk:
				speakers[t.currentIndex].TalkSeconds += live
			case segmentQuestions:
				speakers[t.currentIndex].QuestionsSeconds += live
			}
		}
	}

	budget := t.tmpl.BudgetSeconds()
	remaining := budget - used
	if remaining < 0 {
		remaining = 0
	}

	current := t.currentIndex
	if !t.active {
		current = 0
		speakers = []SpeakerRecord{}
		budget = 0
		used = 0
		remaining = 0
	}

	return State{
		Active:             t.active,
		TotalBudgetSeconds: budget,
		UsedSeconds:        used,
		RemainingSeconds:   remaining,
		CurrentIndex:       current,
		Speakers:           speakers,
	}
}

func (t *Tracker) emitLocked() {
	if t.onChange == nil {
		return
	}
	state := t.stateLocked()
	t.mu.Unlock()
	t.onChange(state)
	t.mu.Lock()
}

func sanitizeName(name string) string {
	name = strings.TrimSpace(name)
	if utf8.RuneCountInString(name) <= MaxNameLength {
		return name
	}
	runes := []rune(name)
	return string(runes[:MaxNameLength])
}

func displayName(names []string, index int) string {
	if index >= 0 && index < len(names) {
		if name := strings.TrimSpace(names[index]); name != "" {
			return name
		}
	}
	return fmt.Sprintf("Докладчик %d", index+1)
}
