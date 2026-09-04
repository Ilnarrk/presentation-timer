//go:build !windows

package audio

import (
	"context"
	"errors"
)

func ListDevices() ([]Device, error) {
	return []Device{{ID: "default", Name: "Default output"}}, nil
}

func playWAV(ctx context.Context, deviceID string, wav []byte) error {
	return errors.New("audio playback is only supported on Windows")
}
