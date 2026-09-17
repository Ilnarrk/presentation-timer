//go:build windows

package audio

import (
	"context"
	"encoding/binary"
	"fmt"
	"sync"
	"time"
	"unsafe"

	"github.com/go-ole/go-ole"
	"github.com/moutend/go-wca/pkg/wca"
)

func attachPlatformPlayback(player *Player) {
	output := newLocalAudioOutput()
	player.playbackFn = output.Play
	player.warmupFn = output.Warmup
}

type localAudioOutput struct {
	mu      sync.Mutex
	comInit bool
}

func newLocalAudioOutput() *localAudioOutput {
	return &localAudioOutput{}
}

func (o *localAudioOutput) Warmup(deviceID string) error {
	silent := encodeWAV(make([]int16, outputSampleRate/20), outputSampleRate, 2)
	return o.Play(context.Background(), deviceID, silent)
}

func (o *localAudioOutput) Play(ctx context.Context, deviceID string, wav []byte) error {
	if len(wav) < 44 {
		return fmt.Errorf("invalid wav data")
	}

	format, err := wavFormat(wav)
	if err != nil {
		return err
	}
	pcm := wav[44:]

	o.mu.Lock()
	defer o.mu.Unlock()

	session, err := o.openSession(deviceID, format)
	if err != nil {
		return err
	}
	defer session.release()

	return session.playPCM(ctx, pcm, wav)
}

type wasapiSession struct {
	client       *wca.IAudioClient
	render       *wca.IAudioRenderClient
	bufferFrames uint32
	frameSize    int
}

func (o *localAudioOutput) openSession(deviceID string, format wca.WAVEFORMATEX) (*wasapiSession, error) {
	if err := o.initCOM(); err != nil {
		return nil, err
	}

	var enumerator *wca.IMMDeviceEnumerator
	if err := wca.CoCreateInstance(wca.CLSID_MMDeviceEnumerator, 0, wca.CLSCTX_ALL, wca.IID_IMMDeviceEnumerator, &enumerator); err != nil {
		return nil, err
	}

	device, err := resolveDevice(enumerator, deviceID)
	if err != nil {
		return nil, err
	}

	var client *wca.IAudioClient
	if err := device.Activate(wca.IID_IAudioClient, wca.CLSCTX_ALL, nil, &client); err != nil {
		return nil, err
	}

	var defaultPeriod wca.REFERENCE_TIME
	if err := client.GetDevicePeriod(&defaultPeriod, nil); err != nil || defaultPeriod <= 0 {
		defaultPeriod = 100000 // 10 ms fallback
	}

	if err := client.Initialize(
		wca.AUDCLNT_SHAREMODE_SHARED,
		wca.AUDCLNT_STREAMFLAGS_AUTOCONVERTPCM|wca.AUDCLNT_STREAMFLAGS_SRC_DEFAULT_QUALITY,
		defaultPeriod,
		0,
		&format,
		nil,
	); err != nil {
		return nil, err
	}

	var bufferFrames uint32
	if err := client.GetBufferSize(&bufferFrames); err != nil {
		return nil, err
	}

	var render *wca.IAudioRenderClient
	if err := client.GetService(wca.IID_IAudioRenderClient, &render); err != nil {
		return nil, err
	}

	return &wasapiSession{
		client:       client,
		render:       render,
		bufferFrames: bufferFrames,
		frameSize:    int(format.NBlockAlign),
	}, nil
}

func (s *wasapiSession) release() {
	if s.client != nil {
		_ = s.client.Stop()
	}
}

func (s *wasapiSession) playPCM(ctx context.Context, pcm []byte, wav []byte) error {
	if err := s.client.Start(); err != nil {
		return err
	}
	defer s.release()

	offset := 0
	for offset < len(pcm) {
		if err := ctx.Err(); err != nil {
			return err
		}

		var padding uint32
		if err := s.client.GetCurrentPadding(&padding); err != nil {
			return err
		}

		available := s.bufferFrames - padding
		if available == 0 {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(2 * time.Millisecond):
				continue
			}
		}

		framesToWrite := available
		bytesToWrite := int(framesToWrite) * s.frameSize
		if offset+bytesToWrite > len(pcm) {
			bytesToWrite = len(pcm) - offset
			framesToWrite = uint32(bytesToWrite / s.frameSize)
		}
		if framesToWrite == 0 {
			break
		}

		var data *byte
		if err := s.render.GetBuffer(framesToWrite, &data); err != nil {
			return err
		}

		target := unsafe.Slice(data, bytesToWrite)
		copy(target, pcm[offset:offset+bytesToWrite])

		if err := s.render.ReleaseBuffer(framesToWrite, 0); err != nil {
			return err
		}
		offset += bytesToWrite
	}

	if err := ctx.Err(); err != nil {
		return err
	}

	return s.waitForDrain(ctx, wavDuration(wav)+250*time.Millisecond)
}

func (s *wasapiSession) waitForDrain(ctx context.Context, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		var padding uint32
		if err := s.client.GetCurrentPadding(&padding); err != nil {
			return err
		}
		if padding == 0 {
			return nil
		}
		if time.Now().After(deadline) {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(2 * time.Millisecond):
		}
	}
}

func (o *localAudioOutput) initCOM() error {
	if o.comInit {
		return nil
	}
	if _, err := coInitializeMTA(); err != nil {
		return err
	}
	o.comInit = true
	return nil
}

func wavFormat(wav []byte) (wca.WAVEFORMATEX, error) {
	format := wca.WAVEFORMATEX{
		WFormatTag:     wca.WAVE_FORMAT_PCM,
		NChannels:      binary.LittleEndian.Uint16(wav[22:24]),
		NSamplesPerSec: binary.LittleEndian.Uint32(wav[24:28]),
		WBitsPerSample: binary.LittleEndian.Uint16(wav[34:36]),
	}
	if format.NChannels == 0 || format.WBitsPerSample == 0 || format.NSamplesPerSec == 0 {
		return format, fmt.Errorf("invalid wav format")
	}
	format.NBlockAlign = format.NChannels * format.WBitsPerSample / 8
	format.NAvgBytesPerSec = format.NSamplesPerSec * uint32(format.NBlockAlign)
	return format, nil
}

func coInitializeMTA() (needUninit bool, err error) {
	err = ole.CoInitializeEx(0, ole.COINIT_MULTITHREADED)
	if err == nil {
		return true, nil
	}
	if oleErr, ok := err.(*ole.OleError); ok && oleErr.Code() == 1 {
		return false, nil
	}
	return false, err
}
