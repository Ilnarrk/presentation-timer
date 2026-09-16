package windowmode

import "timer/internal/settings"

const (
	WidgetWidth  = 320
	WidgetHeight = 150
	WidgetMargin = 12
)

type WorkArea struct {
	Left   int
	Top    int
	Right  int
	Bottom int
}

func WidgetPosition(workArea WorkArea, placement string, freeX, freeY int) (int, int) {
	if workArea.Right-workArea.Left < WidgetWidth || workArea.Bottom-workArea.Top < WidgetHeight {
		workArea = WorkArea{Left: 0, Top: 0, Right: 1920, Bottom: 1080}
	}
	w, h := WidgetWidth, WidgetHeight
	switch settings.NormalizeWidgetPlacement(placement) {
	case settings.WidgetPlacementTopLeft:
		return workArea.Left + WidgetMargin, workArea.Top + WidgetMargin
	case settings.WidgetPlacementFree:
		x, y := freeX, freeY
		if x == 0 && y == 0 {
			x = workArea.Right - w - WidgetMargin
			y = workArea.Top + WidgetMargin
		}
		return ClampPosition(workArea, w, h, x, y)
	default:
		return workArea.Right - w - WidgetMargin, workArea.Top + WidgetMargin
	}
}

func ClampPosition(workArea WorkArea, width, height, x, y int) (int, int) {
	if x < workArea.Left {
		x = workArea.Left
	}
	if y < workArea.Top {
		y = workArea.Top
	}
	if x+width > workArea.Right {
		x = workArea.Right - width
	}
	if y+height > workArea.Bottom {
		y = workArea.Bottom - height
	}
	return x, y
}
