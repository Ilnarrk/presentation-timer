//go:build windows

package conference

import (
	"sync"
	"sync/atomic"
)

var (
	activeBrowserWindowMu sync.Mutex
	activeBrowserWindow   *conferenceSession
	mainWindowRaiseHandler func()
)

func setMainWindowRaiseHandler(fn func()) {
	mainWindowRaiseHandler = fn
}

func raiseMainWindow() {
	if mainWindowRaiseHandler != nil {
		mainWindowRaiseHandler()
	}
}

func registerBrowserWindow(session *conferenceSession) {
	if session == nil {
		return
	}
	session.visible.Store(true)
	activeBrowserWindowMu.Lock()
	activeBrowserWindow = session
	activeBrowserWindowMu.Unlock()
}

func unregisterBrowserWindow(session *conferenceSession) {
	if session == nil {
		return
	}
	activeBrowserWindowMu.Lock()
	if activeBrowserWindow == session {
		activeBrowserWindow = nil
	}
	activeBrowserWindowMu.Unlock()
}

func setBrowserWindowVisible(visible bool) error {
	activeBrowserWindowMu.Lock()
	session := activeBrowserWindow
	activeBrowserWindowMu.Unlock()
	if session == nil || atomic.LoadUintptr(&session.hwnd) == 0 {
		return ErrBrowserWindowUnavailable
	}
	session.setVisible(visible)
	return nil
}

func isBrowserWindowVisible() bool {
	activeBrowserWindowMu.Lock()
	session := activeBrowserWindow
	activeBrowserWindowMu.Unlock()
	if session == nil {
		return false
	}
	return session.visible.Load()
}
