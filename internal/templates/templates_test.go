package templates

import (
	"errors"
	"testing"

	"timer/internal/session"
)

func sampleTemplate() session.Template {
	return session.Template{
		TotalMinutes: 180,
		SpeakerCount: 5,
		UseDefaultTalk: true,
		UseDefaultQuestions: true,
	}.Normalize()
}

func TestAutoNameWithHours(t *testing.T) {
	name := AutoName(sampleTemplate())
	if name != "3 ч 0 мин · 5 докл." {
		t.Fatalf("unexpected name: %q", name)
	}
}

func TestAutoNameWithoutHours(t *testing.T) {
	name := AutoName(session.Template{TotalMinutes: 45, SpeakerCount: 2}.Normalize())
	if name != "45 мин · 2 докл." {
		t.Fatalf("unexpected name: %q", name)
	}
}

func TestSaveAndList(t *testing.T) {
	store := NewMemoryStore()
	entry, err := store.Save(sampleTemplate())
	if err != nil {
		t.Fatalf("save failed: %v", err)
	}
	if entry.Name != "3 ч 0 мин · 5 докл." {
		t.Fatalf("unexpected entry name: %q", entry.Name)
	}
	if len(store.List()) != 1 {
		t.Fatalf("expected one entry")
	}
}

func TestSaveUpdatesDuplicate(t *testing.T) {
	store := NewMemoryStore()
	first, err := store.Save(sampleTemplate())
	if err != nil {
		t.Fatalf("first save failed: %v", err)
	}
	second, err := store.Save(sampleTemplate())
	if err != nil {
		t.Fatalf("second save failed: %v", err)
	}
	if first.ID != second.ID {
		t.Fatalf("duplicate should update existing entry")
	}
	if len(store.List()) != 1 {
		t.Fatalf("expected one entry after duplicate save")
	}
}

func TestSaveLimit(t *testing.T) {
	store := NewMemoryStore()
	for i := 1; i <= MaxEntries; i++ {
		_, err := store.Save(session.Template{
			TotalMinutes: i,
			SpeakerCount: 1,
		}.Normalize())
		if err != nil {
			t.Fatalf("save %d failed: %v", i, err)
		}
	}
	_, err := store.Save(session.Template{
		TotalMinutes: MaxEntries + 1,
		SpeakerCount: 1,
	}.Normalize())
	if !errors.Is(err, ErrLimitReached) {
		t.Fatalf("expected limit error, got %v", err)
	}
}

func TestDelete(t *testing.T) {
	store := NewMemoryStore()
	entry, err := store.Save(sampleTemplate())
	if err != nil {
		t.Fatalf("save failed: %v", err)
	}
	if err := store.Delete(entry.ID); err != nil {
		t.Fatalf("delete failed: %v", err)
	}
	if len(store.List()) != 0 {
		t.Fatalf("expected empty list")
	}
}

func TestMigrateFromSettings(t *testing.T) {
	store := NewMemoryStore()
	if err := store.MigrateFromSettings(sampleTemplate()); err != nil {
		t.Fatalf("migrate failed: %v", err)
	}
	if len(store.List()) != 1 {
		t.Fatalf("expected migrated entry")
	}
	if err := store.MigrateFromSettings(sampleTemplate()); err != nil {
		t.Fatalf("second migrate failed: %v", err)
	}
	if len(store.List()) != 1 {
		t.Fatalf("migrate should not duplicate entries")
	}
}

func TestLatest(t *testing.T) {
	store := NewMemoryStore()
	if _, ok := store.Latest(); ok {
		t.Fatalf("expected no latest template")
	}
	entry, err := store.Save(sampleTemplate())
	if err != nil {
		t.Fatalf("save failed: %v", err)
	}
	latest, ok := store.Latest()
	if !ok || latest.SpeakerCount != 5 {
		t.Fatalf("unexpected latest template: %+v ok=%v", latest, ok)
	}
	_, err = store.Save(session.Template{TotalMinutes: 10, SpeakerCount: 1}.Normalize())
	if err != nil {
		t.Fatalf("second save failed: %v", err)
	}
	latest, ok = store.Latest()
	if !ok || latest.SpeakerCount != 1 {
		t.Fatalf("expected newest template, got %+v", latest)
	}
	_ = entry
}
