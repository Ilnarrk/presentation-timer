//go:build !windows

package windowmode

func FindWindowByTitle(_ string) uintptr {
	return 0
}

func WorkAreaForWindow(_ uintptr) WorkArea {
	return WorkArea{Left: 0, Top: 0, Right: 1920, Bottom: 1080}
}

func WorkAreaForCurrentWindow(hwnd uintptr) WorkArea {
	return WorkAreaForWindow(hwnd)
}

func WindowBounds(_ uintptr) (x, y, w, h int) {
	return 0, 0, 0, 0
}

func WorkAreaForBounds(_, _, _, _ int) WorkArea {
	return WorkArea{Left: 0, Top: 0, Right: 1920, Bottom: 1080}
}

func SetFrameless(_ uintptr, _, _ bool) {}

func SetRoundedCorners(_ uintptr, _ bool) {}

func SetWidgetBorderHidden(_ uintptr, _ bool) {}
