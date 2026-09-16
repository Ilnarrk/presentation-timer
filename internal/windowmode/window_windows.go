//go:build windows

package windowmode

import (
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

const (
	gwlStyle   int32 = -16
	gwlExStyle int32 = -20

	wsOverlappedWindow = 0x00CF0000
	wsPopup            = 0x80000000
	wsVisible          = 0x10000000
	wsThickFrame       = 0x00040000
	wsExDlgModalFrame  = 0x00000001

	swpFrameChanged = 0x0020
	swpNoActivate   = 0x0010
	swpShowWindow   = 0x0040

	monitorDefaultToNearest = 2
)

var (
	user32                    = windows.NewLazySystemDLL("user32")
	dwmapi                    = windows.NewLazySystemDLL("dwmapi")
	procFindWindowW           = user32.NewProc("FindWindowW")
	procGetWindowLongPtrW     = user32.NewProc("GetWindowLongPtrW")
	procSetWindowLongPtrW     = user32.NewProc("SetWindowLongPtrW")
	procSetWindowPos          = user32.NewProc("SetWindowPos")
	procMonitorFromWindow     = user32.NewProc("MonitorFromWindow")
	procMonitorFromRect       = user32.NewProc("MonitorFromRect")
	procGetMonitorInfoW       = user32.NewProc("GetMonitorInfoW")
	procSystemParametersInfoW = user32.NewProc("SystemParametersInfoW")
	procDwmSetWindowAttribute = dwmapi.NewProc("DwmSetWindowAttribute")
)

type monitorInfo struct {
	cbSize    uint32
	rcMonitor rect
	rcWork    rect
	flags     uint32
}

type rect struct {
	left   int32
	top    int32
	right  int32
	bottom int32
}

type frameState struct {
	style   uintptr
	exStyle uintptr
}

var savedFrame frameState

func FindWindowByTitle(title string) uintptr {
	titlePtr, err := syscall.UTF16PtrFromString(title)
	if err != nil {
		return 0
	}
	hwnd, _, _ := procFindWindowW.Call(0, uintptr(unsafe.Pointer(titlePtr)))
	return hwnd
}

func WorkAreaForWindow(hwnd uintptr) WorkArea {
	if hwnd == 0 {
		return primaryWorkArea()
	}
	monitor, _, _ := procMonitorFromWindow.Call(hwnd, uintptr(monitorDefaultToNearest))
	return workAreaForMonitor(monitor)
}

func WorkAreaForBounds(x, y, width, height int) WorkArea {
	bounds := rect{
		left:   int32(x),
		top:    int32(y),
		right:  int32(x + max(width, 1)),
		bottom: int32(y + max(height, 1)),
	}
	monitor, _, _ := procMonitorFromRect.Call(
		uintptr(unsafe.Pointer(&bounds)),
		uintptr(monitorDefaultToNearest),
	)
	return workAreaForMonitor(monitor)
}

func workAreaForMonitor(monitor uintptr) WorkArea {
	if monitor == 0 {
		return primaryWorkArea()
	}
	info := monitorInfo{cbSize: uint32(unsafe.Sizeof(monitorInfo{}))}
	if ret, _, _ := procGetMonitorInfoW.Call(monitor, uintptr(unsafe.Pointer(&info))); ret == 0 {
		return primaryWorkArea()
	}
	return WorkArea{
		Left:   int(info.rcWork.left),
		Top:    int(info.rcWork.top),
		Right:  int(info.rcWork.right),
		Bottom: int(info.rcWork.bottom),
	}
}

func primaryWorkArea() WorkArea {
	const spiGetWorkArea = 0x0030
	var work rect
	if ret, _, _ := procSystemParametersInfoW.Call(
		uintptr(spiGetWorkArea),
		0,
		uintptr(unsafe.Pointer(&work)),
		0,
	); ret != 0 {
		return WorkArea{
			Left:   int(work.left),
			Top:    int(work.top),
			Right:  int(work.right),
			Bottom: int(work.bottom),
		}
	}
	return WorkArea{Left: 0, Top: 0, Right: 1920, Bottom: 1080}
}

func gwlParam(index int32) uintptr {
	return uintptr(int(index))
}

func SetFrameless(hwnd uintptr, frameless, resizable bool) {
	if hwnd == 0 {
		return
	}
	if frameless {
		savedFrame.style, _, _ = procGetWindowLongPtrW.Call(hwnd, gwlParam(gwlStyle))
		savedFrame.exStyle, _, _ = procGetWindowLongPtrW.Call(hwnd, gwlParam(gwlExStyle))
		newStyle := savedFrame.style&^uintptr(wsOverlappedWindow) | uintptr(wsPopup|wsVisible)
		if resizable {
			newStyle |= uintptr(wsThickFrame)
		}
		newExStyle := savedFrame.exStyle &^ uintptr(wsExDlgModalFrame)
		procSetWindowLongPtrW.Call(hwnd, gwlParam(gwlStyle), newStyle)
		procSetWindowLongPtrW.Call(hwnd, gwlParam(gwlExStyle), newExStyle)
	} else if savedFrame.style != 0 {
		procSetWindowLongPtrW.Call(hwnd, gwlParam(gwlStyle), savedFrame.style)
		procSetWindowLongPtrW.Call(hwnd, gwlParam(gwlExStyle), savedFrame.exStyle)
		savedFrame = frameState{}
	}
	procSetWindowPos.Call(hwnd, 0, 0, 0, 0, 0, uintptr(swpFrameChanged|swpNoActivate|swpShowWindow))
}

func SetRoundedCorners(hwnd uintptr, rounded bool) {
	if hwnd == 0 {
		return
	}
	const dwmwaWindowCornerPreference = 33
	preference := uint32(1) // DWMWCP_DONOTROUND
	if rounded {
		preference = 2 // DWMWCP_ROUND
	}
	procDwmSetWindowAttribute.Call(
		hwnd,
		uintptr(dwmwaWindowCornerPreference),
		uintptr(unsafe.Pointer(&preference)),
		unsafe.Sizeof(preference),
	)
}

func SetWidgetBorderHidden(hwnd uintptr, hidden bool) {
	if hwnd == 0 {
		return
	}
	const dwmwaBorderColor = 34
	const dwmwaNcRenderingPolicy = 2
	const dwmwaColorNone uint32 = 0xFFFFFFFE
	const dwmwaColorDefault uint32 = 0xFFFFFFFF
	const dwmNCRenderingUseWindowStyle uint32 = 0
	const dwmNCRenderingDisabled uint32 = 1
	color := dwmwaColorDefault
	ncRendering := dwmNCRenderingUseWindowStyle
	if hidden {
		color = dwmwaColorNone
		ncRendering = dwmNCRenderingDisabled
	}
	procDwmSetWindowAttribute.Call(
		hwnd,
		uintptr(dwmwaNcRenderingPolicy),
		uintptr(unsafe.Pointer(&ncRendering)),
		unsafe.Sizeof(ncRendering),
	)
	procDwmSetWindowAttribute.Call(
		hwnd,
		uintptr(dwmwaBorderColor),
		uintptr(unsafe.Pointer(&color)),
		unsafe.Sizeof(color),
	)
}
