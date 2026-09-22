//go:build windows

package audio

import (
	"sync"

	"github.com/go-ole/go-ole"
)

const rpcEChangedMode = 0x80010106

var (
	comOnce sync.Once
	comErr  error
)

func ensureCOMInitialized() error {
	comOnce.Do(func() {
		err := ole.CoInitializeEx(0, ole.COINIT_MULTITHREADED)
		if err == nil {
			return
		}
		if oleErr, ok := err.(*ole.OleError); ok {
			switch oleErr.Code() {
			case 1: // S_FALSE — already initialized on this thread
				return
			case rpcEChangedMode:
				// Thread already initialized COM in a different apartment (e.g. conference webview).
				return
			}
		}
		comErr = err
	})
	return comErr
}

func ReleaseCOM() {
	ole.CoUninitialize()
}
