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
	"sync"
	"time"

	cdpbrowser "github.com/chromedp/cdproto/browser"
	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/cdproto/runtime"
	"github.com/chromedp/cdproto/target"
	"github.com/chromedp/chromedp"
)

type attachedTarget struct {
	Type string `json:"type"`
	URL  string `json:"url"`
}

type chromeBrowser struct {
	ctx         context.Context
	description string
	mu          sync.Mutex
	attached    []attachedTarget
}

func (b *chromeBrowser) Evaluate(ctx context.Context, expression string, result any) error {
	return chromedp.Run(b.ctx, chromedp.Evaluate(expression, result))
}

func (b *chromeBrowser) Description() string {
	return b.description
}

func (b *chromeBrowser) Done() <-chan struct{} {
	return b.ctx.Done()
}

func (b *chromeBrowser) recordAttach(info *target.Info) {
	if b == nil || info == nil {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	b.attached = append(b.attached, attachedTarget{Type: info.Type, URL: info.URL})
	if len(b.attached) > 50 {
		b.attached = b.attached[len(b.attached)-50:]
	}
}

func (b *chromeBrowser) Attachments() []attachedTarget {
	if b == nil {
		return nil
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	out := make([]attachedTarget, len(b.attached))
	copy(out, b.attached)
	return out
}

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

	result := &chromeBrowser{ctx: browserCtx, description: description}
	chromedp.ListenTarget(browserCtx, func(ev any) {
		attached, ok := ev.(*target.EventAttachedToTarget)
		if !ok || attached.TargetInfo == nil {
			return
		}
		info := attached.TargetInfo
		switch info.Type {
		case "iframe", "page", "webview":
		default:
			return
		}
		if c := chromedp.FromContext(browserCtx); c != nil && c.Target != nil && c.Target.TargetID == info.TargetID {
			return
		}
		result.recordAttach(info)
		go injectMediaBridgeIntoTarget(browserCtx, info)
	})

	err = chromedp.Run(browserCtx,
		chromedp.ActionFunc(func(ctx context.Context) error {
			grantMediaPermissions(ctx, targetURL)
			return nil
		}),
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

	return result, cancel, nil
}

func grantMediaPermissions(ctx context.Context, targetURL string) {
	origin := ""
	if parsed, err := url.Parse(targetURL); err == nil && parsed.Scheme != "" && parsed.Host != "" {
		origin = parsed.Scheme + "://" + parsed.Host
	}
	perms := []cdpbrowser.PermissionType{
		cdpbrowser.PermissionTypeAudioCapture,
		cdpbrowser.PermissionTypeVideoCapture,
	}
	grant := cdpbrowser.GrantPermissions(perms)
	if origin != "" {
		grant = grant.WithOrigin(origin)
	}
	_ = grant.Do(ctx)
	for _, name := range []string{"microphone", "camera"} {
		set := cdpbrowser.SetPermission(&cdpbrowser.PermissionDescriptor{Name: name}, cdpbrowser.PermissionSettingGranted)
		if origin != "" {
			set = set.WithOrigin(origin)
		}
		_ = set.Do(ctx)
	}
}

func injectMediaBridgeIntoTarget(parent context.Context, info *target.Info) {
	if info == nil || info.TargetID == "" {
		return
	}
	ctx, cancel := chromedp.NewContext(parent, chromedp.WithTargetID(info.TargetID))
	defer cancel()
	runCtx, runCancel := context.WithTimeout(ctx, 8*time.Second)
	defer runCancel()
	_ = chromedp.Run(runCtx, chromedp.ActionFunc(func(ctx context.Context) error {
		if _, err := page.AddScriptToEvaluateOnNewDocument(mediaBridgeScript).Do(ctx); err != nil {
			return nil
		}
		_, _, _ = runtime.Evaluate(mediaBridgeScript).Do(ctx)
		return nil
	}))
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

func discoverBrowserEndpoints(ctx context.Context, appDir, platformID string) []debugEndpoint {
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
				if browserCompatible("edge", platformID) {
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

func findChromiumCandidates(platformID string) []browserCandidate {
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
	case strings.Contains(lower, "yandex") || filepath.Base(lower) == "browser.exe":
		return "yandex", "Яндекс Браузер"
	case strings.Contains(lower, "chrome"):
		return "chrome", "Google Chrome"
	default:
		return "edge", "Microsoft Edge"
	}
}

func browserCompatible(kind, platformID string) bool {
	if kind == "edge" && platformID == "salutejazz" {
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

const diagnosticsScript = `JSON.stringify(typeof window.__timerGetDiagnostics === 'function' ? window.__timerGetDiagnostics() : {error:'bridge missing'})`

const mediaBridgeScript = `(function __timerInstallMediaBridge() {
  if (window.__presentationTimerBridgeInstalled) return;
  window.__presentationTimerBridgeInstalled = true;
  window.__timerInstallMediaBridge = __timerInstallMediaBridge;
  try {
    Object.defineProperty(navigator, 'webdriver', { get: () => undefined });
  } catch (_) {}

  window.__timerDebugLog = window.__timerDebugLog || [];
  window.__timerLastLevel = 0;
  window.__timerLastAudioError = '';
  window.__presentationTimerMediaUsed = false;
  window.__timerPeerCount = 0;
  const log = (event, data) => {
    try {
      window.__timerDebugLog.push({ t: Date.now(), event, data: data || {} });
      if (window.__timerDebugLog.length > 200) window.__timerDebugLog.splice(0, window.__timerDebugLog.length - 200);
    } catch (_) {}
  };
  log('bridge.install', { href: String(location.href || ''), frame: window !== window.top });

  let context;
  let destination;
  let mix;
  let analyser;
  let silence;
  let pendingBuffers = [];

  const updateLevel = () => {
    if (!analyser) return 0;
    const data = new Uint8Array(analyser.fftSize);
    analyser.getByteTimeDomainData(data);
    let sum = 0;
    for (let i = 0; i < data.length; i += 1) {
      const v = (data[i] - 128) / 128;
      sum += v * v;
    }
    window.__timerLastLevel = Math.sqrt(sum / data.length);
    return window.__timerLastLevel;
  };

  const flushQueue = () => {
    if (!context || !mix || context.state === 'suspended') return;
    while (pendingBuffers.length) {
      const buffer = pendingBuffers.shift();
      playBuffer(buffer);
    }
  };

  const resumeContext = async () => {
    if (!context) return;
    if (context.state === 'suspended') {
      try {
        await context.resume();
        log('context.resume', { state: context.state });
      } catch (error) {
        log('context.resume.error', { error: String(error) });
      }
    }
    flushQueue();
  };

  const playBuffer = (buffer) => {
    const source = context.createBufferSource();
    source.buffer = buffer;
    source.connect(mix);
    source.onended = () => log('playWav.end', { duration: buffer.duration });
    source.start();
    log('playWav.start', { duration: buffer.duration, state: context.state });
    setTimeout(updateLevel, 80);
  };

  const ensureAudio = () => {
    if (destination) return destination;
    const AudioContextClass = window.AudioContext || window.webkitAudioContext;
    context = new AudioContextClass({ sampleRate: 48000 });
    destination = context.createMediaStreamDestination();
    mix = context.createGain();
    mix.gain.value = 1;
    analyser = context.createAnalyser();
    analyser.fftSize = 256;
    silence = context.createConstantSource();
    const zero = context.createGain();
    zero.gain.value = 0;
    silence.connect(zero);
    zero.connect(mix);
    mix.connect(destination);
    mix.connect(analyser);
    silence.start();
    log('audio.created', { state: context.state, sampleRate: context.sampleRate });
    resumeContext();
    return destination;
  };

  const hardenAudioTrack = (track) => {
    if (!track || track.__timerHardened) return track;
    track.__timerHardened = true;
    const settings = {
      deviceId: 'presentation-timer',
      groupId: 'presentation-timer',
      echoCancellation: false,
      noiseSuppression: false,
      autoGainControl: false,
      sampleRate: 48000,
      channelCount: 1
    };
    try { Object.defineProperty(track, 'label', { configurable: true, get: () => 'Presentation Timer' }); } catch (_) {}
    track.getSettings = () => Object.assign({}, settings);
    track.getCapabilities = () => ({
      deviceId: 'presentation-timer',
      groupId: 'presentation-timer',
      echoCancellation: [false, true],
      noiseSuppression: [false, true],
      autoGainControl: [false, true],
      sampleRate: { min: 8000, max: 48000 },
      channelCount: { min: 1, max: 2 }
    });
    const originalApply = track.applyConstraints ? track.applyConstraints.bind(track) : null;
    track.applyConstraints = async (constraints) => {
      log('track.applyConstraints', constraints || {});
      try {
        if (originalApply) await originalApply({});
      } catch (_) {}
    };
    const originalClone = track.clone ? track.clone.bind(track) : null;
    track.clone = () => {
      const cloned = originalClone ? originalClone() : ensureAudio().stream.getAudioTracks()[0];
      return hardenAudioTrack(cloned);
    };
    ['mute', 'unmute', 'ended'].forEach((name) => {
      track.addEventListener(name, () => log('track.' + name, { enabled: track.enabled, muted: track.muted, readyState: track.readyState }));
    });
    window.__timerAudioTrack = track;
    log('track.harden', { readyState: track.readyState, enabled: track.enabled, muted: track.muted });
    return track;
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

  const virtualMic = () => ({
    deviceId: 'presentation-timer',
    groupId: 'presentation-timer',
    kind: 'audioinput',
    label: 'Presentation Timer',
    toJSON() {
      return { deviceId: this.deviceId, groupId: this.groupId, kind: this.kind, label: this.label };
    }
  });

  if (navigator.permissions && navigator.permissions.query) {
    const originalQuery = navigator.permissions.query.bind(navigator.permissions);
    navigator.permissions.query = async (desc) => {
      const name = desc && desc.name;
      log('permissions.query', { name: name || '' });
      if (name === 'microphone' || name === 'camera') {
        return {
          state: 'granted',
          status: 'granted',
          onchange: null,
          addEventListener() {},
          removeEventListener() {},
          dispatchEvent() { return false; }
        };
      }
      return originalQuery(desc);
    };
  }

  const originalGetUserMedia = navigator.mediaDevices?.getUserMedia?.bind(navigator.mediaDevices);
  const originalEnumerateDevices = navigator.mediaDevices?.enumerateDevices?.bind(navigator.mediaDevices);
  const OriginalRTC = window.RTCPeerConnection;
  if (typeof OriginalRTC === 'function') {
    window.RTCPeerConnection = function(...args) {
      window.__timerPeerCount = (window.__timerPeerCount || 0) + 1;
      log('rtc.create', { count: window.__timerPeerCount });
      return new OriginalRTC(...args);
    };
    window.RTCPeerConnection.prototype = OriginalRTC.prototype;
  }
  if (navigator.mediaDevices && originalGetUserMedia) {
    navigator.mediaDevices.getUserMedia = async (constraints = {}) => {
      log('getUserMedia', constraints || {});
      if (constraints.audio) window.__presentationTimerMediaUsed = true;
      const stream = new MediaStream();
      if (constraints.audio) {
        ensureAudio();
        await resumeContext();
        ensureAudio().stream.getAudioTracks().forEach((track) => stream.addTrack(hardenAudioTrack(track)));
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
      log('enumerateDevices', { count: devices.length });
      if (devices.some((device) => device.deviceId === 'presentation-timer' || device.label === 'Presentation Timer')) {
        return devices;
      }
      return devices.concat(virtualMic());
    };
  }

  if (typeof window.open === 'function') {
    const originalOpen = window.open.bind(window);
    window.open = (...args) => {
      log('window.open', { url: String(args[0] || '') });
      return originalOpen(...args);
    };
  }
  const installerSource = '(' + __timerInstallMediaBridge.toString() + ')()';
  const injectIntoFrame = (frame) => {
    if (!frame || frame.tagName !== 'IFRAME') return;
    const src = frame.src || frame.getAttribute('src') || '';
    const tryInject = () => {
      try {
        const child = frame.contentWindow;
        if (!child || child === window) return;
        if (child.__presentationTimerBridgeInstalled) {
          log('iframe.ready', { src: src, mediaUsed: Boolean(child.__presentationTimerMediaUsed), peerCount: Number(child.__timerPeerCount || 0) });
          return;
        }
        try {
          child.eval(installerSource);
        } catch (_) {
          const script = child.document.createElement('script');
          script.textContent = installerSource;
          (child.document.documentElement || child.document.head || child.document.body).appendChild(script);
          script.remove();
        }
        log('iframe.inject', { src: src, installed: Boolean(child.__presentationTimerBridgeInstalled) });
      } catch (error) {
        log('iframe.inject.fail', { src: src, error: String(error) });
      }
    };
    tryInject();
    if (!frame.__timerInjectBound) {
      frame.__timerInjectBound = true;
      frame.addEventListener('load', tryInject);
    }
  };
  const injectChildFrames = () => {
    try {
      document.querySelectorAll('iframe').forEach(injectIntoFrame);
    } catch (error) {
      log('iframe.watch.error', { error: String(error) });
    }
  };
  const watchIframes = () => {
    injectChildFrames();
    try {
      const observer = new MutationObserver((mutations) => {
        mutations.forEach((mutation) => {
          mutation.addedNodes.forEach((node) => {
            if (node.tagName === 'IFRAME') injectIntoFrame(node);
            else if (node.querySelectorAll) node.querySelectorAll('iframe').forEach(injectIntoFrame);
          });
        });
      });
      observer.observe(document.documentElement || document, { childList: true, subtree: true });
    } catch (error) {
      log('iframe.watch.error', { error: String(error) });
    }
  };
  if (document.readyState === 'loading') document.addEventListener('DOMContentLoaded', watchIframes);
  else watchIframes();

  setInterval(() => { resumeContext(); updateLevel(); injectChildFrames(); }, 2000);
  ['click', 'keydown', 'pointerdown'].forEach((name) => {
    window.addEventListener(name, () => { resumeContext(); }, true);
  });

  const playLocalWav = (base64) => {
    ensureAudio();
    const binary = atob(base64);
    const bytes = new Uint8Array(binary.length);
    for (let i = 0; i < binary.length; i += 1) bytes[i] = binary.charCodeAt(i);
    log('playWav.call', { bytes: bytes.length, state: context.state });
    context.decodeAudioData(bytes.buffer.slice(0))
      .then(async (buffer) => {
        log('playWav.decode', { duration: buffer.duration, state: context.state });
        await resumeContext();
        if (context.state === 'suspended') {
          pendingBuffers.push(buffer);
          log('playWav.queued', { queued: pendingBuffers.length });
          return;
        }
        playBuffer(buffer);
      })
      .catch((error) => {
        window.__timerLastAudioError = String(error);
        log('playWav.decode.error', { error: String(error) });
      });
    return true;
  };
  window.__timerPlayLocalWav = playLocalWav;
  window.__timerPlayWav = (base64) => {
    playLocalWav(base64);
    document.querySelectorAll('iframe').forEach((frame) => {
      try {
        injectIntoFrame(frame);
        const child = frame.contentWindow;
        if (child && child !== window && child.__timerPlayLocalWav) child.__timerPlayLocalWav(base64);
      } catch (_) {}
    });
    return true;
  };

  const localDiagnostics = () => {
    updateLevel();
    const track = window.__timerAudioTrack;
    let trackSettings = null;
    try { trackSettings = track && track.getSettings ? track.getSettings() : null; } catch (_) {}
    return {
      href: String(location.href || ''),
      frame: window !== window.top,
      log: window.__timerDebugLog || [],
      contextState: context ? context.state : '',
      trackReadyState: track ? track.readyState : '',
      trackEnabled: track ? track.enabled : null,
      trackMuted: track ? track.muted : null,
      trackSettings,
      lastLevel: window.__timerLastLevel || 0,
      peerCount: Number(window.__timerPeerCount || 0),
      mediaUsed: Boolean(window.__presentationTimerMediaUsed),
      lastAudioError: window.__timerLastAudioError || '',
      iframeCount: document.querySelectorAll ? document.querySelectorAll('iframe').length : 0
    };
  };
  window.__timerGetLocalDiagnostics = localDiagnostics;
  window.__timerGetDiagnostics = () => {
    const snapshot = localDiagnostics();
    const frames = [];
    document.querySelectorAll('iframe').forEach((frame) => {
      try {
        injectIntoFrame(frame);
        const child = frame.contentWindow;
        if (child && child.__timerGetLocalDiagnostics) frames.push(child.__timerGetLocalDiagnostics());
        else frames.push({ src: frame.src || '', error: 'bridge missing in iframe' });
      } catch (error) {
        frames.push({ src: frame.src || '', error: String(error) });
      }
    });
    snapshot.frames = frames;
    return snapshot;
  };
})()`
