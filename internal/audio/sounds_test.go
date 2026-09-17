package audio

import (
	"context"
	"encoding/binary"
	"errors"
	"math"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"testing/fstest"
	"time"
)

func TestResolveSoundIDMigratesUnknownIDs(t *testing.T) {
	wav := synthesizeTone(440, 20*time.Millisecond, 1)
	catalog := NewMemoryCatalog(fstest.MapFS{
		"sounds/alert.wav": {Data: wav},
		"sounds/other.wav": {Data: wav},
	})

	if got := catalog.ResolveSoundID("chime", catalog.Defaults().AlertID); got != "embedded:alert.wav" {
		t.Fatalf("expected alert fallback, got %q", got)
	}
	if got := catalog.ResolveOptionalSoundID("chime"); got != "" {
		t.Fatalf("expected empty optional sound, got %q", got)
	}
	if got := catalog.ResolveOptionalSoundID("embedded:other.wav"); got != "embedded:other.wav" {
		t.Fatalf("expected optional sound to stay, got %q", got)
	}
}

func TestProjectSoundsAndDefaults(t *testing.T) {
	wav := synthesizeTone(440, 20*time.Millisecond, 1)
	project := fstest.MapFS{
		"sounds/alert.wav":               {Data: wav},
		"sounds/время вопросов.wav":      {Data: wav},
		"sounds/следующий-докладчик.wav": {Data: wav},
		"sounds/other.wav":               {Data: wav},
		"sounds/README.txt":              {Data: []byte("documentation")},
	}
	catalog := NewMemoryCatalog(project)

	sounds := catalog.ListSounds()
	if len(sounds) != 4 {
		t.Fatalf("expected four embedded sounds, got %d", len(sounds))
	}
	if sounds[0].Label != "alert" || sounds[1].Label != "other" {
		t.Fatalf("expected original filenames as labels, got %+v", sounds)
	}
	defaults := catalog.Defaults()
	if defaults.AlertID != "embedded:alert.wav" {
		t.Fatalf("unexpected alert default: %+v", defaults)
	}
	if defaults.QuestionsID != "" || defaults.NextID != "" {
		t.Fatalf("only alert should be auto-assigned: %+v", defaults)
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

func testPlayerWithSound() *Player {
	wav := synthesizeTone(440, 20*time.Millisecond, 1)
	catalog := NewMemoryCatalog(fstest.MapFS{
		"sounds/test.wav": {Data: wav},
	})
	return NewPlayer(catalog)
}

func TestPreviewRejectsConcurrentPlayback(t *testing.T) {
	player := testPlayerWithSound()
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
		firstDone <- player.Preview("embedded:test.wav")
	}()
	<-started

	if err := player.Preview("embedded:test.wav"); !errors.Is(err, ErrPreviewInProgress) {
		t.Fatalf("second Preview() error = %v, want ErrPreviewInProgress", err)
	}
	close(release)
	if err := <-firstDone; err != nil {
		t.Fatalf("first Preview() error = %v", err)
	}
}

func TestPlayInterruptsConcurrentPlayback(t *testing.T) {
	player := testPlayerWithSound()
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
		firstDone <- player.Play("embedded:test.wav")
	}()
	<-firstStarted

	player.playbackFn = func(context.Context, string, []byte) error {
		close(secondStarted)
		return nil
	}

	secondDone := make(chan error, 1)
	go func() {
		secondDone <- player.Play("embedded:test.wav")
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

func synthesizeTone(freq float64, duration time.Duration, volume float64) []byte {
	const sampleRate = outputSampleRate
	frameCount := int(float64(sampleRate) * duration.Seconds())
	samples := make([]int16, frameCount)
	for i := 0; i < frameCount; i++ {
		t := float64(i) / float64(sampleRate)
		envelope := math.Min(1, math.Min(t*12, (duration.Seconds()-t)*12))
		sample := math.Sin(2*math.Pi*freq*t) * envelope * volume
		samples[i] = int16(math.MaxInt16 * math.Max(-1, math.Min(1, sample)))
	}
	return encodeWAV(samples, sampleRate, 1)
}
