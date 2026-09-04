//go:build windows

package conference

import (
	"sync"
	"sync/atomic"
)

var (
	activeBrowserWindowMu      sync.Mutex
	activeBrowserWindow        *conferenceSession
	mainWindowRaiseHandler     func()
	mainWindowBoundsProvider   func() (int, int, int, int)
	mainWindowMoveHandler      func(int, int)
)

func setMainWindowRaiseHandler(fn func()) {
	mainWindowRaiseHandler = fn
}

func setMainWindowBoundsProvider(fn func() (int, int, int, int)) {
	mainWindowBoundsProvider = fn
}

func getMainWindowBounds() (int, int, int, int) {
	if mainWindowBoundsProvider == nil {
		return 0, 0, 0, 0
	}
	return mainWindowBoundsProvider()
}

func setMainWindowMoveHandler(fn func(int, int)) {
	mainWindowMoveHandler = fn
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
	alignConferenceWindow(atomic.LoadUintptr(&session.hwnd))
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
	if visible {
		alignConferenceWindow(atomic.LoadUintptr(&session.hwnd))
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
