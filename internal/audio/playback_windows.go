//go:build windows

package audio

import (
	"encoding/binary"
	"fmt"
	"time"
	"unsafe"

	"github.com/go-ole/go-ole"
	"github.com/moutend/go-wca/pkg/wca"
)

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

func playWAV(deviceID string, wav []byte) error {
	if len(wav) < 44 {
		return fmt.Errorf("invalid wav data")
	}

	pcm := append([]byte(nil), wav[44:]...)
	format := &wca.WAVEFORMATEX{
		WFormatTag:     wca.WAVE_FORMAT_PCM,
		NChannels:      binary.LittleEndian.Uint16(wav[22:24]),
		NSamplesPerSec: binary.LittleEndian.Uint32(wav[24:28]),
		WBitsPerSample: binary.LittleEndian.Uint16(wav[34:36]),
	}
	if format.NChannels == 0 || format.WBitsPerSample == 0 || format.NSamplesPerSec == 0 {
		return fmt.Errorf("invalid wav format")
	}
	format.NBlockAlign = format.NChannels * format.WBitsPerSample / 8
	format.NAvgBytesPerSec = format.NSamplesPerSec * uint32(format.NBlockAlign)

	if needUninit, err := coInitializeMTA(); err != nil {
		return err
	} else if needUninit {
		defer ole.CoUninitialize()
	}

	var enumerator *wca.IMMDeviceEnumerator
	if err := wca.CoCreateInstance(wca.CLSID_MMDeviceEnumerator, 0, wca.CLSCTX_ALL, wca.IID_IMMDeviceEnumerator, &enumerator); err != nil {
		return err
	}

	device, err := resolveDevice(enumerator, deviceID)
	if err != nil {
		return err
	}

	var client *wca.IAudioClient
	if err := device.Activate(wca.IID_IAudioClient, wca.CLSCTX_ALL, nil, &client); err != nil {
		return err
	}

	bufferDuration := wca.REFERENCE_TIME(10000000)
	if err := client.Initialize(
		wca.AUDCLNT_SHAREMODE_SHARED,
		wca.AUDCLNT_STREAMFLAGS_AUTOCONVERTPCM|wca.AUDCLNT_STREAMFLAGS_SRC_DEFAULT_QUALITY,
		bufferDuration,
		0,
		format,
		nil,
	); err != nil {
		return err
	}

	var bufferFrames uint32
	if err := client.GetBufferSize(&bufferFrames); err != nil {
		return err
	}

	var render *wca.IAudioRenderClient
	if err := client.GetService(wca.IID_IAudioRenderClient, &render); err != nil {
		return err
	}

	if err := client.Start(); err != nil {
		return err
	}
	defer client.Stop()

	frameSize := int(format.NBlockAlign)
	offset := 0

	for offset < len(pcm) {
		var padding uint32
		if err := client.GetCurrentPadding(&padding); err != nil {
			return err
		}

		available := bufferFrames - padding
		if available == 0 {
			time.Sleep(5 * time.Millisecond)
			continue
		}

		framesToWrite := available
		bytesToWrite := int(framesToWrite) * frameSize
		if offset+bytesToWrite > len(pcm) {
			bytesToWrite = len(pcm) - offset
			framesToWrite = uint32(bytesToWrite / frameSize)
		}
		if framesToWrite == 0 {
			break
		}

		var data *byte
		if err := render.GetBuffer(framesToWrite, &data); err != nil {
			return err
		}

		target := unsafe.Slice(data, bytesToWrite)
		copy(target, pcm[offset:offset+bytesToWrite])

		if err := render.ReleaseBuffer(framesToWrite, 0); err != nil {
			return err
		}
		offset += bytesToWrite
	}

	time.Sleep(wavDuration(wav) + 120*time.Millisecond)
	return nil
}
