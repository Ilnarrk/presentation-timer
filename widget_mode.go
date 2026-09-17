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

func (a *App) setWindowPositionAbsolute(absX, absY int) {
	if a.ctx == nil {
		return
	}
	hwnd := windowmode.FindWindowByTitle(a.windowTitle)
	work := windowmode.WorkAreaForWindow(hwnd)
	relX, relY := windowmode.ToWailsPosition(work, absX, absY)
	runtime.WindowSetPosition(a.ctx, relX, relY)
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
			widgetWidth = min(max(s.WidgetFreeWidth, windowmode.WidgetMinWidth), windowmode.WidgetMaxWidth)
		}
		if s.WidgetFreeHeight > 0 {
			widgetHeight = min(max(s.WidgetFreeHeight, windowmode.WidgetMinHeight), windowmode.WidgetMaxHeight)
		}
		widgetWidth = min(widgetWidth, work.Right-work.Left)
		widgetHeight = min(widgetHeight, work.Bottom-work.Top)
	}
	wx, wy := windowmode.WidgetPositionForSize(work, s.WidgetPlacement, s.WidgetFreeX, s.WidgetFreeY, widgetWidth, widgetHeight)

	windowmode.SetFrameless(hwnd, true, freePlacement)
	// CSS border-radius handles widget shape; DWM rounding causes a double border.
	windowmode.SetRoundedCorners(hwnd, false)
	windowmode.SetWidgetBorderHidden(hwnd, true)
	if freePlacement {
		runtime.WindowSetMinSize(a.ctx, windowmode.WidgetMinWidth, windowmode.WidgetMinHeight)
		runtime.WindowSetMaxSize(a.ctx, windowmode.WidgetMaxWidth, windowmode.WidgetMaxHeight)
	} else {
		runtime.WindowSetMinSize(a.ctx, widgetWidth, widgetHeight)
		runtime.WindowSetMaxSize(a.ctx, widgetWidth, widgetHeight)
	}
	runtime.WindowSetSize(a.ctx, widgetWidth, widgetHeight)
	a.setWindowPositionAbsolute(wx, wy)
	runtime.WindowSetAlwaysOnTop(a.ctx, true)
	a.widgetMode = true
	a.widgetQuickTimeOpen = false
	a.widgetCompactBounds = windowBounds{w: widgetWidth, h: widgetHeight}
	runtime.EventsEmit(a.ctx, "window:widget", true)
	return nil
}

func (a *App) SetWidgetQuickTimeOpen(open bool) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.ctx == nil || !a.widgetMode || a.widgetQuickTimeOpen == open {
		return nil
	}

	hwnd := windowmode.FindWindowByTitle(a.windowTitle)
	work := windowmode.WorkAreaForWindow(hwnd)
	x, y := runtime.WindowGetPosition(a.ctx)
	w, h := runtime.WindowGetSize(a.ctx)
	freePlacement := settings.NormalizeWidgetPlacement(a.settings.Get().WidgetPlacement) == settings.WidgetPlacementFree

	if open {
		a.widgetCompactBounds = windowBounds{w: w, h: h}
		x, y, targetHeight := windowmode.QuickTimePanelPosition(work, x, y, w, h)
		if freePlacement {
			runtime.WindowSetMinSize(a.ctx, windowmode.WidgetMinWidth, windowmode.WidgetMinHeight)
			runtime.WindowSetMaxSize(a.ctx, windowmode.WidgetMaxWidth, windowmode.WidgetMaxHeight)
		} else {
			runtime.WindowSetMinSize(a.ctx, w, targetHeight)
			runtime.WindowSetMaxSize(a.ctx, w, targetHeight)
		}
		runtime.WindowSetSize(a.ctx, w, targetHeight)
		a.setWindowPositionAbsolute(x, y)
	} else {
		compact := a.widgetCompactBounds
		if compact.w <= 0 || compact.h <= 0 {
			compact = windowBounds{w: windowmode.WidgetWidth, h: windowmode.WidgetHeight}
		}
		if freePlacement {
			runtime.WindowSetMinSize(a.ctx, windowmode.WidgetMinWidth, windowmode.WidgetMinHeight)
			runtime.WindowSetMaxSize(a.ctx, windowmode.WidgetMaxWidth, windowmode.WidgetMaxHeight)
		} else {
			runtime.WindowSetMinSize(a.ctx, compact.w, compact.h)
			runtime.WindowSetMaxSize(a.ctx, compact.w, compact.h)
		}
		runtime.WindowSetSize(a.ctx, compact.w, compact.h)
		x, y = windowmode.ClampPosition(work, compact.w, compact.h, x, y)
		a.setWindowPositionAbsolute(x, y)
	}

	a.widgetQuickTimeOpen = open
	return nil
}

func (a *App) saveWidgetBounds() {
	if a.ctx == nil || a.settings == nil {
		return
	}
	s := a.settings.Get()
	if settings.NormalizeWidgetPlacement(s.WidgetPlacement) != settings.WidgetPlacementFree {
		return
	}
	x, y := runtime.WindowGetPosition(a.ctx)
	w, h := runtime.WindowGetSize(a.ctx)
	if a.widgetQuickTimeOpen && a.widgetCompactBounds.w > 0 && a.widgetCompactBounds.h > 0 {
		w = a.widgetCompactBounds.w
		h = a.widgetCompactBounds.h
	}
	s.WidgetFreeX = x
	s.WidgetFreeY = y
	s.WidgetFreeWidth = min(max(w, windowmode.WidgetMinWidth), windowmode.WidgetMaxWidth)
	s.WidgetFreeHeight = min(max(h, windowmode.WidgetMinHeight), windowmode.WidgetMaxHeight)
	_ = a.settings.Save(s)
}

func (a *App) saveMainWindowBounds() {
	if a.ctx == nil || a.settings == nil {
		return
	}
	s := a.settings.Get()
	a.mu.Lock()
	widgetMode := a.widgetMode
	bounds := a.normalBounds
	a.mu.Unlock()

	if widgetMode && bounds.w > 0 && bounds.h > 0 {
		s.MainWindowX = bounds.x
		s.MainWindowY = bounds.y
		s.MainWindowWidth = bounds.w
		s.MainWindowHeight = bounds.h
		_ = a.settings.Save(s)
		return
	}
	if widgetMode {
		return
	}
	x, y := runtime.WindowGetPosition(a.ctx)
	w, h := runtime.WindowGetSize(a.ctx)
	if w <= 0 || h <= 0 {
		return
	}
	s.MainWindowX = x
	s.MainWindowY = y
	s.MainWindowWidth = w
	s.MainWindowHeight = h
	_ = a.settings.Save(s)
}

func (a *App) restoreMainWindowBounds() {
	if a.ctx == nil || a.settings == nil {
		return
	}
	s := a.settings.Get()
	if s.MainWindowWidth <= 0 || s.MainWindowHeight <= 0 {
		return
	}
	work := windowmode.WorkAreaForBounds(s.MainWindowX, s.MainWindowY, s.MainWindowWidth, s.MainWindowHeight)
	width := min(max(s.MainWindowWidth, normalWindowMinWidth), work.Right-work.Left)
	height := min(max(s.MainWindowHeight, normalWindowMinHeight), work.Bottom-work.Top)
	x, y := windowmode.ClampPosition(work, width, height, s.MainWindowX, s.MainWindowY)
	runtime.WindowSetSize(a.ctx, width, height)
	a.setWindowPositionAbsolute(x, y)
}

func (a *App) ExitWidgetMode() error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.ctx == nil || !a.widgetMode {
		return nil
	}

	a.saveWidgetBounds()

	hwnd := windowmode.FindWindowByTitle(a.windowTitle)
	windowmode.SetFrameless(hwnd, false, false)
	windowmode.SetRoundedCorners(hwnd, true)
	windowmode.SetWidgetBorderHidden(hwnd, false)

	minW := a.normalMinW
	minH := a.normalMinH
	if minW == 0 {
		minW = normalWindowMinWidth
	}
	if minH == 0 {
		minH = normalWindowMinHeight
	}
	runtime.WindowSetMaxSize(a.ctx, 0, 0)
	runtime.WindowSetMinSize(a.ctx, minW, minH)
	runtime.WindowSetSize(a.ctx, a.normalBounds.w, a.normalBounds.h)
	a.setWindowPositionAbsolute(a.normalBounds.x, a.normalBounds.y)
	runtime.WindowSetAlwaysOnTop(a.ctx, true)
	a.widgetMode = false
	a.widgetQuickTimeOpen = false
	runtime.EventsEmit(a.ctx, "window:widget", false)
	return nil
}

func (a *App) IsWidgetMode() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.widgetMode
}
