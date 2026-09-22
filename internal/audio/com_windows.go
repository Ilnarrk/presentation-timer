//go:build windows

package audio

import (
	"github.com/go-ole/go-ole"
)

const rpcEChangedMode = 0x80010106

func ensureCOMInitialized() error {
	err := ole.CoInitializeEx(0, ole.COINIT_MULTITHREADED)
	if err == nil {
		return nil
	}
	if oleErr, ok := err.(*ole.OleError); ok {
		switch oleErr.Code() {
		case 1: // S_FALSE — already initialized on this thread
			return nil
		case rpcEChangedMode:
			// Thread already initialized COM in a different apartment (e.g. conference webview).
			return nil
		}
	}
	return err
}

func ReleaseCOM() {
	ole.CoUninitialize()
}
