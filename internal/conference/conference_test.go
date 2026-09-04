package conference

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestResolveSupportedPlatforms(t *testing.T) {
	tests := []struct {
		url      string
		platform string
	}{
		{"https://salutejazz.ru/abc?token=secret", "salutejazz"},
		{"https://room.sberjazz.ru/abc", "salutejazz"},
		{"https://telemost.yandex.ru/j/123", "telemost"},
		{"https://telemost.yandex.com/j/123", "telemost"},
		{"https://company.ktalk.ru/room", "kontur-talk"},
		{"https://my.mts-link.ru/j/123", "mts-link"},
		{"https://events.webinar.ru/123", "mts-link"},
		{"https://mint.tatneft.tatar/meet/6a994cc595725fe897218764", "mint"},
		{"https://mintconf.ru/meet/abc", "mint"},
		{"https://jazz.corp.local/room", "generic"},
		{"https://10.0.0.5/meet/abc", "generic"},
	}

	for _, test := range tests {
		t.Run(test.platform, func(t *testing.T) {
			resolved, err := Resolve(test.url)
			if err != nil {
				t.Fatalf("Resolve() error = %v", err)
			}
			if resolved.Adapter.ID() != test.platform {
				t.Fatalf("platform = %q, want %q", resolved.Adapter.ID(), test.platform)
			}
			if strings.Contains(resolved.DisplayURL, "secret") {
				t.Fatal("DisplayURL contains a query secret")
			}
		})
	}
}

func TestResolveRejectsUnsafeAndUnsupportedURLs(t *testing.T) {
	for _, rawURL := range []string{
		"http://telemost.yandex.ru/j/123",
		"https://telemost.yandex.ru.evil.example/j/123",
		"https://user:password@telemost.yandex.ru/j/123",
	} {
		if _, err := Resolve(rawURL); err == nil {
			t.Fatalf("Resolve(%q) unexpectedly succeeded", rawURL)
		}
	}
}

type fakeBrowser struct {
	result any
	err    error
	expr   string
	done   <-chan struct{}
}

func (f *fakeBrowser) Evaluate(_ context.Context, expression string, result any) error {
	f.expr = expression
	if f.err != nil {
		return f.err
	}
	switch target := result.(type) {
	case *bool:
		*target = f.result.(bool)
	case *joinProbe:
		*target = f.result.(joinProbe)
	case *string:
		switch value := f.result.(type) {
		case string:
			*target = value
		default:
			*target = fmt.Sprint(value)
		}
	}
	return nil
}

func (f *fakeBrowser) Description() string {
	return "Тестовый браузер"
}

func (f *fakeBrowser) Done() <-chan struct{} {
	return f.done
}

func TestPlayWAVMarksSuccessfulTest(t *testing.T) {
	browser := &fakeBrowser{result: true}
	controller := NewController(nil)
	controller.browser = browser
	controller.cancel = func() {}
	controller.state.update(func(state *State) {
		state.Phase = PhaseJoined
	})

	if err := controller.TestSound([]byte("RIFF test")); err != nil {
		t.Fatalf("TestSound() error = %v", err)
	}
	state := controller.GetState()
	if !state.Tested || state.Phase != PhaseJoined {
		t.Fatalf("state = %+v, want joined and tested", state)
	}
	if !strings.Contains(browser.expr, "__timerPlayWav") {
		t.Fatalf("expression does not invoke media bridge: %s", browser.expr)
	}
}

func TestPlayWAVRejectsDisconnectedState(t *testing.T) {
	controller := NewController(nil)
	if err := controller.PlaySound([]byte("RIFF")); !errors.Is(err, ErrNotJoined) {
		t.Fatalf("PlaySound() error = %v, want ErrNotJoined", err)
	}
}

func TestSetReceiveMutedInvokesMediaBridge(t *testing.T) {
	browser := &fakeBrowser{result: true}
	controller := NewController(nil)
	controller.browser = browser

	controller.SetReceiveMuted(true)
	if !strings.Contains(browser.expr, "__timerSetReceiveMuted(true)") {
		t.Fatalf("muted expression = %q", browser.expr)
	}

	controller.SetReceiveMuted(false)
	if !strings.Contains(browser.expr, "__timerSetReceiveMuted(false)") {
		t.Fatalf("unmuted expression = %q", browser.expr)
	}
}

func TestDisconnectImmediatelySetsLeft(t *testing.T) {
	controller := NewController(nil)
	cancelled := false
	controller.cancel = func() { cancelled = true }
	controller.browser = &fakeBrowser{result: true}
	controller.state.update(func(state *State) {
		state.Phase = PhaseConnecting
		state.Tested = true
	})

	controller.Disconnect()

	state := controller.GetState()
	if state.Phase != PhaseLeft || state.Tested {
		t.Fatalf("state = %+v, want left and not tested", state)
	}
	if !cancelled {
		t.Fatal("session context was not cancelled")
	}
	if controller.cancel != nil || controller.browser != nil {
		t.Fatal("session was not cleared synchronously")
	}
}

func TestConfirmJoinedRequiresBrowser(t *testing.T) {
	controller := NewController(nil)
	if err := controller.ConfirmJoined(); !errors.Is(err, ErrNotJoined) {
		t.Fatalf("ConfirmJoined() error = %v, want ErrNotJoined", err)
	}
}

func TestConfirmJoinedChecksMediaBridge(t *testing.T) {
	controller := NewController(nil)
	controller.browser = &fakeBrowser{result: true}
	controller.cancel = func() {}
	controller.state.update(func(state *State) {
		state.Phase = PhaseConnecting
	})

	if err := controller.ConfirmJoined(); err != nil {
		t.Fatalf("ConfirmJoined() error = %v", err)
	}
	if state := controller.GetState(); state.Phase != PhaseJoined {
		t.Fatalf("phase = %q, want joined", state.Phase)
	}
}

func TestConfirmJoinedClearsClosedBrowserSession(t *testing.T) {
	controller := NewController(nil)
	controller.runID = 7
	controller.cancel = func() {}
	controller.browser = &fakeBrowser{err: errors.New("target closed")}
	controller.state.update(func(state *State) {
		state.Phase = PhaseConnecting
	})

	if err := controller.ConfirmJoined(); err == nil {
		t.Fatal("ConfirmJoined() unexpectedly succeeded")
	}
	state := controller.GetState()
	if state.Phase != PhaseError || !strings.Contains(state.Message, "Подключитесь заново") {
		t.Fatalf("state = %+v, want reconnectable error", state)
	}
	if controller.cancel != nil || controller.browser != nil {
		t.Fatal("closed browser session was not cleared")
	}
}

func TestWatchBrowserClearsUnexpectedlyClosedSession(t *testing.T) {
	done := make(chan struct{})
	stateChanged := make(chan State, 4)
	controller := NewController(func(state State) {
		stateChanged <- state
	})
	controller.runID = 3
	controller.cancel = func() {}
	controller.browser = &fakeBrowser{result: true, done: done}
	controller.state.update(func(state *State) {
		state.Phase = PhaseConnecting
	})

	ctx := context.Background()
	go controller.watchBrowser(ctx, 3, done)
	close(done)

	for {
		select {
		case state := <-stateChanged:
			if state.Phase != PhaseError {
				continue
			}
			if controller.cancel != nil || controller.browser != nil {
				t.Fatal("unexpectedly closed browser session was not cleared")
			}
			return
		case <-time.After(time.Second):
			t.Fatal("browser closure was not detected")
		}
	}
}

func TestProbeBrowserInfoRejectsNonLocalEndpoint(t *testing.T) {
	if _, err := probeBrowserInfo(context.Background(), "http://example.com:9222"); err == nil {
		t.Fatal("probeBrowserInfo() unexpectedly accepted a non-local endpoint")
	}
}

func TestProbeBrowserInfoUsesLocalEndpoint(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/json/version" {
			http.NotFound(writer, request)
			return
		}
		_ = json.NewEncoder(writer).Encode(browserVersion{
			Browser:              "Chrome/140.0",
			WebSocketDebuggerURL: "ws://127.0.0.1:12345/devtools/browser/test",
		})
	}))
	defer server.Close()

	info, err := probeBrowserInfo(context.Background(), server.URL)
	if err != nil {
		t.Fatalf("probeBrowserInfo() error = %v", err)
	}
	if info.WebSocketURL == "" || info.URL != server.URL {
		t.Fatalf("info = %+v, want local websocket endpoint", info)
	}
}

func TestExistingPageTargetID(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/json/list" {
			http.NotFound(writer, request)
			return
		}
		_ = json.NewEncoder(writer).Encode([]jsonTarget{
			{ID: "background", Type: "service_worker"},
			{ID: "page-1", Type: "page", URL: "about:blank"},
		})
	}))
	defer server.Close()

	if got := existingPageTargetID(context.Background(), server.URL); got != "page-1" {
		t.Fatalf("existingPageTargetID() = %q, want page-1", got)
	}
}

func TestChromeLikeUserAgentStripsEdgeToken(t *testing.T) {
	raw := "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/140.0.0.0 Safari/537.36 Edg/140.0.3485.54"
	got := chromeLikeUserAgent(raw)
	if strings.Contains(strings.ToLower(got), "edg/") {
		t.Fatalf("user agent still looks like Edge: %s", got)
	}
	if !strings.Contains(got, "Chrome/140") {
		t.Fatalf("user agent lost Chrome version: %s", got)
	}
	if chromeVersionMajor(got) != "140" {
		t.Fatalf("chromeVersionMajor() = %s, want 140", chromeVersionMajor(got))
	}
}

func TestGetDiagnosticsRequiresBrowser(t *testing.T) {
	controller := NewController(nil)
	if _, err := controller.GetDiagnostics(); !errors.Is(err, ErrNotJoined) {
		t.Fatalf("GetDiagnostics() error = %v, want ErrNotJoined", err)
	}
}

func TestGetDiagnosticsReturnsSnapshot(t *testing.T) {
	browser := &fakeBrowser{result: `{"peerCount":2,"mediaUsed":true}`}
	controller := NewController(nil)
	controller.browser = browser
	controller.cancel = func() {}
	controller.state.update(func(state *State) {
		state.Phase = PhaseJoined
	})

	got, err := controller.GetDiagnostics()
	if err != nil {
		t.Fatalf("GetDiagnostics() error = %v", err)
	}
	if !strings.Contains(browser.expr, "__timerGetDiagnostics") {
		t.Fatalf("expression does not request diagnostics: %s", browser.expr)
	}
	if !strings.Contains(got, `"peerCount":2`) {
		t.Fatalf("diagnostics = %s, want snapshot payload", got)
	}
}
