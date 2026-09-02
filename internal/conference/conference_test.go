package conference

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
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
		"https://example.com/meeting",
		"https://telemost.yandex.ru.evil.example/j/123",
		"https://user:password@telemost.yandex.ru/j/123",
		"https://127.0.0.1/meeting",
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
	}
	return nil
}

func (f *fakeBrowser) Description() string {
	return "Тестовый браузер"
}

func TestPlayWAVMarksSuccessfulTest(t *testing.T) {
	browser := &fakeBrowser{result: true}
	controller := NewController(nil)
	controller.browser = browser
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

func TestDiscoverBrowserEndpointsUsesSavedEndpoint(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/json/version" {
			http.NotFound(writer, request)
			return
		}
		_ = json.NewEncoder(writer).Encode(browserVersion{
			Browser:              "Edg/140.0",
			WebSocketDebuggerURL: "ws://127.0.0.1:12345/devtools/browser/test",
		})
	}))
	defer server.Close()

	appDir := t.TempDir()
	if err := saveBrowserEndpoint(appDir, server.URL); err != nil {
		t.Fatalf("saveBrowserEndpoint() error = %v", err)
	}
	endpoints := discoverBrowserEndpoints(context.Background(), appDir, "https://telemost.yandex.ru/j/123")
	if len(endpoints) == 0 || endpoints[0].URL != server.URL {
		t.Fatalf("endpoints = %v, want saved endpoint first", endpoints)
	}
}

func TestBrowserCompatibility(t *testing.T) {
	saluteURL := "https://salutejazz.ru/room"
	if browserCompatible("edge", saluteURL) {
		t.Fatal("Edge must not be used for SaluteJazz")
	}
	if !browserCompatible("chrome", saluteURL) || !browserCompatible("yandex", saluteURL) {
		t.Fatal("Chrome and Yandex Browser must be supported for SaluteJazz")
	}
	if !browserCompatible("edge", "https://telemost.yandex.ru/j/123") {
		t.Fatal("Edge fallback should remain available for other platforms")
	}
}
