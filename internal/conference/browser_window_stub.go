//go:build !windows

package conference

func setBrowserWindowVisible(_ bool) error {
	return ErrBrowserWindowUnavailable
}

func isBrowserWindowVisible() bool {
	return false
}

func setMainWindowRaiseHandler(_ func()) {}

func setMainWindowBoundsProvider(_ func() (int, int, int, int)) {}

func setMainWindowMoveHandler(_ func(int, int)) {}
