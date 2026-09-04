//go:build windows

package conference

import (
	"errors"
	"unsafe"

	"github.com/wailsapp/go-webview2/pkg/edge"
	"golang.org/x/sys/windows"
)

type comObject struct {
	vtbl *comVtbl
}

type comVtbl struct {
	queryInterface edge.ComProc
	addRef         edge.ComProc
	release        edge.ComProc
}

type iCoreWebView2_8 struct {
	vtbl *iCoreWebView2_8Vtbl
}

type iCoreWebView2_8Vtbl struct {
	queryInterface                      edge.ComProc
	addRef                              edge.ComProc
	release                             edge.ComProc
	addIsMutedChanged                   edge.ComProc
	removeIsMutedChanged                edge.ComProc
	getIsMuted                          edge.ComProc
	putIsMuted                          edge.ComProc
	addIsDocumentPlayingAudioChanged    edge.ComProc
	removeIsDocumentPlayingAudioChanged edge.ComProc
	getIsDocumentPlayingAudio           edge.ComProc
}

func queryCoreWebView2_8(webview *edge.ICoreWebView2) (*iCoreWebView2_8, error) {
	if webview == nil {
		return nil, errors.New("webview not initialized")
	}
	obj := (*comObject)(unsafe.Pointer(webview))
	if obj.vtbl == nil {
		return nil, errors.New("webview vtbl missing")
	}
	var result *iCoreWebView2_8
	iid := edge.NewGUID("{E9632730-6E1E-43AB-B7B8-7B2C9E62E094}")
	hr, _, _ := obj.vtbl.queryInterface.Call(
		uintptr(unsafe.Pointer(webview)),
		uintptr(unsafe.Pointer(iid)),
		uintptr(unsafe.Pointer(&result)),
	)
	if windows.Handle(hr) != windows.S_OK {
		return nil, windows.Errno(hr)
	}
	if result == nil {
		return nil, errors.New("ICoreWebView2_8 not available")
	}
	return result, nil
}

func (i *iCoreWebView2_8) putIsMuted(value bool) error {
	var flag int32
	if value {
		flag = 1
	}
	hr, _, _ := i.vtbl.putIsMuted.Call(
		uintptr(unsafe.Pointer(i)),
		uintptr(flag),
	)
	if windows.Handle(hr) != windows.S_OK {
		return windows.Errno(hr)
	}
	return nil
}

func setWebViewOutputMuted(chromium *edge.Chromium, muted bool) error {
	if chromium == nil {
		return errors.New("webview not initialized")
	}
	if !chromium.HasCapability(edge.IsMuted) {
		return errors.New("IsMuted not supported by WebView2 runtime")
	}
	controller := chromium.GetController()
	if controller == nil {
		return errors.New("webview controller not initialized")
	}
	webview, err := controller.GetCoreWebView2()
	if err != nil {
		return err
	}
	wv8, err := queryCoreWebView2_8(webview)
	if err != nil {
		return err
	}
	return wv8.putIsMuted(muted)
}

func setConferenceWebViewOutputMuted(muted bool) error {
	activeBrowserWindowMu.Lock()
	session := activeBrowserWindow
	activeBrowserWindowMu.Unlock()
	if session == nil || session.chromium == nil {
		return ErrBrowserWindowUnavailable
	}
	return setWebViewOutputMuted(session.chromium, muted)
}
