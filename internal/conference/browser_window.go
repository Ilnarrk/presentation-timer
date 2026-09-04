package conference

import "errors"

var ErrBrowserWindowUnavailable = errors.New("окно браузера ВКС недоступно")

func SetBrowserWindowVisible(visible bool) error {
	return setBrowserWindowVisible(visible)
}

func IsBrowserWindowVisible() bool {
	return isBrowserWindowVisible()
}
