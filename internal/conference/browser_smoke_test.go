package conference

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"
)

// This smoke test is opt-in because it opens a real Edge/Chrome window.
func TestBrowserMediaBridgeSmoke(t *testing.T) {
	if os.Getenv("RUN_BROWSER_SMOKE") != "1" {
		t.Skip("set RUN_BROWSER_SMOKE=1 to run the real browser smoke test")
	}

	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		_, _ = writer.Write([]byte("<!doctype html><html><body>timer bridge test</body></html>"))
	}))
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	browser, closeBrowser, err := openBrowser(ctx, server.URL, "generic")
	if err != nil {
		t.Fatalf("openBrowser() error = %v", err)
	}
	defer closeBrowser()

	var result struct {
		Ready     bool `json:"ready"`
		WebDriver bool `json:"webDriver"`
	}
	err = browser.Evaluate(ctx,
		`({ ready: Boolean(window.__presentationTimerBridgeInstalled && window.__timerPlayWav), webDriver: navigator.webdriver === true })`,
		&result,
	)
	if err != nil {
		t.Fatalf("Evaluate() error = %v", err)
	}
	if !result.Ready {
		t.Fatal("media bridge was not installed before navigation")
	}
	if result.WebDriver {
		t.Fatal("browser exposes navigator.webdriver=true")
	}
}
