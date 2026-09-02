package audio

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"strings"

	"github.com/hajimehoshi/go-mp3"
	"github.com/jfreymuth/oggvorbis"
)

const outputSampleRate = 44100

type decodedAudio struct {
	samples    []float32
	sampleRate int
	channels   int
}

func supportedExtension(path string) bool {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".wav", ".mp3", ".ogg":
		return true
	default:
		return false
	}
}

func normalizeAudioFile(path string) ([]byte, error) {
	if !supportedExtension(path) {
		return nil, ErrUnsupportedFormat
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if len(data) > maxImportBytes {
		return nil, fmt.Errorf("sound exceeds %d MiB limit", maxImportBytes>>20)
	}
	return normalizeAudio(filepath.Ext(path), data)
}

func normalizeAudio(extension string, data []byte) ([]byte, error) {
	var (
		decoded decodedAudio
		err     error
	)
	switch strings.ToLower(extension) {
	case ".wav":
		decoded, err = decodeWAV(data)
	case ".mp3":
		decoded, err = decodeMP3(data)
	case ".ogg":
		decoded, err = decodeOGG(data)
	default:
		return nil, ErrUnsupportedFormat
	}
	if err != nil {
		return nil, err
	}
	return normalizePCM(decoded)
}

func decodeMP3(data []byte) (decodedAudio, error) {
	decoder, err := mp3.NewDecoder(bytes.NewReader(data))
	if err != nil {
		return decodedAudio{}, fmt.Errorf("decode MP3: %w", err)
	}
	if decoder.SampleRate() <= 0 || decoder.SampleRate() > 192000 {
		return decodedAudio{}, errors.New("unsupported MP3 sample rate")
	}
	maxBytes := int64(5*60*decoder.SampleRate()*2*2 + 1)
	pcm, err := io.ReadAll(io.LimitReader(decoder, maxBytes))
	if err != nil {
		return decodedAudio{}, fmt.Errorf("decode MP3: %w", err)
	}
	if int64(len(pcm)) >= maxBytes {
		return decodedAudio{}, errors.New("sound exceeds 5 minute duration limit")
	}
	if len(pcm)%4 != 0 {
		return decodedAudio{}, errors.New("invalid MP3 PCM output")
	}
	samples := make([]float32, len(pcm)/2)
	for i := range samples {
		samples[i] = float32(int16(binary.LittleEndian.Uint16(pcm[i*2:]))) / 32768
	}
	return decodedAudio{samples: samples, sampleRate: decoder.SampleRate(), channels: 2}, nil
}

func decodeOGG(data []byte) (decodedAudio, error) {
	reader, err := oggvorbis.NewReader(bytes.NewReader(data))
	if err != nil {
		return decodedAudio{}, fmt.Errorf("decode OGG: %w", err)
	}
	rate, channels := reader.SampleRate(), reader.Channels()
	if rate <= 0 || rate > 192000 || channels <= 0 || channels > 8 {
		return decodedAudio{}, errors.New("unsupported OGG channel count or sample rate")
	}
	maxSamples := 5 * 60 * rate * channels
	samples := make([]float32, 0, min(maxSamples, rate*channels*10))
	buffer := make([]float32, 8192)
	for {
		n, readErr := reader.Read(buffer)
		if len(samples)+n > maxSamples {
			return decodedAudio{}, errors.New("sound exceeds 5 minute duration limit")
		}
		samples = append(samples, buffer[:n]...)
		if errors.Is(readErr, io.EOF) {
			break
		}
		if readErr != nil {
			return decodedAudio{}, fmt.Errorf("decode OGG: %w", readErr)
		}
	}
	return decodedAudio{samples: samples, sampleRate: rate, channels: channels}, nil
}

func decodeWAV(data []byte) (decodedAudio, error) {
	if len(data) < 12 || string(data[:4]) != "RIFF" || string(data[8:12]) != "WAVE" {
		return decodedAudio{}, errors.New("invalid WAV header")
	}
	var format, pcm []byte
	for offset := 12; offset+8 <= len(data); {
		size := int(binary.LittleEndian.Uint32(data[offset+4 : offset+8]))
		start := offset + 8
		end := start + size
		if size < 0 || end < start || end > len(data) {
			return decodedAudio{}, errors.New("invalid WAV chunk")
		}
		switch string(data[offset : offset+4]) {
		case "fmt ":
			format = data[start:end]
		case "data":
			pcm = data[start:end]
		}
		offset = end + size%2
	}
	if len(format) < 16 || pcm == nil {
		return decodedAudio{}, errors.New("WAV is missing format or data")
	}
	formatTag := binary.LittleEndian.Uint16(format[0:2])
	channels := int(binary.LittleEndian.Uint16(format[2:4]))
	rate := int(binary.LittleEndian.Uint32(format[4:8]))
	blockAlign := int(binary.LittleEndian.Uint16(format[12:14]))
	bits := int(binary.LittleEndian.Uint16(format[14:16]))
	if formatTag == 0xfffe && len(format) >= 26 {
		formatTag = binary.LittleEndian.Uint16(format[24:26])
	}
	if channels <= 0 || channels > 8 || rate <= 0 || rate > 192000 {
		return decodedAudio{}, errors.New("unsupported WAV channel count or sample rate")
	}
	bytesPerSample := (bits + 7) / 8
	if blockAlign != channels*bytesPerSample || blockAlign == 0 || len(pcm)%blockAlign != 0 {
		return decodedAudio{}, errors.New("invalid WAV block alignment")
	}
	frames := len(pcm) / blockAlign
	if frames > 5*60*rate {
		return decodedAudio{}, errors.New("sound exceeds 5 minute duration limit")
	}
	samples := make([]float32, frames*channels)
	for i := range samples {
		offset := i * bytesPerSample
		switch {
		case formatTag == 1 && bits == 8:
			samples[i] = (float32(pcm[offset]) - 128) / 128
		case formatTag == 1 && bits == 16:
			samples[i] = float32(int16(binary.LittleEndian.Uint16(pcm[offset:]))) / 32768
		case formatTag == 1 && bits == 24:
			value := int32(pcm[offset]) | int32(pcm[offset+1])<<8 | int32(pcm[offset+2])<<16
			if value&0x800000 != 0 {
				value |= ^int32(0xffffff)
			}
			samples[i] = float32(value) / 8388608
		case formatTag == 1 && bits == 32:
			samples[i] = float32(int32(binary.LittleEndian.Uint32(pcm[offset:]))) / 2147483648
		case formatTag == 3 && bits == 32:
			samples[i] = math.Float32frombits(binary.LittleEndian.Uint32(pcm[offset:]))
		default:
			return decodedAudio{}, fmt.Errorf("unsupported WAV encoding: format %d, %d bits", formatTag, bits)
		}
	}
	return decodedAudio{samples: samples, sampleRate: rate, channels: channels}, nil
}

func normalizePCM(input decodedAudio) ([]byte, error) {
	if input.sampleRate <= 0 || input.channels <= 0 || len(input.samples) == 0 ||
		len(input.samples)%input.channels != 0 {
		return nil, errors.New("audio contains no valid samples")
	}
	sourceFrames := len(input.samples) / input.channels
	outputFrames := int(math.Ceil(float64(sourceFrames) * outputSampleRate / float64(input.sampleRate)))
	if outputFrames <= 0 || outputFrames > maxSoundFrames {
		return nil, errors.New("sound exceeds 5 minute duration limit")
	}

	output := make([]int16, outputFrames*2)
	for frame := 0; frame < outputFrames; frame++ {
		sourcePosition := float64(frame) * float64(input.sampleRate) / outputSampleRate
		leftFrame := min(int(sourcePosition), sourceFrames-1)
		rightFrame := min(leftFrame+1, sourceFrames-1)
		fraction := float32(sourcePosition - float64(leftFrame))
		for channel := 0; channel < 2; channel++ {
			left := channelSample(input, leftFrame, channel)
			right := channelSample(input, rightFrame, channel)
			sample := left + (right-left)*fraction
			if math.IsNaN(float64(sample)) || math.IsInf(float64(sample), 0) {
				sample = 0
			}
			sample = max(-1, min(1, sample))
			output[frame*2+channel] = int16(math.Round(float64(sample) * math.MaxInt16))
		}
	}
	return encodeWAV(output, outputSampleRate, 2), nil
}

func channelSample(input decodedAudio, frame, channel int) float32 {
	base := frame * input.channels
	if input.channels == 1 {
		return input.samples[base]
	}
	if input.channels == 2 {
		return input.samples[base+channel]
	}
	var sum float32
	for i := 0; i < input.channels; i++ {
		sum += input.samples[base+i]
	}
	return sum / float32(input.channels)
}
