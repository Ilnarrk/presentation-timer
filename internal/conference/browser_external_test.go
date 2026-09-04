//go:build !windows

package conference

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestDiscoverBrowserEndpointsUsesSavedEndpoint(t *testing.T) {
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

	appDir := t.TempDir()
	if err := saveBrowserEndpoint(appDir, server.URL); err != nil {
		t.Fatalf("saveBrowserEndpoint() error = %v", err)
	}
	endpoints := discoverBrowserEndpoints(context.Background(), appDir, "telemost")
	if len(endpoints) == 0 || endpoints[0].URL != server.URL {
		t.Fatalf("endpoints = %v, want saved endpoint first", endpoints)
	}
}

func TestBrowserCompatibility(t *testing.T) {
	if browserCompatible("edge", "salutejazz") {
		t.Fatal("Edge must not be used for SaluteJazz")
	}
	if !browserCompatible("chrome", "salutejazz") || !browserCompatible("yandex", "salutejazz") {
		t.Fatal("Chrome and Yandex Browser must be supported for SaluteJazz")
	}
	if !browserCompatible("edge", "telemost") {
		t.Fatal("Edge fallback should remain available for other platforms")
	}
	if !browserCompatible("edge", "generic") {
		t.Fatal("Edge should remain available for on-prem platforms")
	}
}
