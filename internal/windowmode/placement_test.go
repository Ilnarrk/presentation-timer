package windowmode

import (
	"testing"

	"timer/internal/settings"
)

func TestWidgetPositionCorners(t *testing.T) {
	work := WorkArea{Left: 0, Top: 0, Right: 1920, Bottom: 1080}

	x, y := WidgetPosition(work, settings.WidgetPlacementTopRight, 0, 0)
	if x != 1920-WidgetWidth-WidgetMargin || y != WidgetMargin {
		t.Fatalf("top-right: got %d,%d", x, y)
	}

	x, y = WidgetPosition(work, settings.WidgetPlacementTopLeft, 0, 0)
	if x != WidgetMargin || y != WidgetMargin {
		t.Fatalf("top-left: got %d,%d", x, y)
	}

	x, y = WidgetPosition(work, settings.WidgetPlacementTopCenter, 0, 0)
	if x != (1920-WidgetWidth)/2 || y != WidgetMargin {
		t.Fatalf("top-center: got %d,%d", x, y)
	}
}

func TestWidgetPositionFreeDefaultsToTopRight(t *testing.T) {
	work := WorkArea{Left: 100, Top: 50, Right: 1900, Bottom: 1050}
	x, y := WidgetPosition(work, settings.WidgetPlacementFree, 0, 0)
	wantX := 1900 - WidgetWidth - WidgetMargin
	wantY := 50 + WidgetMargin
	if x != wantX || y != wantY {
		t.Fatalf("free default: got %d,%d want %d,%d", x, y, wantX, wantY)
	}
}

func TestClampPosition(t *testing.T) {
	work := WorkArea{Left: 0, Top: 0, Right: 400, Bottom: 300}
	x, y := ClampPosition(work, WidgetWidth, WidgetHeight, 500, 500)
	if x != 400-WidgetWidth || y != 300-WidgetHeight {
		t.Fatalf("clamp overflow: got %d,%d", x, y)
	}
}

func TestWidgetPositionFallsBackForInvalidWorkArea(t *testing.T) {
	x, y := WidgetPosition(WorkArea{}, settings.WidgetPlacementTopRight, 0, 0)
	if x < 0 || y < 0 {
		t.Fatalf("invalid work area must not place widget off-screen: got %d,%d", x, y)
	}
}

func TestWidgetDefaultAndMinimumSizes(t *testing.T) {
	if WidgetWidth != 400 || WidgetHeight != 120 {
		t.Fatalf("unexpected default widget size: %dx%d", WidgetWidth, WidgetHeight)
	}
	if WidgetMinWidth != 360 || WidgetMinHeight != 88 {
		t.Fatalf("unexpected minimum widget size: %dx%d", WidgetMinWidth, WidgetMinHeight)
	}
	if WidgetMaxWidth != 800 || WidgetMaxHeight != 320 {
		t.Fatalf("unexpected maximum widget size: %dx%d", WidgetMaxWidth, WidgetMaxHeight)
	}
}
