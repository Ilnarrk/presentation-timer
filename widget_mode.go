package main

import (
	"github.com/wailsapp/wails/v2/pkg/runtime"

	"timer/internal/settings"
	"timer/internal/windowmode"
)

const (
	normalWindowMinWidth  = 800
	normalWindowMinHeight = 600
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
	freePlacement := settings.NormalizeWidgetPlacement(s.WidgetPlacement) == settings.WidgetPlacementFree
	widgetWidth, widgetHeight := windowmode.WidgetWidth, windowmode.WidgetHeight
	if freePlacement {
		if s.WidgetFreeWidth > 0 {
			widgetWidth = max(s.WidgetFreeWidth, windowmode.WidgetMinWidth)
		}
		if s.WidgetFreeHeight > 0 {
			widgetHeight = max(s.WidgetFreeHeight, windowmode.WidgetMinHeight)
		}
		widgetWidth = min(widgetWidth, work.Right-work.Left)
		widgetHeight = min(widgetHeight, work.Bottom-work.Top)
	}
	wx, wy := windowmode.WidgetPositionForSize(work, s.WidgetPlacement, s.WidgetFreeX, s.WidgetFreeY, widgetWidth, widgetHeight)

	windowmode.SetFrameless(hwnd, true, freePlacement)
	windowmode.SetRoundedCorners(hwnd, s.WidgetShape != settings.WidgetShapeRectangular)
	if freePlacement {
		runtime.WindowSetMinSize(a.ctx, windowmode.WidgetMinWidth, windowmode.WidgetMinHeight)
	} else {
		runtime.WindowSetMinSize(a.ctx, widgetWidth, widgetHeight)
	}
	runtime.WindowSetSize(a.ctx, widgetWidth, widgetHeight)
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
		w, h := runtime.WindowGetSize(a.ctx)
		s.WidgetFreeX = x
		s.WidgetFreeY = y
		s.WidgetFreeWidth = w
		s.WidgetFreeHeight = h
		_ = a.settings.Save(s)
	}

	hwnd := windowmode.FindWindowByTitle(a.windowTitle)
	windowmode.SetFrameless(hwnd, false, false)
	windowmode.SetRoundedCorners(hwnd, true)

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
