//go:build windows

package conference

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
)

func openBrowser(parent context.Context, targetURL, _ string) (*chromeBrowser, context.CancelFunc, error) {
	configDir, err := os.UserConfigDir()
	if err != nil {
		return nil, nil, fmt.Errorf("не удалось определить каталог профиля WebView2: %w", err)
	}
	appDir := filepath.Join(configDir, "presentation-timer")
	profileDir := filepath.Join(appDir, "conference-webview")
	if err := os.MkdirAll(profileDir, 0o755); err != nil {
		return nil, nil, fmt.Errorf("не удалось создать профиль WebView2: %w", err)
	}

	session, err := startConferenceWebView(parent, profileDir)
	if err != nil {
		return nil, nil, err
	}

	browser, cancel, err := attachBrowser(parent, targetURL, session.endpoint, "Открыто встроенное окно WebView2")
	if err != nil {
		session.close()
		return nil, nil, err
	}

	go func() {
		<-session.done
		cancel()
	}()

	return browser, func() {
		cancel()
		session.close()
	}, nil
}
