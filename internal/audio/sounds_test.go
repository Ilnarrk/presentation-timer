package audio

import (
	"encoding/binary"
	"os"
	"path/filepath"
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
