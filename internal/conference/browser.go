package conference

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"

	cdpbrowser "github.com/chromedp/cdproto/browser"
	"github.com/chromedp/cdproto/emulation"
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

	var browserCtx context.Context
	var browserCancel context.CancelFunc
	if pageID := existingPageTargetID(parent, endpoint); pageID != "" {
		browserCtx, browserCancel = chromedp.NewContext(allocatorCtx, chromedp.WithTargetID(target.ID(pageID)))
	} else {
		browserCtx, browserCancel = chromedp.NewContext(allocatorCtx)
	}
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
			return applyChromeUserAgent(ctx)
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
		_ = applyChromeUserAgent(ctx)
		if _, err := page.AddScriptToEvaluateOnNewDocument(mediaBridgeScript).Do(ctx); err != nil {
			return nil
		}
		_, _, _ = runtime.Evaluate(mediaBridgeScript).Do(ctx)
		return nil
	}))
}

func pickLocalDebugPort() (int, error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, fmt.Errorf("не удалось выбрать порт отладки: %w", err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	_ = listener.Close()
	return port, nil
}

func waitForDebugEndpoint(ctx context.Context, endpoint string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if _, err := probeBrowserEndpoint(ctx, endpoint); err == nil {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(200 * time.Millisecond):
		}
	}
	return fmt.Errorf("порт отладки %s не открылся за %s", endpoint, timeout)
}

type browserVersion struct {
	Browser              string `json:"Browser"`
	WebSocketDebuggerURL string `json:"webSocketDebuggerUrl"`
}

type jsonTarget struct {
	ID   string `json:"id"`
	Type string `json:"type"`
	URL  string `json:"url"`
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
	if err := json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(&version); err != nil {
		return debugEndpoint{}, err
	}
	product := strings.ToLower(version.Browser)
	if version.WebSocketDebuggerURL == "" ||
		(!strings.Contains(product, "chrome") && !strings.Contains(product, "edg") && !strings.Contains(product, "webview")) {
		return debugEndpoint{}, errors.New("на порту нет совместимого Chromium-браузера")
	}
	label := "Chromium"
	if strings.Contains(product, "edg") || strings.Contains(product, "webview") {
		label = "Microsoft Edge WebView2"
	}
	return debugEndpoint{
		URL:          endpoint,
		WebSocketURL: version.WebSocketDebuggerURL,
		Product:      version.Browser,
		Label:        label,
	}, nil
}

func existingPageTargetID(ctx context.Context, endpoint string) string {
	parsed, err := url.Parse(endpoint)
	if err != nil || parsed.Scheme != "http" || (parsed.Hostname() != "127.0.0.1" && parsed.Hostname() != "localhost") {
		return ""
	}
	requestCtx, cancel := context.WithTimeout(ctx, 800*time.Millisecond)
	defer cancel()
	request, err := http.NewRequestWithContext(requestCtx, http.MethodGet, strings.TrimRight(endpoint, "/")+"/json/list", nil)
	if err != nil {
		return ""
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return ""
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return ""
	}
	var targets []jsonTarget
	if err := json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(&targets); err != nil {
		return ""
	}
	for _, item := range targets {
		switch item.Type {
		case "page", "webview":
			if item.ID != "" {
				return item.ID
			}
		}
	}
	return ""
}

const defaultChromeDesktopUA = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/140.0.0.0 Safari/537.36"

var edgeUserAgentToken = regexp.MustCompile(`(?i)\s*(?:Edg(?:A|iOS)?|Edge?)\/[\d.]+`)

func chromeLikeUserAgent(ua string) string {
	ua = strings.TrimSpace(edgeUserAgentToken.ReplaceAllString(ua, ""))
	if ua == "" {
		return defaultChromeDesktopUA
	}
	return ua
}

func chromeVersionMajor(ua string) string {
	if match := regexp.MustCompile(`Chrome/(\d+)`).FindStringSubmatch(ua); len(match) == 2 {
		return match[1]
	}
	return "140"
}

func applyChromeUserAgent(ctx context.Context) error {
	var ua string
	if err := chromedp.Evaluate(`navigator.userAgent`, &ua).Do(ctx); err != nil || strings.TrimSpace(ua) == "" {
		ua = defaultChromeDesktopUA
	}
	chromeUA := chromeLikeUserAgent(ua)
	major := chromeVersionMajor(chromeUA)
	full := major + ".0.0.0"
	brands := []*emulation.UserAgentBrandVersion{
		{Brand: "Not A(Brand", Version: "8"},
		{Brand: "Chromium", Version: major},
		{Brand: "Google Chrome", Version: major},
	}
	return emulation.SetUserAgentOverride(chromeUA).
		WithPlatform("Win32").
		WithUserAgentMetadata(&emulation.UserAgentMetadata{
			Brands: brands,
			FullVersionList: []*emulation.UserAgentBrandVersion{
				{Brand: "Not A(Brand", Version: "10.0.0.0"},
				{Brand: "Chromium", Version: full},
				{Brand: "Google Chrome", Version: full},
			},
			Platform:        "Windows",
			PlatformVersion: "15.0.0",
			Architecture:    "x86",
			Model:           "",
			Mobile:          false,
			Bitness:         "64",
			Wow64:           false,
		}).Do(ctx)
}

const diagnosticsScript = `JSON.stringify(typeof window.__timerGetDiagnostics === 'function' ? window.__timerGetDiagnostics() : {error:'bridge missing'})`

const setReceiveMutedScript = `Boolean(window.__timerSetReceiveMuted && window.__timerSetReceiveMuted(%v))`

const mediaBridgeScript = `(function __timerInstallMediaBridge() {
  if (window.__presentationTimerBridgeInstalled) return;
  window.__presentationTimerBridgeInstalled = true;
  window.__timerInstallMediaBridge = __timerInstallMediaBridge;
  try {
    Object.defineProperty(navigator, 'webdriver', { get: () => undefined });
  } catch (_) {}
  try {
    const ua = String(navigator.userAgent || '').replace(/\sEdg(?:A|iOS)?\/[\d.]+/gi, '').replace(/\sEdge?\/[\d.]+/gi, '');
    const chrome = (ua.match(/Chrome\/(\d+)/) || [])[1] || '140';
    Object.defineProperty(navigator, 'userAgent', { configurable: true, get: () => ua });
    Object.defineProperty(navigator, 'appVersion', { configurable: true, get: () => ua.replace(/^Mozilla\//, '') });
    Object.defineProperty(navigator, 'vendor', { configurable: true, get: () => 'Google Inc.' });
    if (navigator.userAgentData) {
      const brands = [
        { brand: 'Not A(Brand', version: '8' },
        { brand: 'Chromium', version: chrome },
        { brand: 'Google Chrome', version: chrome }
      ];
      try { Object.defineProperty(navigator.userAgentData, 'brands', { configurable: true, get: () => brands }); } catch (_) {}
      const originalHints = navigator.userAgentData.getHighEntropyValues && navigator.userAgentData.getHighEntropyValues.bind(navigator.userAgentData);
      if (originalHints) {
        navigator.userAgentData.getHighEntropyValues = async (hints) => {
          const values = await originalHints(hints);
          values.brands = brands;
          values.fullVersionList = brands.map((item) => ({
            brand: item.brand,
            version: item.brand === 'Not A(Brand' ? '10.0.0.0' : chrome + '.0.0.0'
          }));
          values.uaFullVersion = chrome + '.0.0.0';
          return values;
        };
      }
    }
  } catch (_) {}

  window.__timerDebugLog = window.__timerDebugLog || [];
  window.__timerLastLevel = 0;
  window.__timerLastAudioError = '';
  window.__presentationTimerMediaUsed = false;
  window.__timerPeerCount = 0;
  window.__timerReceiveMuted = true;
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
    track.__timerSynthetic = true;
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

  const muteRemoteMediaElement = (el) => {
    if (!el || el.__timerAllowedMedia || !window.__timerReceiveMuted) return;
    try {
      el.muted = true;
      el.volume = 0;
      el.__timerRemoteMuted = true;
    } catch (_) {}
  };

  const muteRemoteTrack = (track) => {
    if (!track || track.kind !== 'audio' || track.__timerSynthetic || !window.__timerReceiveMuted) return;
    try { track.enabled = false; } catch (_) {}
  };

  const suppressRemotePlayback = () => {
    if (!window.__timerReceiveMuted) return;
    try {
      document.querySelectorAll('audio, video').forEach(muteRemoteMediaElement);
    } catch (_) {}
  };

  window.__timerSetReceiveMuted = (muted) => {
    window.__timerReceiveMuted = Boolean(muted);
    log('receive.muted', { muted: window.__timerReceiveMuted });
    if (window.__timerReceiveMuted) suppressRemotePlayback();
    document.querySelectorAll('iframe').forEach((frame) => {
      try {
        injectIntoFrame(frame);
        const child = frame.contentWindow;
        if (child && child !== window && child.__timerSetReceiveMuted) child.__timerSetReceiveMuted(muted);
      } catch (_) {}
    });
    return true;
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
      const pc = new OriginalRTC(...args);
      pc.addEventListener('track', (event) => {
        if (event.track) muteRemoteTrack(event.track);
        if (event.streams) {
          event.streams.forEach((stream) => {
            stream.getAudioTracks().forEach(muteRemoteTrack);
          });
        }
      });
      return pc;
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

  setInterval(() => { resumeContext(); updateLevel(); injectChildFrames(); suppressRemotePlayback(); }, 2000);
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
