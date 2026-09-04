//go:build windows

package conference

import (
	_ "embed"
	"os"
	"path/filepath"
	"sync"
	"unsafe"

	"golang.org/x/sys/windows"
)

// App icon copy of build/windows/icon.ico — go:embed cannot read parent directories.
//
//go:embed icon.ico
var conferenceIconICO []byte

const (
	imageIcon       = 1
	lrLoadFromFile  = 0x00000010
	lrDefaultSize   = 0x00000040
)

var (
	procLoadImageW = user32.NewProc("LoadImageW")

	embeddedIconPathOnce sync.Once
	embeddedIconPath     string
	conferenceIconOnce   sync.Once
	conferenceIcon       uintptr
)

func loadConferenceWindowIcon() uintptr {
	conferenceIconOnce.Do(func() {
		conferenceIcon = loadEmbeddedIcon()
		if conferenceIcon == 0 {
			conferenceIcon = loadModuleIcon()
		}
	})
	return conferenceIcon
}

func loadModuleIcon() uintptr {
	instance, _, _ := procGetModuleHandleW.Call(0)
	icon, _, _ := procLoadIconW.Call(instance, iconResourceID)
	return icon
}

func embeddedIconFilePath() string {
	embeddedIconPathOnce.Do(func() {
		if len(conferenceIconICO) == 0 {
			return
		}
		dir, err := os.MkdirTemp("", "presentation-timer-icon-")
		if err != nil {
			return
		}
		path := filepath.Join(dir, "icon.ico")
		if err := os.WriteFile(path, conferenceIconICO, 0o600); err != nil {
			_ = os.RemoveAll(dir)
			return
		}
		embeddedIconPath = path
	})
	return embeddedIconPath
}

func loadEmbeddedIcon() uintptr {
	path := embeddedIconFilePath()
	if path == "" {
		return 0
	}
	pathPtr, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return 0
	}

	icon, _, _ := procLoadImageW.Call(
		0,
		uintptr(unsafe.Pointer(pathPtr)),
		imageIcon,
		0,
		0,
		lrLoadFromFile|lrDefaultSize,
	)
	return icon
}
