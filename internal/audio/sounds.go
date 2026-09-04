package audio

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	maxImportBytes = 20 << 20
	maxSoundFrames = 5 * 60 * outputSampleRate
)

var ErrUnsupportedFormat = errors.New("unsupported audio format")
var ErrPreviewInProgress = errors.New("предпрослушивание уже выполняется")

type Device struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type Sound struct {
	ID     string `json:"id"`
	Label  string `json:"label"`
	Source string `json:"source"`
}

type Defaults struct {
	AlertID     string
	QuestionsID string
	NextID      string
}

type soundData struct {
	Sound
	wav  []byte
	path string
}

type soundMetadata struct {
	Label string `json:"label"`
}

type Catalog struct {
	mu        sync.RWMutex
	sounds    map[string]soundData
	order     []string
	customDir string
	defaults  Defaults
}

func NewCatalog(projectFS fs.FS) (*Catalog, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return nil, err
	}
	customDir := filepath.Join(dir, "presentation-timer", "sounds")
	if err := os.MkdirAll(customDir, 0o755); err != nil {
		return nil, err
	}
	catalog := newCatalog(customDir)
	catalog.loadProject(projectFS)
	catalog.loadCustom()
	return catalog, nil
}

func NewMemoryCatalog(projectFS fs.FS) *Catalog {
	catalog := newCatalog("")
	catalog.loadProject(projectFS)
	return catalog
}

func newCatalog(customDir string) *Catalog {
	c := &Catalog{
		sounds:    make(map[string]soundData),
		customDir: customDir,
	}
	for _, builtin := range builtinSounds() {
		c.add(builtin)
	}
	return c
}

func (c *Catalog) ListSounds() []Sound {
	c.mu.RLock()
	defer c.mu.RUnlock()
	out := make([]Sound, 0, len(c.order))
	for _, id := range c.order {
		out = append(out, c.sounds[id].Sound)
	}
	return out
}

func (c *Catalog) Defaults() Defaults {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.defaults
}

func (c *Catalog) Render(soundID string, volume float64) ([]byte, error) {
	c.mu.RLock()
	sound, ok := c.sounds[soundID]
	c.mu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("sound %q not found", soundID)
	}
	wav := sound.wav
	if len(wav) == 0 && sound.path != "" {
		var err error
		wav, err = os.ReadFile(sound.path)
		if err != nil {
			return nil, fmt.Errorf("read sound %q: %w", soundID, err)
		}
	}
	return applyWAVVolume(wav, volume), nil
}

func (c *Catalog) ImportFile(path string) (Sound, error) {
	info, err := os.Stat(path)
	if err != nil {
		return Sound{}, err
	}
	if !info.Mode().IsRegular() {
		return Sound{}, errors.New("selected sound is not a regular file")
	}
	if info.Size() > maxImportBytes {
		return Sound{}, fmt.Errorf("sound exceeds %d MiB limit", maxImportBytes>>20)
	}

	wav, err := normalizeAudioFile(path)
	if err != nil {
		return Sound{}, err
	}
	sum := sha256.Sum256(wav)
	id := fmt.Sprintf("custom:%x", sum[:10])
	label := strings.TrimSpace(strings.TrimSuffix(filepath.Base(path), filepath.Ext(path)))
	if label == "" {
		label = "Импортированный звук"
	}
	sound := soundData{
		Sound: Sound{ID: id, Label: label, Source: "custom"},
		wav:   wav,
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	if existing, ok := c.sounds[id]; ok {
		if existing.Source == "custom" && existing.Label != label {
			existing.Label = label
			c.sounds[id] = existing
			_ = writeSoundMetadata(c.customDir, strings.TrimPrefix(id, "custom:"), label)
		}
		return existing.Sound, nil
	}
	if c.customDir == "" {
		return Sound{}, errors.New("custom sound storage is unavailable")
	}
	stem := strings.TrimPrefix(id, "custom:")
	soundPath := filepath.Join(c.customDir, stem+".wav")
	if err := os.WriteFile(soundPath, wav, 0o644); err != nil {
		return Sound{}, err
	}
	if err := writeSoundMetadata(c.customDir, stem, label); err != nil {
		_ = os.Remove(soundPath)
		return Sound{}, err
	}
	sound.wav = nil
	sound.path = soundPath
	c.addLocked(sound)
	return sound.Sound, nil
}

func (c *Catalog) loadProject(projectFS fs.FS) {
	if projectFS == nil {
		return
	}
	var loaded []soundData
	_ = fs.WalkDir(projectFS, "sounds", func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil || entry.IsDir() || !supportedExtension(path) {
			return nil
		}
		data, err := fs.ReadFile(projectFS, path)
		if err != nil || len(data) > maxImportBytes {
			return nil
		}
		wav, err := normalizeAudio(filepath.Ext(path), data)
		if err != nil {
			return nil
		}
		relative := strings.TrimPrefix(filepath.ToSlash(path), "sounds/")
		label := strings.TrimSuffix(filepath.Base(relative), filepath.Ext(relative))
		loaded = append(loaded, soundData{
			Sound: Sound{ID: "embedded:" + relative, Label: label, Source: "embedded"},
			wav:   wav,
		})
		return nil
	})
	sort.Slice(loaded, func(i, j int) bool { return loaded[i].ID < loaded[j].ID })

	c.mu.Lock()
	defer c.mu.Unlock()
	for _, sound := range loaded {
		c.addLocked(sound)
		c.matchProjectDefaultLocked(sound)
	}
}

func (c *Catalog) loadCustom() {
	if c.customDir == "" {
		return
	}
	entries, err := os.ReadDir(c.customDir)
	if err != nil {
		return
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.EqualFold(filepath.Ext(entry.Name()), ".wav") {
			continue
		}
		path := filepath.Join(c.customDir, entry.Name())
		info, err := entry.Info()
		if err != nil || info.Size() < 44 || info.Size() > maxImportBytes*3 {
			continue
		}
		stem := strings.TrimSuffix(entry.Name(), filepath.Ext(entry.Name()))
		label := "Импортированный звук"
		if data, readErr := os.ReadFile(filepath.Join(c.customDir, stem+".json")); readErr == nil {
			var metadata soundMetadata
			if json.Unmarshal(data, &metadata) == nil && strings.TrimSpace(metadata.Label) != "" {
				label = strings.TrimSpace(metadata.Label)
			}
		}
		id := "custom:" + stem
		c.add(soundData{
			Sound: Sound{ID: id, Label: label, Source: "custom"},
			path:  path,
		})
	}
}

func writeSoundMetadata(dir, stem, label string) error {
	data, err := json.Marshal(soundMetadata{Label: label})
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, stem+".json"), data, 0o644)
}

func (c *Catalog) matchProjectDefaultLocked(sound soundData) {
	name := strings.ToLower(strings.TrimSuffix(filepath.Base(sound.ID), filepath.Ext(sound.ID)))
	name = strings.NewReplacer("_", " ", "-", " ", ".", " ").Replace(name)
	switch {
	case containsAlias(name, "alert", "overtime", "просрочка"):
		if c.defaults.AlertID == "" {
			c.defaults.AlertID = sound.ID
		}
	case containsAlias(name, "questions", "время вопросов"):
		if c.defaults.QuestionsID == "" {
			c.defaults.QuestionsID = sound.ID
		}
	case containsAlias(name, "next", "следующий докладчик"):
		if c.defaults.NextID == "" {
			c.defaults.NextID = sound.ID
		}
	}
}

func containsAlias(name string, aliases ...string) bool {
	for _, alias := range aliases {
		if strings.Contains(name, alias) {
			return true
		}
	}
	return false
}

func (c *Catalog) add(sound soundData) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.addLocked(sound)
}

func (c *Catalog) addLocked(sound soundData) {
	if _, exists := c.sounds[sound.ID]; exists {
		return
	}
	c.sounds[sound.ID] = sound
	c.order = append(c.order, sound.ID)
}

func builtinSounds() []soundData {
	return []soundData{
		{Sound: Sound{ID: "chime", Label: "Мягкий звон", Source: "builtin"}, wav: synthesizeSequence([]toneSegment{
			{freq: 523.25, duration: 180 * time.Millisecond},
			{freq: 659.25, duration: 220 * time.Millisecond},
			{freq: 783.99, duration: 280 * time.Millisecond},
		}, 1)},
		{Sound: Sound{ID: "bell", Label: "Колокол", Source: "builtin"}, wav: synthesizeSequence([]toneSegment{
			{freq: 880, duration: 180 * time.Millisecond},
			{freq: 660, duration: 260 * time.Millisecond},
			{freq: 990, duration: 320 * time.Millisecond},
		}, 1)},
		{Sound: Sound{ID: "beep", Label: "Сигнал", Source: "builtin"}, wav: synthesizeTone(1000, 450*time.Millisecond, 1)},
		{Sound: Sound{ID: "alarm", Label: "Тревога", Source: "builtin"}, wav: synthesizeSequence([]toneSegment{
			{freq: 740, duration: 220 * time.Millisecond},
			{freq: 0, duration: 80 * time.Millisecond},
			{freq: 740, duration: 220 * time.Millisecond},
			{freq: 0, duration: 80 * time.Millisecond},
			{freq: 740, duration: 220 * time.Millisecond},
		}, 1)},
	}
}

type Player struct {
	mu         sync.RWMutex
	previewMu  sync.Mutex
	playMu     sync.Mutex
	catalog    *Catalog
	deviceID   string
	volume     float64
	playbackFn func(string, []byte) error
}

func NewPlayer(catalogs ...*Catalog) *Player {
	var catalog *Catalog
	if len(catalogs) > 0 {
		catalog = catalogs[0]
	}
	if catalog == nil {
		catalog = NewMemoryCatalog(nil)
	}
	return &Player{
		catalog:    catalog,
		deviceID:   "default",
		volume:     0.85,
		playbackFn: playWAV,
	}
}

func (p *Player) SetDevice(deviceID string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.deviceID = deviceID
}

func (p *Player) SetVolume(volume float64) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.volume = clampVolume(volume)
}

func (p *Player) Preview(soundID string) error {
	if !p.previewMu.TryLock() {
		return ErrPreviewInProgress
	}
	defer p.previewMu.Unlock()
	return p.Play(soundID)
}

func (p *Player) Play(soundID string) error {
	p.playMu.Lock()
	defer p.playMu.Unlock()

	p.mu.RLock()
	deviceID, volume := p.deviceID, p.volume
	p.mu.RUnlock()
	wav, err := p.catalog.Render(soundID, volume)
	if err != nil {
		return err
	}
	return p.playbackFn(deviceID, wav)
}

func applyWAVVolume(wav []byte, volume float64) []byte {
	out := append([]byte(nil), wav...)
	volume = clampVolume(volume)
	for i := 44; i+1 < len(out); i += 2 {
		sample := int16(binary.LittleEndian.Uint16(out[i : i+2]))
		scaled := int(math.Round(float64(sample) * volume))
		if scaled > math.MaxInt16 {
			scaled = math.MaxInt16
		} else if scaled < math.MinInt16 {
			scaled = math.MinInt16
		}
		binary.LittleEndian.PutUint16(out[i:i+2], uint16(int16(scaled)))
	}
	return out
}

func clampVolume(volume float64) float64 {
	if volume < 0 {
		return 0
	}
	if volume > 1 {
		return 1
	}
	return volume
}

type toneSegment struct {
	freq     float64
	duration time.Duration
}

func synthesizeTone(freq float64, duration time.Duration, volume float64) []byte {
	return synthesizeSequence([]toneSegment{{freq: freq, duration: duration}}, volume)
}

func synthesizeSequence(segments []toneSegment, volume float64) []byte {
	const sampleRate = outputSampleRate
	var samples []int16
	for _, segment := range segments {
		frameCount := int(float64(sampleRate) * segment.duration.Seconds())
		for i := 0; i < frameCount; i++ {
			var sample float64
			if segment.freq > 0 {
				t := float64(i) / float64(sampleRate)
				envelope := math.Min(1, math.Min(t*12, (segment.duration.Seconds()-t)*12))
				sample = math.Sin(2*math.Pi*segment.freq*t) * envelope * volume
			}
			samples = append(samples, int16(math.MaxInt16*math.Max(-1, math.Min(1, sample))))
		}
	}
	return encodeWAV(samples, sampleRate, 1)
}

func encodeWAV(samples []int16, sampleRate, channels int) []byte {
	data := make([]byte, len(samples)*2)
	for i, sample := range samples {
		binary.LittleEndian.PutUint16(data[i*2:], uint16(sample))
	}
	var buf bytes.Buffer
	buf.WriteString("RIFF")
	_ = binary.Write(&buf, binary.LittleEndian, uint32(36+len(data)))
	buf.WriteString("WAVEfmt ")
	_ = binary.Write(&buf, binary.LittleEndian, uint32(16))
	_ = binary.Write(&buf, binary.LittleEndian, uint16(1))
	_ = binary.Write(&buf, binary.LittleEndian, uint16(channels))
	_ = binary.Write(&buf, binary.LittleEndian, uint32(sampleRate))
	_ = binary.Write(&buf, binary.LittleEndian, uint32(sampleRate*channels*2))
	_ = binary.Write(&buf, binary.LittleEndian, uint16(channels*2))
	_ = binary.Write(&buf, binary.LittleEndian, uint16(16))
	buf.WriteString("data")
	_ = binary.Write(&buf, binary.LittleEndian, uint32(len(data)))
	buf.Write(data)
	return buf.Bytes()
}

func wavDuration(wav []byte) time.Duration {
	if len(wav) < 44 {
		return 0
	}
	bytesPerSecond := binary.LittleEndian.Uint32(wav[28:32])
	dataSize := binary.LittleEndian.Uint32(wav[40:44])
	if bytesPerSecond == 0 {
		return 0
	}
	return time.Duration(float64(dataSize) / float64(bytesPerSecond) * float64(time.Second))
}
