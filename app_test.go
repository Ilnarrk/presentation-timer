package main

import (
	"testing"

	"timer/internal/settings"
)

func TestAlertPlaybackTargets(t *testing.T) {
	connected := true
	disconnected := false

	t.Run("unmuted plays locally and in conference", func(t *testing.T) {
		s := settings.Default()
		s.MuteConferenceSound = false
		playLocal, playConference := alertPlaybackTargets(s, connected)
		if !playLocal || !playConference {
			t.Fatalf("got local=%v conference=%v, want both true", playLocal, playConference)
		}
	})

	t.Run("muted skips local but still plays in conference", func(t *testing.T) {
		s := settings.Default()
		s.MuteConferenceSound = true
		playLocal, playConference := alertPlaybackTargets(s, connected)
		if playLocal || !playConference {
			t.Fatalf("got local=%v conference=%v, want local=false conference=true", playLocal, playConference)
		}
	})

	t.Run("disconnected conference never receives playback", func(t *testing.T) {
		s := settings.Default()
		playLocal, playConference := alertPlaybackTargets(s, disconnected)
		if !playLocal || playConference {
			t.Fatalf("got local=%v conference=%v, want local=true conference=false", playLocal, playConference)
		}
	})
}

func TestShouldPlayLocalSound(t *testing.T) {
	if !shouldPlayLocalSound(settings.Settings{MuteConferenceSound: false}) {
		t.Fatal("unmuted settings should play locally")
	}
	if shouldPlayLocalSound(settings.Settings{MuteConferenceSound: true}) {
		t.Fatal("muted settings should not play locally")
	}
}
