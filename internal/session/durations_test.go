package session

import (
	"testing"

	"timer/internal/settings"
)

func TestResolveDurationsUsesSettingsDefaults(t *testing.T) {
	s := settings.Default()
	s.TalkMinutes = 12
	s.TalkSeconds = 30
	s.QuestionsMinutes = 4
	s.QuestionsSeconds = 15

	tmpl := Template{
		TotalMinutes:        60,
		SpeakerCount:        2,
		UseDefaultTalk:      true,
		UseDefaultQuestions: true,
	}

	talkMin, talkSec, qMin, qSec := tmpl.ResolveDurations(s)
	if talkMin != 12 || talkSec != 30 || qMin != 4 || qSec != 15 {
		t.Fatalf("unexpected defaults: talk=%d:%d questions=%d:%d", talkMin, talkSec, qMin, qSec)
	}
}

func TestResolveDurationsUsesSessionOverrides(t *testing.T) {
	s := settings.Default()
	tmpl := Template{
		TotalMinutes:        60,
		SpeakerCount:        2,
		TalkMinutes:         8,
		TalkSeconds:         20,
		QuestionsMinutes:    3,
		QuestionsSeconds:    10,
		UseDefaultTalk:      false,
		UseDefaultQuestions: false,
	}

	talkMin, talkSec, qMin, qSec := tmpl.ResolveDurations(s)
	if talkMin != 8 || talkSec != 20 || qMin != 3 || qSec != 10 {
		t.Fatalf("unexpected overrides: talk=%d:%d questions=%d:%d", talkMin, talkSec, qMin, qSec)
	}
}

func TestResolveDurationsMixedOverrides(t *testing.T) {
	s := settings.Default()
	s.QuestionsMinutes = 6
	s.QuestionsSeconds = 0

	tmpl := Template{
		TalkMinutes:         15,
		TalkSeconds:         0,
		UseDefaultTalk:      false,
		UseDefaultQuestions: true,
	}

	talkMin, talkSec, qMin, qSec := tmpl.ResolveDurations(s)
	if talkMin != 15 || talkSec != 0 || qMin != 6 || qSec != 0 {
		t.Fatalf("unexpected mixed durations: talk=%d:%d questions=%d:%d", talkMin, talkSec, qMin, qSec)
	}
}
