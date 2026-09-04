package audio

import (
	"context"
	"encoding/binary"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"testing/fstest"
	"time"
)

func TestProjectSoundsAndAliasDefaults(t *testing.T) {
	wav := synthesizeTone(440, 20*time.Millisecond, 1)
	project := fstest.MapFS{
		"sounds/my_alert.wav":            {Data: wav},
		"sounds/время вопросов.wav":      {Data: wav},
		"sounds/следующий-докладчик.wav": {Data: wav},
		"sounds/other.wav":               {Data: wav},
		"sounds/README.txt":              {Data: []byte("documentation")},
	}
	catalog := NewMemoryCatalog(project)

	if got := len(catalog.ListSounds()); got != 8 {
		t.Fatalf("expected built-ins and four embedded sounds, got %d", got)
	}
	defaults := catalog.Defaults()
	if defaults.AlertID != "embedded:my_alert.wav" ||
		defaults.QuestionsID != "embedded:время вопросов.wav" ||
		defaults.NextID != "embedded:следующий-докладчик.wav" {
		t.Fatalf("unexpected project defaults: %+v", defaults)
	}
}

func TestImportNormalizesToStandardWAV(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "source.wav")
	if err := os.WriteFile(source, synthesizeTone(440, 20*time.Millisecond, 1), 0o600); err != nil {
		t.Fatal(err)
	}
	customDir := filepath.Join(dir, "custom")
	if err := os.Mkdir(customDir, 0o700); err != nil {
		t.Fatal(err)
	}
	catalog := newCatalog(customDir)

	sound, err := catalog.ImportFile(source)
	if err != nil {
		t.Fatal(err)
	}
	rendered, err := catalog.Render(sound.ID, 1)
	if err != nil {
		t.Fatal(err)
	}
	if string(rendered[:4]) != "RIFF" ||
		binary.LittleEndian.Uint16(rendered[20:22]) != 1 ||
		binary.LittleEndian.Uint16(rendered[22:24]) != 2 ||
		binary.LittleEndian.Uint32(rendered[24:28]) != outputSampleRate ||
		binary.LittleEndian.Uint16(rendered[34:36]) != 16 {
		t.Fatalf("unexpected normalized WAV format")
	}

	reloaded := newCatalog(customDir)
	reloaded.loadCustom()
	var imported Sound
	for _, candidate := range reloaded.ListSounds() {
		if candidate.ID == sound.ID {
			imported = candidate
			break
		}
	}
	if imported.Label != "source" {
		t.Fatalf("expected original filename after reload, got %q", imported.Label)
	}
	if len(reloaded.sounds[sound.ID].wav) != 0 || reloaded.sounds[sound.ID].path == "" {
		t.Fatal("expected imported sound to be loaded lazily")
	}
}

func TestRejectsOversizedDuration(t *testing.T) {
	input := decodedAudio{
		samples:    make([]float32, 301),
		sampleRate: 1,
		channels:   1,
	}
	if _, err := normalizePCM(input); err == nil {
		t.Fatal("expected duration limit error")
	}
}

func TestPreviewRejectsConcurrentPlayback(t *testing.T) {
	player := NewPlayer()
	started := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	player.playbackFn = func(context.Context, string, []byte) error {
		once.Do(func() { close(started) })
		<-release
		return nil
	}

	firstDone := make(chan error, 1)
	go func() {
		firstDone <- player.Preview("chime")
	}()
	<-started

	if err := player.Preview("chime"); !errors.Is(err, ErrPreviewInProgress) {
		t.Fatalf("second Preview() error = %v, want ErrPreviewInProgress", err)
	}
	close(release)
	if err := <-firstDone; err != nil {
		t.Fatalf("first Preview() error = %v", err)
	}
}

func TestPlayInterruptsConcurrentPlayback(t *testing.T) {
	player := NewPlayer()
	firstStarted := make(chan struct{})
	firstInterrupted := make(chan struct{})
	secondStarted := make(chan struct{})
	var once sync.Once

	player.playbackFn = func(ctx context.Context, _ string, _ []byte) error {
		once.Do(func() { close(firstStarted) })
		select {
		case <-ctx.Done():
			close(firstInterrupted)
			return ctx.Err()
		case <-time.After(5 * time.Second):
			return nil
		}
	}

	firstDone := make(chan error, 1)
	go func() {
		firstDone <- player.Play("chime")
	}()
	<-firstStarted

	player.playbackFn = func(context.Context, string, []byte) error {
		close(secondStarted)
		return nil
	}

	secondDone := make(chan error, 1)
	go func() {
		secondDone <- player.Play("chime")
	}()

	<-firstInterrupted
	<-secondStarted

	if err := <-firstDone; !errors.Is(err, context.Canceled) {
		t.Fatalf("first Play() error = %v, want context.Canceled", err)
	}
	if err := <-secondDone; err != nil {
		t.Fatalf("second Play() error = %v", err)
	}
}
