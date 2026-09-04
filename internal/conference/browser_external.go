//go:build !windows

package conference

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

func openBrowser(parent context.Context, targetURL, platformID string) (*chromeBrowser, context.CancelFunc, error) {
	configDir, err := os.UserConfigDir()
	if err != nil {
		return nil, nil, fmt.Errorf("не удалось определить каталог профиля браузера: %w", err)
	}
	appDir := filepath.Join(configDir, "presentation-timer")
	if err := os.MkdirAll(appDir, 0o755); err != nil {
		return nil, nil, fmt.Errorf("не удалось создать каталог браузера: %w", err)
	}
	profileDir := filepath.Join(appDir, "conference-browser")
	if err := os.MkdirAll(profileDir, 0o755); err != nil {
		return nil, nil, fmt.Errorf("не удалось создать профиль браузера: %w", err)
	}

	var failures []string
	for _, endpoint := range discoverBrowserEndpoints(parent, appDir, platformID) {
		browser, cancel, err := attachBrowser(parent, targetURL, endpoint.URL, "Подключено к уже запущенному "+endpoint.Label)
		if err == nil {
			return browser, cancel, nil
		}
		failures = append(failures, fmt.Sprintf("%s: %v", endpoint.URL, err))
	}

	candidates := findChromiumCandidates(platformID)
	if len(candidates) == 0 {
		return nil, nil, errors.New("не найден совместимый браузер: установите Google Chrome или Chromium")
	}
	for _, candidate := range candidates {
		endpoint, err := launchBrowser(parent, candidate.Path, profileDir)
		if err != nil {
			failures = append(failures, fmt.Sprintf("%s: %v", candidate.Label, err))
			continue
		}
		_ = saveBrowserEndpoint(appDir, endpoint)
		browser, cancel, err := attachBrowser(parent, targetURL, endpoint, "Запущен "+candidate.Label)
		if err == nil {
			return browser, cancel, nil
		}
		failures = append(failures, fmt.Sprintf("%s: %v", candidate.Label, err))
	}

	return nil, nil, fmt.Errorf("не удалось запустить совместимый браузер: %s", strings.Join(failures, "; "))
}

func launchBrowser(ctx context.Context, executable, profileDir string) (string, error) {
	port, err := pickLocalDebugPort()
	if err != nil {
		return "", err
	}
	args := []string{
		fmt.Sprintf("--remote-debugging-port=%d", port),
		"--remote-debugging-address=127.0.0.1",
		"--user-data-dir=" + profileDir,
		"--no-first-run",
		"--no-default-browser-check",
		"--disable-background-timer-throttling",
		"--disable-renderer-backgrounding",
		"--autoplay-policy=no-user-gesture-required",
		"--disable-features=HardwareMediaKeyHandling",
		"about:blank",
	}
	command := exec.Command(executable, args...)
	if err := command.Start(); err != nil {
		return "", err
	}
	go func() { _ = command.Wait() }()

	endpoint := fmt.Sprintf("http://127.0.0.1:%d", port)
	if err := waitForDebugEndpoint(ctx, endpoint, 20*time.Second); err != nil {
		return "", errors.New("браузер не открыл порт отладки за 20 секунд")
	}
	return endpoint, nil
}

type endpointSettings struct {
	Endpoint string `json:"endpoint"`
}

func saveBrowserEndpoint(appDir, endpoint string) error {
	data, err := json.Marshal(endpointSettings{Endpoint: endpoint})
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(appDir, "browser-debug.json"), data, 0o600)
}

func discoverBrowserEndpoints(ctx context.Context, appDir, platformID string) []debugEndpoint {
	data, err := os.ReadFile(filepath.Join(appDir, "browser-debug.json"))
	if err != nil {
		return nil
	}
	var saved endpointSettings
	if json.Unmarshal(data, &saved) != nil || saved.Endpoint == "" {
		return nil
	}
	info, err := probeBrowserInfo(ctx, saved.Endpoint)
	if err != nil {
		return nil
	}
	if strings.Contains(strings.ToLower(info.Product), "edg") && !browserCompatible("edge", platformID) {
		return nil
	}
	return []debugEndpoint{info}
}

type browserCandidate struct {
	Path  string
	Kind  string
	Label string
}

func findChromiumCandidates(platformID string) []browserCandidate {
	var candidates []browserCandidate
	for _, name := range []string{"google-chrome", "chromium", "chromium-browser", "chrome", "microsoft-edge", "msedge"} {
		if path, err := exec.LookPath(name); err == nil {
			kind, label := classifyBrowser(path)
			candidates = append(candidates, browserCandidate{Path: path, Kind: kind, Label: label})
		}
	}
	candidates = append(candidates,
		browserCandidate{Path: "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome", Kind: "chrome", Label: "Google Chrome"},
		browserCandidate{Path: "/Applications/Microsoft Edge.app/Contents/MacOS/Microsoft Edge", Kind: "edge", Label: "Microsoft Edge"},
	)

	result := make([]browserCandidate, 0, len(candidates))
	seen := make(map[string]struct{}, len(candidates))
	for _, candidate := range candidates {
		if !browserCompatible(candidate.Kind, platformID) {
			continue
		}
		if info, err := os.Stat(candidate.Path); err == nil && !info.IsDir() {
			absolute, err := filepath.Abs(candidate.Path)
			if err != nil {
				absolute = candidate.Path
			}
			key := strings.ToLower(absolute)
			if _, exists := seen[key]; !exists {
				seen[key] = struct{}{}
				candidate.Path = absolute
				result = append(result, candidate)
			}
		}
	}
	return result
}

func classifyBrowser(path string) (string, string) {
	lower := strings.ToLower(path)
	switch {
	case strings.Contains(lower, "edge"):
		return "edge", "Microsoft Edge"
	default:
		return "chrome", "Google Chrome"
	}
}

func browserCompatible(kind, platformID string) bool {
	if kind == "edge" && platformID == "salutejazz" {
		return false
	}
	return kind == "chrome" || kind == "yandex" || kind == "edge"
}
