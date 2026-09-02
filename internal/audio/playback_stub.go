//go:build !windows

package audio

import "errors"

func ListDevices() ([]Device, error) {
	return []Device{{ID: "default", Name: "Default output"}}, nil
}

func playWAV(deviceID string, wav []byte) error {
	return errors.New("audio playback is only supported on Windows")
}
