package conference

import "errors"

var ErrBrowserWindowUnavailable = errors.New("окно браузера ВКС недоступно")

func SetBrowserWindowVisible(visible bool) error {
	return setBrowserWindowVisible(visible)
}

func IsBrowserWindowVisible() bool {
	return isBrowserWindowVisible()
}

func SetMainWindowRaiseHandler(fn func()) {
	setMainWindowRaiseHandler(fn)
}

func SetMainWindowBoundsProvider(fn func() (x, y, width, height int)) {
	setMainWindowBoundsProvider(fn)
}

func SetMainWindowMoveHandler(fn func(x, y int)) {
	setMainWindowMoveHandler(fn)
}
