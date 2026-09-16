package windowmode

import "timer/internal/settings"

const (
	WidgetWidth     = 400
	WidgetHeight    = 120
	WidgetMinWidth  = 280
	WidgetMinHeight = 88
	WidgetMargin    = 12
)

type WorkArea struct {
	Left   int
	Top    int
	Right  int
	Bottom int
}

func WidgetPosition(workArea WorkArea, placement string, freeX, freeY int) (int, int) {
	return WidgetPositionForSize(workArea, placement, freeX, freeY, WidgetWidth, WidgetHeight)
}

func WidgetPositionForSize(workArea WorkArea, placement string, freeX, freeY, width, height int) (int, int) {
	if workArea.Right-workArea.Left < width || workArea.Bottom-workArea.Top < height {
		workArea = WorkArea{Left: 0, Top: 0, Right: 1920, Bottom: 1080}
	}
	switch settings.NormalizeWidgetPlacement(placement) {
	case settings.WidgetPlacementTopLeft:
		return workArea.Left + WidgetMargin, workArea.Top + WidgetMargin
	case settings.WidgetPlacementTopCenter:
		return workArea.Left + (workArea.Right-workArea.Left-width)/2, workArea.Top + WidgetMargin
	case settings.WidgetPlacementFree:
		x, y := freeX, freeY
		if x == 0 && y == 0 {
			x = workArea.Right - width - WidgetMargin
			y = workArea.Top + WidgetMargin
		}
		return ClampPosition(workArea, width, height, x, y)
	default:
		return workArea.Right - width - WidgetMargin, workArea.Top + WidgetMargin
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
