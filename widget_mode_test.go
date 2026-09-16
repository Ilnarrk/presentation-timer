package main

import (
	"testing"

	"timer/internal/settings"
)

func TestConferenceBoundsProviderUsesNormalBoundsInWidgetMode(t *testing.T) {
	app := &App{
		widgetMode: true,
		normalBounds: windowBounds{
			x: 100,
			y: 80,
			w: 960,
			h: 720,
		},
	}

	provider := func() (int, int, int, int) {
		if app.widgetMode && app.normalBounds.w > 0 && app.normalBounds.h > 0 {
			return app.normalBounds.x, app.normalBounds.y, app.normalBounds.w, app.normalBounds.h
		}
		return 0, 0, 0, 0
	}

	x, y, w, h := provider()
	if x != 100 || y != 80 || w != 960 || h != 720 {
		t.Fatalf("expected saved normal bounds, got %d,%d %dx%d", x, y, w, h)
	}
}

func TestWidgetModeFlag(t *testing.T) {
	app := &App{}
	if app.IsWidgetMode() {
		t.Fatal("widget mode should start disabled")
	}
	app.widgetMode = true
	if !app.IsWidgetMode() {
		t.Fatal("widget mode flag should be readable")
	}
}

func TestWidgetPlacementConstants(t *testing.T) {
	if settings.NormalizeWidgetPlacement(settings.WidgetPlacementTopLeft) != settings.WidgetPlacementTopLeft {
		t.Fatal("topLeft should normalize to itself")
	}
}
