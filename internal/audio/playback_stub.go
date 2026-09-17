//go:build !windows

package audio

import (
	"context"
	"errors"
)

func ListDevices() ([]Device, error) {
	return []Device{{ID: "default", Name: "Default output"}}, nil
}

func attachPlatformPlayback(player *Player) {
	player.playbackFn = func(context.Context, string, []byte) error {
		return errors.New("audio playback is only supported on Windows")
	}
}
