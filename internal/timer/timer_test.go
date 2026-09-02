package timer

import (
	"testing"
	"time"
)

func testConfig() Config {
	return Config{
		TalkDuration:      10 * time.Minute,
		QuestionsDuration: 5 * time.Minute,
		ReminderInterval:  2 * time.Minute,
	}
}

func TestStartAndTalkDeadline(t *testing.T) {
	clock := NewFakeClock(time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC))
	engine := NewEngineWithClock(testConfig(), clock)

	var alerts []AlertEvent
	engine.SetCallbacks(nil, func(event AlertEvent) {
		alerts = append(alerts, event)
	})

	if err := engine.Start(); err != nil {
		t.Fatalf("start: %v", err)
	}

	snap := engine.Snapshot()
	if snap.Phase != PhaseTalk || snap.RemainingSeconds != 600 {
		t.Fatalf("unexpected start snapshot: %+v", snap)
	}

	clock.Advance(10 * time.Minute)
	engine.tick()

	snap = engine.Snapshot()
	if snap.Phase != PhaseTalkOvertime {
		t.Fatalf("expected talk overtime, got %+v", snap)
	}
	if len(alerts) != 1 || alerts[0].Repeated {
		t.Fatalf("expected first alert, got %+v", alerts)
	}
}

func TestOvertimeRepeatsEveryTwoMinutes(t *testing.T) {
	clock := NewFakeClock(time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC))
	engine := NewEngineWithClock(testConfig(), clock)

	var alerts []AlertEvent
	engine.SetCallbacks(nil, func(event AlertEvent) {
		alerts = append(alerts, event)
	})

	_ = engine.Start()
	clock.Advance(10 * time.Minute)
	engine.tick()

	clock.Advance(2 * time.Minute)
	engine.tick()

	if len(alerts) != 2 || !alerts[1].Repeated {
		t.Fatalf("expected repeated alert, got %+v", alerts)
	}
}

func TestOvertimeUsesConfiguredReminderInterval(t *testing.T) {
	clock := NewFakeClock(time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC))
	cfg := testConfig()
	cfg.ReminderInterval = 35 * time.Second
	engine := NewEngineWithClock(cfg, clock)

	var alerts []AlertEvent
	engine.SetCallbacks(nil, func(event AlertEvent) {
		alerts = append(alerts, event)
	})

	_ = engine.Start()
	clock.Advance(10 * time.Minute)
	engine.tick()

	clock.Advance(34 * time.Second)
	engine.tick()
	if len(alerts) != 1 {
		t.Fatalf("reminder fired too early: %+v", alerts)
	}

	clock.Advance(time.Second)
	engine.tick()
	if len(alerts) != 2 || !alerts[1].Repeated {
		t.Fatalf("configured reminder did not fire: %+v", alerts)
	}
}

func TestInvalidReminderFallsBackToDefault(t *testing.T) {
	cfg := testConfig()
	cfg.ReminderInterval = 0
	engine := NewEngine(cfg)
	if engine.cfg.ReminderInterval != DefaultReminderInterval {
		t.Fatalf("unexpected reminder fallback: %v", engine.cfg.ReminderInterval)
	}
}

func TestManualTransitions(t *testing.T) {
	clock := NewFakeClock(time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC))
	engine := NewEngineWithClock(testConfig(), clock)

	_ = engine.Start()
	clock.Advance(5 * time.Minute)

	if err := engine.GoToQuestions(); err != nil {
		t.Fatalf("go to questions: %v", err)
	}

	snap := engine.Snapshot()
	if snap.Phase != PhaseQuestions || snap.RemainingSeconds != 300 {
		t.Fatalf("unexpected questions snapshot: %+v", snap)
	}

	clock.Advance(5 * time.Minute)
	if err := engine.NextSpeaker(); err != nil {
		t.Fatalf("next speaker: %v", err)
	}

	snap = engine.Snapshot()
	if snap.Phase != PhaseTalk || snap.RemainingSeconds != 600 {
		t.Fatalf("unexpected next speaker snapshot: %+v", snap)
	}
}

func TestNextSpeakerDirectlyFromTalk(t *testing.T) {
	clock := NewFakeClock(time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC))
	engine := NewEngineWithClock(testConfig(), clock)

	_ = engine.Start()
	clock.Advance(4 * time.Minute)
	if err := engine.NextSpeaker(); err != nil {
		t.Fatalf("next speaker from talk: %v", err)
	}

	snap := engine.Snapshot()
	if snap.Phase != PhaseTalk || snap.RemainingSeconds != 600 {
		t.Fatalf("unexpected next speaker snapshot: %+v", snap)
	}
}

func TestPausePreservesRemaining(t *testing.T) {
	clock := NewFakeClock(time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC))
	engine := NewEngineWithClock(testConfig(), clock)

	_ = engine.Start()
	clock.Advance(2 * time.Minute)
	engine.Pause()

	snap := engine.Snapshot()
	if !snap.IsPaused || snap.RemainingSeconds != 480 {
		t.Fatalf("unexpected paused snapshot: %+v", snap)
	}

	clock.Advance(1 * time.Minute)
	engine.tick()
	snap = engine.Snapshot()
	if snap.RemainingSeconds != 480 {
		t.Fatalf("remaining changed while paused: %+v", snap)
	}

	_ = engine.Start()
	clock.Advance(1 * time.Minute)
	engine.tick()
	snap = engine.Snapshot()
	if snap.RemainingSeconds != 420 {
		t.Fatalf("unexpected resumed snapshot: %+v", snap)
	}
}

func TestInvalidTransitions(t *testing.T) {
	engine := NewEngine(testConfig())

	if err := engine.GoToQuestions(); err != ErrInvalidTransition {
		t.Fatalf("expected invalid transition, got %v", err)
	}

	if err := engine.NextSpeaker(); err != ErrInvalidTransition {
		t.Fatalf("expected invalid transition, got %v", err)
	}
}

func TestReset(t *testing.T) {
	engine := NewEngine(testConfig())
	_ = engine.Start()
	engine.Reset()

	snap := engine.Snapshot()
	if snap.Phase != PhaseIdle || snap.IsRunning {
		t.Fatalf("expected idle after reset, got %+v", snap)
	}
}
