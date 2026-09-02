package conference

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/chromedp"
)

type chromeBrowser struct {
	ctx         context.Context
	description string
}

func (b *chromeBrowser) Evaluate(ctx context.Context, expression string, result any) error {
	return chromedp.Run(b.ctx, chromedp.Evaluate(expression, result))
}

func (b *chromeBrowser) Description() string {
	return b.description
}

func openBrowser(parent context.Context, targetURL string) (*chromeBrowser, context.CancelFunc, error) {
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
	for _, endpoint := range discoverBrowserEndpoints(parent, appDir, targetURL) {
		browser, cancel, err := attachBrowser(parent, targetURL, endpoint.URL, "Подключено к уже запущенному "+endpoint.Label)
		if err == nil {
			return browser, cancel, nil
		}
		failures = append(failures, fmt.Sprintf("%s: %v", endpoint.URL, err))
	}

	candidates := findChromiumCandidates(targetURL)
	if len(candidates) == 0 {
		if firefoxInstalled() {
			return nil, nil, errors.New("найден Firefox, но синтетический микрофон таймера требует Google Chrome или Яндекс Браузер; установите один из них")
		}
		return nil, nil, errors.New("не найден совместимый браузер: установите Google Chrome или Яндекс Браузер")
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

func attachBrowser(
	parent context.Context,
	targetURL string,
	endpoint string,
	description string,
) (*chromeBrowser, context.CancelFunc, error) {
	wsURL, err := probeBrowserEndpoint(parent, endpoint)
	if err != nil {
		return nil, nil, err
	}
	allocatorCtx, allocatorCancel := chromedp.NewRemoteAllocator(parent, wsURL)
	browserCtx, browserCancel := chromedp.NewContext(allocatorCtx)
	cancel := func() {
		browserCancel()
		allocatorCancel()
	}

	err = chromedp.Run(browserCtx,
		chromedp.ActionFunc(func(ctx context.Context) error {
			_, err := page.AddScriptToEvaluateOnNewDocument(mediaBridgeScript).Do(ctx)
			return err
		}),
		chromedp.ActionFunc(func(ctx context.Context) error {
			navigationCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
			defer cancel()
			_, _, errorText, _, err := page.Navigate(targetURL).Do(navigationCtx)
			if err != nil {
				return err
			}
			if errorText != "" {
				return errors.New(errorText)
			}
			return nil
		}),
	)
	if err != nil {
		cancel()
		return nil, nil, err
	}

	return &chromeBrowser{ctx: browserCtx, description: description}, cancel, nil
}

func launchBrowser(ctx context.Context, executable, profileDir string) (string, error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", fmt.Errorf("не удалось выбрать порт отладки: %w", err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	_ = listener.Close()

	args := []string{
		fmt.Sprintf("--remote-debugging-port=%d", port),
		"--remote-debugging-address=127.0.0.1",
		"--user-data-dir=" + profileDir,
		"--no-first-run",
		"--no-default-browser-check",
		"--disable-background-timer-throttling",
		"--disable-renderer-backgrounding",
		"--autoplay-policy=no-user-gesture-required",
		"--use-fake-ui-for-media-stream",
		"--disable-features=HardwareMediaKeyHandling",
		"about:blank",
	}
	command := exec.Command(executable, args...)
	if err := command.Start(); err != nil {
		return "", err
	}
	go func() { _ = command.Wait() }()

	endpoint := fmt.Sprintf("http://127.0.0.1:%d", port)
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := probeBrowserEndpoint(ctx, endpoint); err == nil {
			return endpoint, nil
		}
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(200 * time.Millisecond):
		}
	}
	return "", errors.New("браузер не открыл порт отладки за 20 секунд")
}

type browserVersion struct {
	Browser              string `json:"Browser"`
	WebSocketDebuggerURL string `json:"webSocketDebuggerUrl"`
}

func probeBrowserEndpoint(ctx context.Context, endpoint string) (string, error) {
	info, err := probeBrowserInfo(ctx, endpoint)
	return info.WebSocketURL, err
}

type debugEndpoint struct {
	URL          string
	WebSocketURL string
	Product      string
	Label        string
}

func probeBrowserInfo(ctx context.Context, endpoint string) (debugEndpoint, error) {
	parsed, err := url.Parse(endpoint)
	if err != nil || parsed.Scheme != "http" || (parsed.Hostname() != "127.0.0.1" && parsed.Hostname() != "localhost") {
		return debugEndpoint{}, errors.New("небезопасный адрес отладки браузера")
	}
	requestCtx, cancel := context.WithTimeout(ctx, 800*time.Millisecond)
	defer cancel()
	request, err := http.NewRequestWithContext(requestCtx, http.MethodGet, strings.TrimRight(endpoint, "/")+"/json/version", nil)
	if err != nil {
		return debugEndpoint{}, err
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return debugEndpoint{}, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return debugEndpoint{}, fmt.Errorf("порт отладки вернул HTTP %d", response.StatusCode)
	}
	var version browserVersion
	if err := json.NewDecoder(response.Body).Decode(&version); err != nil {
		return debugEndpoint{}, err
	}
	if version.WebSocketDebuggerURL == "" ||
		(!strings.Contains(strings.ToLower(version.Browser), "chrome") && !strings.Contains(strings.ToLower(version.Browser), "edg")) {
		return debugEndpoint{}, errors.New("на порту нет совместимого Chromium-браузера")
	}
	label := "Chromium"
	if strings.Contains(strings.ToLower(version.Browser), "edg") {
		label = "Microsoft Edge"
	}
	return debugEndpoint{
		URL:          endpoint,
		WebSocketURL: version.WebSocketDebuggerURL,
		Product:      version.Browser,
		Label:        label,
	}, nil
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

func discoverBrowserEndpoints(ctx context.Context, appDir, targetURL string) []debugEndpoint {
	var endpoints []string
	data, err := os.ReadFile(filepath.Join(appDir, "browser-debug.json"))
	if err == nil {
		var saved endpointSettings
		if json.Unmarshal(data, &saved) == nil && saved.Endpoint != "" {
			endpoints = append(endpoints, saved.Endpoint)
		}
	}

	ports := []int{9222, 9333}
	scanCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	script := `(Get-CimInstance Win32_Process -ErrorAction SilentlyContinue | Where-Object { $_.Name -in @('msedge.exe','chrome.exe','browser.exe') } | Select-Object -ExpandProperty CommandLine) -join [Environment]::NewLine`
	if output, err := exec.CommandContext(scanCtx, "powershell.exe", "-NoProfile", "-NonInteractive", "-Command", script).Output(); err == nil {
		matches := regexp.MustCompile(`(?i)--remote-debugging-port(?:=|\s+)(\d+)`).FindAllStringSubmatch(string(output), -1)
		for _, match := range matches {
			if port, err := strconv.Atoi(match[1]); err == nil && port > 0 && port <= 65535 {
				ports = append(ports, port)
			}
		}
	}
	for _, port := range ports {
		endpoints = append(endpoints, fmt.Sprintf("http://127.0.0.1:%d", port))
	}

	seen := make(map[string]struct{}, len(endpoints))
	var preferred []debugEndpoint
	var edge []debugEndpoint
	for _, endpoint := range endpoints {
		if _, exists := seen[endpoint]; exists {
			continue
		}
		seen[endpoint] = struct{}{}
		if info, err := probeBrowserInfo(ctx, endpoint); err == nil {
			if strings.Contains(strings.ToLower(info.Product), "edg") {
				if browserCompatible("edge", targetURL) {
					edge = append(edge, info)
				}
			} else {
				preferred = append(preferred, info)
			}
		}
	}
	return append(preferred, edge...)
}

type browserCandidate struct {
	Path  string
	Kind  string
	Label string
}

func findChromiumCandidates(targetURL string) []browserCandidate {
	var candidates []browserCandidate
	for _, name := range []string{"chrome.exe", "google-chrome", "chromium"} {
		if path, err := exec.LookPath(name); err == nil {
			kind, label := classifyBrowser(path)
			candidates = append(candidates, browserCandidate{Path: path, Kind: kind, Label: label})
		}
	}

	roots := []string{
		os.Getenv("LOCALAPPDATA"),
		os.Getenv("PROGRAMFILES(X86)"),
		os.Getenv("PROGRAMFILES"),
	}
	for _, root := range roots {
		if root == "" {
			continue
		}
		candidates = append(candidates,
			browserCandidate{Path: filepath.Join(root, "Google", "Chrome", "Application", "chrome.exe"), Kind: "chrome", Label: "Google Chrome"},
			browserCandidate{Path: filepath.Join(root, "Yandex", "YandexBrowser", "Application", "browser.exe"), Kind: "yandex", Label: "Яндекс Браузер"},
		)
	}
	for _, root := range roots {
		if root != "" {
			candidates = append(candidates,
				browserCandidate{Path: filepath.Join(root, "Microsoft", "Edge", "Application", "msedge.exe"), Kind: "edge", Label: "Microsoft Edge"},
			)
		}
	}
	for _, name := range []string{"msedge.exe", "msedge"} {
		if path, err := exec.LookPath(name); err == nil {
			candidates = append(candidates, browserCandidate{Path: path, Kind: "edge", Label: "Microsoft Edge"})
		}
	}

	result := make([]browserCandidate, 0, len(candidates))
	seen := make(map[string]struct{}, len(candidates))
	for _, candidate := range candidates {
		if !browserCompatible(candidate.Kind, targetURL) {
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
	case strings.Contains(lower, "yandex") || filepath.Base(lower) == "browser.exe":
		return "yandex", "Яндекс Браузер"
	case strings.Contains(lower, "chrome"):
		return "chrome", "Google Chrome"
	default:
		return "edge", "Microsoft Edge"
	}
}

func browserCompatible(kind, targetURL string) bool {
	parsed, err := url.Parse(targetURL)
	if err != nil {
		return false
	}
	host := strings.ToLower(parsed.Hostname())
	if kind == "edge" && (host == "salutejazz.ru" || strings.HasSuffix(host, ".salutejazz.ru") ||
		host == "sberjazz.ru" || strings.HasSuffix(host, ".sberjazz.ru") ||
		host == "jazz.sber.ru" || strings.HasSuffix(host, ".jazz.sber.ru")) {
		return false
	}
	return kind == "chrome" || kind == "yandex" || kind == "edge"
}

func firefoxInstalled() bool {
	for _, root := range []string{os.Getenv("PROGRAMFILES"), os.Getenv("PROGRAMFILES(X86)"), os.Getenv("LOCALAPPDATA")} {
		if root == "" {
			continue
		}
		if info, err := os.Stat(filepath.Join(root, "Mozilla Firefox", "firefox.exe")); err == nil && !info.IsDir() {
			return true
		}
	}
	return false
}

const mediaBridgeScript = `(() => {
  if (window.__presentationTimerBridgeInstalled) return;
  window.__presentationTimerBridgeInstalled = true;
  try {
    Object.defineProperty(navigator, 'webdriver', { get: () => undefined });
  } catch (_) {}

  let context;
  let destination;
  let silence;

  const ensureAudio = () => {
    if (destination) return destination;
    const AudioContextClass = window.AudioContext || window.webkitAudioContext;
    context = new AudioContextClass({ sampleRate: 48000 });
    destination = context.createMediaStreamDestination();
    silence = context.createConstantSource();
    const zero = context.createGain();
    zero.gain.value = 0;
    silence.connect(zero).connect(destination);
    silence.start();
    return destination;
  };

  const syntheticVideoTrack = () => {
    const canvas = document.createElement('canvas');
    canvas.width = 640;
    canvas.height = 360;
    const painter = canvas.getContext('2d');
    painter.fillStyle = '#111827';
    painter.fillRect(0, 0, canvas.width, canvas.height);
    return canvas.captureStream(1).getVideoTracks()[0];
  };

  const originalGetUserMedia = navigator.mediaDevices?.getUserMedia?.bind(navigator.mediaDevices);
  const originalEnumerateDevices = navigator.mediaDevices?.enumerateDevices?.bind(navigator.mediaDevices);
  window.__presentationTimerMediaUsed = false;
  window.__timerPeerCount = 0;
  const OriginalRTC = window.RTCPeerConnection;
  if (typeof OriginalRTC === 'function') {
    window.RTCPeerConnection = function(...args) {
      window.__timerPeerCount = (window.__timerPeerCount || 0) + 1;
      return new OriginalRTC(...args);
    };
    window.RTCPeerConnection.prototype = OriginalRTC.prototype;
  }
  if (navigator.mediaDevices && originalGetUserMedia) {
    navigator.mediaDevices.getUserMedia = async (constraints = {}) => {
      if (constraints.audio) window.__presentationTimerMediaUsed = true;
      const stream = new MediaStream();
      if (constraints.audio) {
        const audio = ensureAudio();
        if (context.state === 'suspended') await context.resume();
        audio.stream.getAudioTracks().forEach((track) => stream.addTrack(track));
      }
      if (constraints.video) {
        stream.addTrack(syntheticVideoTrack());
      }
      return stream;
    };
  }
  if (navigator.mediaDevices && originalEnumerateDevices) {
    navigator.mediaDevices.enumerateDevices = async () => {
      const devices = await originalEnumerateDevices();
      if (devices.some((device) => device.kind === 'audioinput')) return devices;
      return devices.concat({
        deviceId: 'presentation-timer',
        groupId: 'presentation-timer',
        kind: 'audioinput',
        label: 'Presentation Timer',
        toJSON() {
          return {
            deviceId: this.deviceId,
            groupId: this.groupId,
            kind: this.kind,
            label: this.label
          };
        }
      });
    };
  }

  window.__timerPlayWav = (base64) => {
    const audio = ensureAudio();
    if (context.state === 'suspended') context.resume();
    const binary = atob(base64);
    const bytes = new Uint8Array(binary.length);
    for (let i = 0; i < binary.length; i += 1) bytes[i] = binary.charCodeAt(i);
    context.decodeAudioData(bytes.buffer)
      .then((buffer) => {
        const source = context.createBufferSource();
        source.buffer = buffer;
        source.connect(audio);
        source.start();
      })
      .catch((error) => {
        window.__timerLastAudioError = String(error);
      });
    return true;
  };
})()`
