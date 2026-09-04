//go:build !windows

package conference

func setBrowserWindowVisible(_ bool) error {
	return ErrBrowserWindowUnavailable
}

func isBrowserWindowVisible() bool {
	return false
}
