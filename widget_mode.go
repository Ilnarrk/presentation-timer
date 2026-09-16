package main

import (
	"github.com/wailsapp/wails/v2/pkg/runtime"

	"timer/internal/settings"
	"timer/internal/windowmode"
)

const (
	normalWindowMinWidth  = 820
	normalWindowMinHeight = 640
)

type windowBounds struct {
	x int
	y int
	w int
	h int
}

func (a *App) EnterWidgetMode() error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.ctx == nil {
		return nil
	}

	s := a.settings.Get()
	if !a.widgetMode {
		x, y := runtime.WindowGetPosition(a.ctx)
		w, h := runtime.WindowGetSize(a.ctx)
		a.normalBounds = windowBounds{x: x, y: y, w: w, h: h}
		a.normalMinW = normalWindowMinWidth
		a.normalMinH = normalWindowMinHeight
	}

	hwnd := windowmode.FindWindowByTitle(a.windowTitle)
	work := windowmode.WorkAreaForBounds(
		a.normalBounds.x,
		a.normalBounds.y,
		a.normalBounds.w,
		a.normalBounds.h,
	)
	wx, wy := windowmode.WidgetPosition(work, s.WidgetPlacement, s.WidgetFreeX, s.WidgetFreeY)

	windowmode.SetFrameless(hwnd, true)
	runtime.WindowSetMinSize(a.ctx, windowmode.WidgetWidth, windowmode.WidgetHeight)
	runtime.WindowSetSize(a.ctx, windowmode.WidgetWidth, windowmode.WidgetHeight)
	runtime.WindowSetPosition(a.ctx, wx, wy)
	runtime.WindowSetAlwaysOnTop(a.ctx, true)
	a.widgetMode = true
	runtime.EventsEmit(a.ctx, "window:widget", true)
	return nil
}

func (a *App) ExitWidgetMode() error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.ctx == nil || !a.widgetMode {
		return nil
	}

	s := a.settings.Get()
	x, y := runtime.WindowGetPosition(a.ctx)
	if settings.NormalizeWidgetPlacement(s.WidgetPlacement) == settings.WidgetPlacementFree {
		s.WidgetFreeX = x
		s.WidgetFreeY = y
		_ = a.settings.Save(s)
	}

	hwnd := windowmode.FindWindowByTitle(a.windowTitle)
	windowmode.SetFrameless(hwnd, false)

	minW := a.normalMinW
	minH := a.normalMinH
	if minW == 0 {
		minW = normalWindowMinWidth
	}
	if minH == 0 {
		minH = normalWindowMinHeight
	}
	runtime.WindowSetMinSize(a.ctx, minW, minH)
	runtime.WindowSetSize(a.ctx, a.normalBounds.w, a.normalBounds.h)
	runtime.WindowSetPosition(a.ctx, a.normalBounds.x, a.normalBounds.y)
	runtime.WindowSetAlwaysOnTop(a.ctx, true)
	a.widgetMode = false
	runtime.EventsEmit(a.ctx, "window:widget", false)
	return nil
}

func (a *App) IsWidgetMode() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.widgetMode
}
