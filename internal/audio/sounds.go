package audio

import (
	"bytes"
	"encoding/binary"
	"math"
	"time"
)

type Device struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type Sound struct {
	ID    string `json:"id"`
	Label string `json:"label"`
}

var soundCatalog = []Sound{
	{ID: "chime", Label: "Мягкий звон"},
	{ID: "bell", Label: "Колокол"},
	{ID: "beep", Label: "Сигнал"},
	{ID: "alarm", Label: "Тревога"},
}

type Player struct {
	deviceID string
	volume   float64
}

func NewPlayer() *Player {
	return &Player{
		deviceID: "default",
		volume:   0.85,
	}
}

func (p *Player) SetDevice(deviceID string) {
	p.deviceID = deviceID
}

func (p *Player) SetVolume(volume float64) {
	if volume < 0 {
		volume = 0
	}
	if volume > 1 {
		volume = 1
	}
	p.volume = volume
}

func ListSounds() []Sound {
	out := make([]Sound, len(soundCatalog))
	copy(out, soundCatalog)
	return out
}

func (p *Player) Preview(soundID string) error {
	return p.Play(soundID)
}

func (p *Player) Play(soundID string) error {
	wav, err := renderSound(soundID, p.volume)
	if err != nil {
		return err
	}
	return playWAV(p.deviceID, wav)
}

// RenderSound returns the same WAV payload used by local playback. Callers can
// send it to another audio transport, such as a conference MediaStream.
func RenderSound(soundID string, volume float64) ([]byte, error) {
	if volume < 0 {
		volume = 0
	}
	if volume > 1 {
		volume = 1
	}
	return renderSound(soundID, volume)
}

type toneSegment struct {
	freq     float64
	duration time.Duration
}

func renderSound(soundID string, volume float64) ([]byte, error) {
	switch soundID {
	case "bell":
		return synthesizeSequence([]toneSegment{
			{freq: 880, duration: 180 * time.Millisecond},
			{freq: 660, duration: 260 * time.Millisecond},
			{freq: 990, duration: 320 * time.Millisecond},
		}, volume), nil
	case "beep":
		return synthesizeTone(1000, 450*time.Millisecond, volume), nil
	case "alarm":
		return synthesizeSequence([]toneSegment{
			{freq: 740, duration: 220 * time.Millisecond},
			{freq: 0, duration: 80 * time.Millisecond},
			{freq: 740, duration: 220 * time.Millisecond},
			{freq: 0, duration: 80 * time.Millisecond},
			{freq: 740, duration: 220 * time.Millisecond},
		}, volume), nil
	default:
		return synthesizeSequence([]toneSegment{
			{freq: 523.25, duration: 180 * time.Millisecond},
			{freq: 659.25, duration: 220 * time.Millisecond},
			{freq: 783.99, duration: 280 * time.Millisecond},
		}, volume), nil
	}
}

func synthesizeTone(freq float64, duration time.Duration, volume float64) []byte {
	return synthesizeSequence([]toneSegment{{freq: freq, duration: duration}}, volume)
}

func synthesizeSequence(segments []toneSegment, volume float64) []byte {
	const sampleRate = 44100
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
			if sample > 1 {
				sample = 1
			}
			if sample < -1 {
				sample = -1
			}
			samples = append(samples, int16(sample*math.MaxInt16))
		}
	}

	return encodeWAV(samples, sampleRate, 1)
}

func encodeWAV(samples []int16, sampleRate int, channels int) []byte {
	data := make([]byte, len(samples)*2)
	for i, sample := range samples {
		binary.LittleEndian.PutUint16(data[i*2:], uint16(sample))
	}

	var buf bytes.Buffer
	buf.WriteString("RIFF")
	_ = binary.Write(&buf, binary.LittleEndian, uint32(36+len(data)))
	buf.WriteString("WAVE")
	buf.WriteString("fmt ")
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
	sampleRate := binary.LittleEndian.Uint32(wav[24:28])
	channels := binary.LittleEndian.Uint16(wav[22:24])
	bitsPerSample := binary.LittleEndian.Uint16(wav[34:36])
	dataSize := binary.LittleEndian.Uint32(wav[40:44])
	if sampleRate == 0 || channels == 0 || bitsPerSample == 0 {
		return 0
	}
	bytesPerSecond := sampleRate * uint32(channels) * uint32(bitsPerSample) / 8
	if bytesPerSecond == 0 {
		return 0
	}
	seconds := float64(dataSize) / float64(bytesPerSecond)
	return time.Duration(seconds * float64(time.Second))
}
