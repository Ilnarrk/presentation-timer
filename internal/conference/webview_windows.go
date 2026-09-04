//go:build windows

package conference

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
	"unsafe"

	"github.com/wailsapp/go-webview2/pkg/edge"
	"golang.org/x/sys/windows"

	"timer/internal/buildinfo"
)

const (
	conferenceWindowClass = "PresentationTimerConferenceWebView"

	wmDestroy       = 0x0002
	wmSize          = 0x0005
	wmClose         = 0x0010
	wmSetIcon       = 0x0080
	wmAppSetVisible = 0x8001
	sizeMinimized   = 1
	swHide          = 0
	swShow          = 5
	csHRedraw       = 0x0002
	csVRedraw       = 0x0001
	wsOverlappedWin = 0x00CF0000
	wsClipChildren  = 0x02000000
	iconSmall       = 0
	iconBig         = 1
	idcArrow        = 32512
	colorWindow     = 5
	coInitApartment = 0x2
	classExists     = 1410
	iconResourceID  = 1

	dwmwaUseImmersiveDarkModeBefore20h1 = 19
	dwmwaUseImmersiveDarkMode           = 20
	dwmwaCaptionColor                   = 35
	dwmwaTextColor                      = 36
	dwmwaBorderColor                    = 34

	titleBarColorBGR = 0x0018120f // #0f1218
	titleTextColor   = 0x00ffffff
	borderColorBGR   = 0x00202830
)

var (
	user32   = windows.NewLazySystemDLL("user32")
	kernel32 = windows.NewLazySystemDLL("kernel32")
	ole32    = windows.NewLazySystemDLL("ole32")
	dwmapi   = windows.NewLazySystemDLL("dwmapi")

	procRegisterClassExW      = user32.NewProc("RegisterClassExW")
	procCreateWindowExW       = user32.NewProc("CreateWindowExW")
	procDestroyWindow         = user32.NewProc("DestroyWindow")
	procShowWindow            = user32.NewProc("ShowWindow")
	procUpdateWindow          = user32.NewProc("UpdateWindow")
	procSetFocus              = user32.NewProc("SetFocus")
	procGetMessageW           = user32.NewProc("GetMessageW")
	procTranslateMessage      = user32.NewProc("TranslateMessage")
	procDispatchMessageW      = user32.NewProc("DispatchMessageW")
	procDefWindowProcW        = user32.NewProc("DefWindowProcW")
	procPostQuitMessage       = user32.NewProc("PostQuitMessage")
	procPostMessageW          = user32.NewProc("PostMessageW")
	procSendMessageW          = user32.NewProc("SendMessageW")
	procLoadCursorW           = user32.NewProc("LoadCursorW")
	procLoadIconW             = user32.NewProc("LoadIconW")
	procGetModuleHandleW      = kernel32.NewProc("GetModuleHandleW")
	procCoInitializeEx        = ole32.NewProc("CoInitializeEx")
	procDwmSetWindowAttribute = dwmapi.NewProc("DwmSetWindowAttribute")

	wndProcCallback = syscall.NewCallback(conferenceWndProc)
	classOnce       sync.Once
	classErr        error
	conferenceWins  sync.Map
)

type wndClassEx struct {
	cbSize        uint32
	style         uint32
	lpfnWndProc   uintptr
	cbClsExtra    int32
	cbWndExtra    int32
	hInstance     windows.Handle
	hIcon         windows.Handle
	hCursor       windows.Handle
	hbrBackground windows.Handle
	lpszMenuName  *uint16
	lpszClassName *uint16
	hIconSm       windows.Handle
}

type winMsg struct {
	hwnd     uintptr
	message  uint32
	wParam   uintptr
	lParam   uintptr
	time     uint32
	pt       struct{ x, y int32 }
	lPrivate uint32
}

type conferenceSession struct {
	hwnd      uintptr
	chromium  *edge.Chromium
	endpoint  string
	done      chan struct{}
	closeOnce sync.Once
	doneOnce  sync.Once
	visible   atomic.Bool
}

func (s *conferenceSession) close() {
	s.closeOnce.Do(func() {
		if hwnd := atomic.LoadUintptr(&s.hwnd); hwnd != 0 {
			procPostMessageW.Call(hwnd, wmClose, 0, 0)
		}
	})
}

func (s *conferenceSession) signalDone() {
	s.doneOnce.Do(func() { close(s.done) })
}

func (s *conferenceSession) setVisible(visible bool) {
	hwnd := atomic.LoadUintptr(&s.hwnd)
	if hwnd == 0 {
		return
	}
	wp := uintptr(0)
	if visible {
		wp = 1
	}
	procPostMessageW.Call(hwnd, wmAppSetVisible, wp, 0)
}

func startConferenceWebView(ctx context.Context, profileDir string) (*conferenceSession, error) {
	port, err := pickLocalDebugPort()
	if err != nil {
		return nil, err
	}
	endpoint := fmt.Sprintf("http://127.0.0.1:%d", port)

	session := &conferenceSession{
		endpoint: endpoint,
		done:     make(chan struct{}),
	}
	ready := make(chan error, 1)

	go func() {
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()
		if err := runConferenceWebView(ctx, session, profileDir, port, ready); err != nil {
			select {
			case ready <- err:
			default:
			}
		}
		session.signalDone()
	}()

	select {
	case <-ctx.Done():
		session.close()
		select {
		case <-session.done:
		case <-time.After(3 * time.Second):
		}
		if errors.Is(ctx.Err(), context.Canceled) {
			return nil, ctx.Err()
		}
		return nil, fmt.Errorf("ожидание окна WebView2: %w", ctx.Err())
	case err := <-ready:
		if err != nil {
			session.close()
			<-session.done
			return nil, err
		}
		return session, nil
	case <-session.done:
		select {
		case err := <-ready:
			if err != nil {
				return nil, err
			}
		default:
		}
		return nil, errors.New("окно WebView2 закрылось до подключения")
	}
}

func runConferenceWebView(ctx context.Context, session *conferenceSession, profileDir string, port int, ready chan<- error) error {
	if hr, _, _ := procCoInitializeEx.Call(0, coInitApartment); hr != 0 && hr != 1 {
		return fmt.Errorf("CoInitializeEx: HRESULT 0x%X", hr)
	}
	if err := registerConferenceClass(); err != nil {
		return err
	}

	hwnd, err := createConferenceWindow()
	if err != nil {
		return err
	}
	atomic.StoreUintptr(&session.hwnd, hwnd)
	conferenceWins.Store(hwnd, session)
	styleConferenceWindow(hwnd)
	registerBrowserWindow(session)
	defer func() {
		unregisterBrowserWindow(session)
		conferenceWins.Delete(hwnd)
	}()

	chromium := edge.NewChromium()
	chromium.DataPath = profileDir
	chromium.AdditionalBrowserArgs = []string{
		fmt.Sprintf("--remote-debugging-port=%d", port),
		"--remote-debugging-address=127.0.0.1",
		"--autoplay-policy=no-user-gesture-required",
		"--disable-background-timer-throttling",
		"--disable-renderer-backgrounding",
		"--disable-features=HardwareMediaKeyHandling",
	}
	chromium.SetPermission(edge.CoreWebView2PermissionKindMicrophone, edge.CoreWebView2PermissionStateAllow)
	chromium.SetPermission(edge.CoreWebView2PermissionKindCamera, edge.CoreWebView2PermissionStateAllow)
	started := atomic.Bool{}
	chromium.SetErrorCallback(func(err error) {
		if err == nil || started.Load() {
			return
		}
		select {
		case ready <- fmt.Errorf("WebView2: %w", err):
		default:
		}
		session.close()
	})
	chromium.ProcessFailedCallback = func(_ *edge.ICoreWebView2, _ *edge.ICoreWebView2ProcessFailedEventArgs) {
		session.close()
	}
	session.chromium = chromium

	go func() {
		select {
		case <-ctx.Done():
			session.close()
		case <-session.done:
		}
	}()

	if !chromium.Embed(hwnd) {
		return errors.New("не удалось встроить WebView2")
	}
	if settings, err := chromium.GetSettings(); err == nil && settings != nil {
		ua, uaErr := settings.GetUserAgent()
		if uaErr != nil || strings.TrimSpace(ua) == "" {
			ua = defaultChromeDesktopUA
		}
		_ = settings.PutUserAgent(chromeLikeUserAgent(ua))
	}
	chromium.SetBackgroundColour(15, 18, 24, 255)
	_ = chromium.Show()
	chromium.Resize()
	procShowWindow.Call(hwnd, swShow)
	procUpdateWindow.Call(hwnd)
	procSetFocus.Call(hwnd)

	go func() {
		if err := waitForDebugEndpoint(ctx, session.endpoint, 20*time.Second); err != nil {
			select {
			case ready <- fmt.Errorf("WebView2 не открыл порт отладки: %w", err):
			default:
			}
			session.close()
			return
		}
		started.Store(true)
		select {
		case ready <- nil:
		default:
		}
	}()

	pumpWindowMessages()
	chromium.ShuttingDown()
	return nil
}

func registerConferenceClass() error {
	classOnce.Do(func() {
		instance, _, _ := procGetModuleHandleW.Call(0)
		cursor, _, _ := procLoadCursorW.Call(0, idcArrow)
		windowIcon := loadConferenceWindowIcon()
		name, err := windows.UTF16PtrFromString(conferenceWindowClass)
		if err != nil {
			classErr = err
			return
		}
		wc := wndClassEx{
			cbSize:        uint32(unsafe.Sizeof(wndClassEx{})),
			style:         csHRedraw | csVRedraw,
			lpfnWndProc:   wndProcCallback,
			hInstance:     windows.Handle(instance),
			hIcon:         windows.Handle(windowIcon),
			hCursor:       windows.Handle(cursor),
			hbrBackground: windows.Handle(colorWindow + 1),
			lpszClassName: name,
			hIconSm:       windows.Handle(windowIcon),
		}
		atom, _, callErr := procRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc)))
		if atom == 0 {
			if errno, ok := callErr.(syscall.Errno); ok && errno == classExists {
				return
			}
			classErr = fmt.Errorf("RegisterClassExW: %v", callErr)
		}
	})
	return classErr
}

func createConferenceWindow() (uintptr, error) {
	className, err := windows.UTF16PtrFromString(conferenceWindowClass)
	if err != nil {
		return 0, err
	}
	title, err := windows.UTF16PtrFromString(conferenceWindowTitle())
	if err != nil {
		return 0, err
	}
	instance, _, _ := procGetModuleHandleW.Call(0)
	hwnd, _, callErr := procCreateWindowExW.Call(
		0,
		uintptr(unsafe.Pointer(className)),
		uintptr(unsafe.Pointer(title)),
		wsOverlappedWin|wsClipChildren,
		100,
		80,
		1200,
		800,
		0,
		0,
		instance,
		0,
	)
	if hwnd == 0 {
		return 0, fmt.Errorf("CreateWindowExW: %v", callErr)
	}
	return hwnd, nil
}

func conferenceWindowTitle() string {
	return buildinfo.Get().Name + " — ВКС"
}

func styleConferenceWindow(hwnd uintptr) {
	windowIcon := loadConferenceWindowIcon()
	if windowIcon != 0 {
		procSendMessageW.Call(hwnd, wmSetIcon, iconBig, windowIcon)
		procSendMessageW.Call(hwnd, wmSetIcon, iconSmall, windowIcon)
	}

	darkMode := int32(1)
	procDwmSetWindowAttribute.Call(
		hwnd,
		uintptr(dwmwaUseImmersiveDarkMode),
		uintptr(unsafe.Pointer(&darkMode)),
		unsafe.Sizeof(darkMode),
	)
	procDwmSetWindowAttribute.Call(
		hwnd,
		uintptr(dwmwaUseImmersiveDarkModeBefore20h1),
		uintptr(unsafe.Pointer(&darkMode)),
		unsafe.Sizeof(darkMode),
	)

	caption := int32(titleBarColorBGR)
	text := int32(titleTextColor)
	border := int32(borderColorBGR)
	procDwmSetWindowAttribute.Call(hwnd, uintptr(dwmwaCaptionColor), uintptr(unsafe.Pointer(&caption)), unsafe.Sizeof(caption))
	procDwmSetWindowAttribute.Call(hwnd, uintptr(dwmwaTextColor), uintptr(unsafe.Pointer(&text)), unsafe.Sizeof(text))
	procDwmSetWindowAttribute.Call(hwnd, uintptr(dwmwaBorderColor), uintptr(unsafe.Pointer(&border)), unsafe.Sizeof(border))
}

func conferenceWndProc(hwnd, msg, wparam, lparam uintptr) uintptr {
	var session *conferenceSession
	if value, ok := conferenceWins.Load(hwnd); ok {
		session = value.(*conferenceSession)
	}
	switch msg {
	case wmAppSetVisible:
		if wparam != 0 {
			procShowWindow.Call(hwnd, swShow)
		} else {
			procShowWindow.Call(hwnd, swHide)
		}
		if session != nil {
			session.visible.Store(wparam != 0)
		}
		return 0
	case wmSize:
		if wparam != sizeMinimized && session != nil && session.chromium != nil {
			session.chromium.Resize()
		}
		return 0
	case wmClose:
		if session != nil && session.chromium != nil {
			session.chromium.ShuttingDown()
		}
		procDestroyWindow.Call(hwnd)
		return 0
	case wmDestroy:
		conferenceWins.Delete(hwnd)
		if session != nil {
			session.signalDone()
		}
		procPostQuitMessage.Call(0)
		return 0
	}
	ret, _, _ := procDefWindowProcW.Call(hwnd, msg, wparam, lparam)
	return ret
}

func pumpWindowMessages() {
	var msg winMsg
	for {
		r, _, _ := procGetMessageW.Call(uintptr(unsafe.Pointer(&msg)), 0, 0, 0)
		if int32(r) <= 0 {
			return
		}
		procTranslateMessage.Call(uintptr(unsafe.Pointer(&msg)))
		procDispatchMessageW.Call(uintptr(unsafe.Pointer(&msg)))
	}
}
