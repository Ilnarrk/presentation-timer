package session

import (
	"testing"
	"time"
)

type fakeClock struct {
	current time.Time
}

func newFakeClock() *fakeClock {
	return &fakeClock{current: time.Date(2026, 9, 4, 12, 0, 0, 0, time.UTC)}
}

func (c *fakeClock) Now() time.Time { return c.current }

func (c *fakeClock) Advance(d time.Duration) { c.current = c.current.Add(d) }

func testTemplate() Template {
	return Template{
		TotalMinutes: 60,
		SpeakerCount: 3,
		SpeakerNames: []string{"Иван", "", "Мария"},
	}
}

func TestCreateRejectsIncompleteTemplate(t *testing.T) {
	tracker := NewTracker()
	if err := tracker.Create(Template{SpeakerCount: 2}); err != ErrInvalidTemplate {
		t.Fatalf("expected invalid template, got %v", err)
	}
	if err := tracker.Create(Template{TotalMinutes: 10}); err != ErrInvalidTemplate {
		t.Fatalf("expected invalid template, got %v", err)
	}
}

func TestCreateInitializesQueue(t *testing.T) {
	tracker := NewTracker()
	if err := tracker.Create(testTemplate()); err != nil {
		t.Fatal(err)
	}
	state := tracker.State()
	if !state.Active || state.TotalBudgetSeconds != 3600 || len(state.Speakers) != 3 {
		t.Fatalf("unexpected state: %+v", state)
	}
	if state.Speakers[0].Name != "Иван" || state.Speakers[0].Status != StatusActive {
		t.Fatalf("unexpected first speaker: %+v", state.Speakers[0])
	}
	if state.Speakers[1].Name != "Докладчик 2" || state.Speakers[1].Status != StatusPending {
		t.Fatalf("unexpected second speaker: %+v", state.Speakers[1])
	}
	if state.Speakers[2].Name != "Мария" {
		t.Fatalf("unexpected third speaker: %+v", state.Speakers[2])
	}
}

func TestTalkAndQuestionsAccumulation(t *testing.T) {
	clock := newFakeClock()
	tracker := NewTrackerWithClock(clock)
	if err := tracker.Create(testTemplate()); err != nil {
		t.Fatal(err)
	}

	tracker.BeginTalk()
	clock.Advance(10 * time.Minute)
	state := tracker.State()
	if state.Speakers[0].TalkSeconds != 600 || state.UsedSeconds != 600 {
		t.Fatalf("talk not accumulated: %+v", state.Speakers[0])
	}

	tracker.BeginQuestions()
	clock.Advance(3*time.Minute + 20*time.Second)
	state = tracker.State()
	if state.Speakers[0].TalkSeconds != 600 {
		t.Fatalf("talk changed after questions: %+v", state.Speakers[0])
	}
	if state.Speakers[0].QuestionsSeconds != 200 {
		t.Fatalf("questions not accumulated: %+v", state.Speakers[0])
	}
	if state.UsedSeconds != 800 || state.RemainingSeconds != 3600-800 {
		t.Fatalf("budget mismatch: used=%d remaining=%d", state.UsedSeconds, state.RemainingSeconds)
	}
}

func TestPauseDoesNotCountTime(t *testing.T) {
	clock := newFakeClock()
	tracker := NewTrackerWithClock(clock)
	_ = tracker.Create(testTemplate())

	tracker.BeginTalk()
	clock.Advance(90 * time.Second)
	tracker.Pause()
	clock.Advance(5 * time.Minute)
	state := tracker.State()
	if state.Speakers[0].TalkSeconds != 90 {
		t.Fatalf("paused time was counted: %+v", state.Speakers[0])
	}

	tracker.Resume()
	clock.Advance(30 * time.Second)
	state = tracker.State()
	if state.Speakers[0].TalkSeconds != 120 {
		t.Fatalf("resume did not continue: %+v", state.Speakers[0])
	}
}

func TestNextSpeakerAndResetTimer(t *testing.T) {
	clock := newFakeClock()
	tracker := NewTrackerWithClock(clock)
	_ = tracker.Create(testTemplate())

	tracker.BeginTalk()
	clock.Advance(2 * time.Minute)
	tracker.BeginQuestions()
	clock.Advance(40 * time.Second)
	tracker.AdvanceSpeaker()

	state := tracker.State()
	if state.Speakers[0].Status != StatusDone || state.Speakers[0].TalkSeconds != 120 || state.Speakers[0].QuestionsSeconds != 40 {
		t.Fatalf("first speaker not closed: %+v", state.Speakers[0])
	}
	if state.Speakers[1].Status != StatusActive || state.CurrentIndex != 1 {
		t.Fatalf("second speaker not active: %+v", state)
	}

	clock.Advance(15 * time.Second)
	tracker.StopSegment()
	clock.Advance(10 * time.Minute)
	state = tracker.State()
	if state.Speakers[1].TalkSeconds != 15 {
		t.Fatalf("timer reset lost or added time: %+v", state.Speakers[1])
	}
	if state.Speakers[1].Status != StatusActive {
		t.Fatalf("speaker should stay active after timer reset: %+v", state.Speakers[1])
	}
}

func TestLastSpeakerAdvanceCountsOverflowBudget(t *testing.T) {
	clock := newFakeClock()
	tracker := NewTrackerWithClock(clock)
	_ = tracker.Create(Template{TotalMinutes: 10, SpeakerCount: 1, SpeakerNames: []string{"Иван"}})

	tracker.BeginTalk()
	clock.Advance(3 * time.Minute)
	tracker.AdvanceSpeaker()
	clock.Advance(2 * time.Minute)

	state := tracker.State()
	if state.Speakers[0].Status != StatusDone {
		t.Fatalf("expected last speaker done: %+v", state.Speakers[0])
	}
	if state.CurrentIndex != 1 {
		t.Fatalf("expected current index past last speaker, got %d", state.CurrentIndex)
	}
	if state.UsedSeconds != 5*60 {
		t.Fatalf("overflow should still count toward budget, used=%d", state.UsedSeconds)
	}
}

func TestResetSessionClearsTimes(t *testing.T) {
	clock := newFakeClock()
	tracker := NewTrackerWithClock(clock)
	_ = tracker.Create(testTemplate())
	tracker.BeginTalk()
	clock.Advance(4 * time.Minute)
	tracker.AdvanceSpeaker()

	if err := tracker.Reset(); err != nil {
		t.Fatal(err)
	}
	state := tracker.State()
	if !state.Active || state.CurrentIndex != 0 || state.UsedSeconds != 0 {
		t.Fatalf("session was not reset: %+v", state)
	}
	if state.Speakers[0].Status != StatusActive || state.Speakers[0].Name != "Иван" {
		t.Fatalf("first speaker not restored: %+v", state.Speakers[0])
	}
	if state.Speakers[0].TalkSeconds != 0 || state.Speakers[1].Status != StatusPending {
		t.Fatalf("times were not cleared: %+v", state)
	}
}

func TestCloseHidesSession(t *testing.T) {
	tracker := NewTracker()
	_ = tracker.Create(testTemplate())
	tracker.Close()
	state := tracker.State()
	if state.Active || len(state.Speakers) != 0 {
		t.Fatalf("expected inactive session, got %+v", state)
	}
}

func TestOvertimeCountsAsCurrentPhase(t *testing.T) {
	clock := newFakeClock()
	tracker := NewTrackerWithClock(clock)
	_ = tracker.Create(testTemplate())
	tracker.BeginTalk()
	clock.Advance(12 * time.Minute)
	state := tracker.State()
	if state.Speakers[0].TalkSeconds != 12*60 {
		t.Fatalf("overtime should count as talk: %+v", state.Speakers[0])
	}
}

func TestResetInactiveSession(t *testing.T) {
	tracker := NewTracker()
	if err := tracker.Reset(); err != ErrSessionInactive {
		t.Fatalf("expected inactive error, got %v", err)
	}
}
