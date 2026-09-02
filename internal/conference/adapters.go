package conference

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/url"
	"strings"
	"time"
)

var ErrUnsupportedURL = errors.New("ссылка не относится к поддерживаемой ВКС")

type Browser interface {
	Evaluate(ctx context.Context, expression string, result any) error
	Description() string
}

type Adapter interface {
	ID() string
	Label() string
	Matches(hostname string) bool
	Join(ctx context.Context, browser Browser, displayName string, progress func(Phase, string)) error
}

type adapterConfig struct {
	id      string
	label   string
	domains []string
}

func (a adapterConfig) ID() string    { return a.id }
func (a adapterConfig) Label() string { return a.label }

func (a adapterConfig) Matches(hostname string) bool {
	host := strings.ToLower(strings.TrimSuffix(hostname, "."))
	for _, domain := range a.domains {
		if host == domain || strings.HasSuffix(host, "."+domain) {
			return true
		}
	}
	return false
}

func (a adapterConfig) Join(
	ctx context.Context,
	browser Browser,
	displayName string,
	progress func(Phase, string),
) error {
	deadline := time.Now().Add(5 * time.Minute)
	waitingReported := false
	connectingReported := false
	progress(PhaseConnecting, "Ожидание загрузки страницы ВКС")

	for time.Now().Before(deadline) {
		var result joinProbe
		expression := fmt.Sprintf(joinProbeScript, jsString(displayName), jsString(a.id))
		if err := browser.Evaluate(ctx, expression, &result); err != nil {
			return fmt.Errorf("%s: не удалось прочитать страницу: %w", a.label, err)
		}

		if result.Error != "" {
			return fmt.Errorf("%s: %s", a.label, result.Error)
		}
		if result.Joined {
			progress(PhaseJoined, "Участник подключён; выполните тест звука")
			return nil
		}
		if result.Waiting && !waitingReported {
			waitingReported = true
			progress(PhaseWaitingAdmission, "Ожидание допуска организатором")
		} else if !result.Waiting && !waitingReported && !connectingReported {
			connectingReported = true
			progress(PhaseConnecting, "Выполняется вход; при необходимости завершите его в браузере")
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(800 * time.Millisecond):
		}
	}

	return fmt.Errorf("%s: превышено время ожидания входа", a.label)
}

type joinProbe struct {
	Joined  bool   `json:"joined"`
	Waiting bool   `json:"waiting"`
	Error   string `json:"error"`
}

var adapters = []Adapter{
	adapterConfig{
		id:      "salutejazz",
		label:   "SaluteJazz",
		domains: []string{"salutejazz.ru", "sberjazz.ru", "jazz.sber.ru"},
	},
	adapterConfig{
		id:      "telemost",
		label:   "Яндекс Телемост",
		domains: []string{"telemost.yandex.ru", "telemost.yandex.com", "telemost.yandex.com.tr"},
	},
	adapterConfig{
		id:      "kontur-talk",
		label:   "Контур.Толк",
		domains: []string{"ktalk.ru", "talk.kontur.ru"},
	},
	adapterConfig{
		id:      "mts-link",
		label:   "МТС Линк",
		domains: []string{"mts-link.ru", "webinar.ru"},
	},
}

type Resolved struct {
	URL        string
	DisplayURL string
	Adapter    Adapter
}

func Resolve(rawURL string) (Resolved, error) {
	trimmed := strings.TrimSpace(rawURL)
	parsed, err := url.Parse(trimmed)
	if err != nil {
		return Resolved{}, fmt.Errorf("некорректная ссылка ВКС: %w", err)
	}
	if parsed.Scheme != "https" || parsed.Hostname() == "" {
		return Resolved{}, errors.New("ссылка ВКС должна быть полным HTTPS-адресом")
	}
	if net.ParseIP(parsed.Hostname()) != nil || parsed.User != nil {
		return Resolved{}, errors.New("адрес ВКС не должен содержать IP или данные авторизации")
	}

	for _, candidate := range adapters {
		if candidate.Matches(parsed.Hostname()) {
			display := parsed.Scheme + "://" + parsed.Hostname() + parsed.EscapedPath()
			return Resolved{
				URL:        parsed.String(),
				DisplayURL: display,
				Adapter:    candidate,
			}, nil
		}
	}
	return Resolved{}, ErrUnsupportedURL
}

func SupportedPlatforms() []Platform {
	result := make([]Platform, 0, len(adapters))
	for _, adapter := range adapters {
		result = append(result, Platform{ID: adapter.ID(), Label: adapter.Label()})
	}
	return result
}

type Platform struct {
	ID    string `json:"id"`
	Label string `json:"label"`
}

func jsString(value string) string {
	return fmt.Sprintf("%q", value)
}

const joinProbeScript = `(async () => {
  const displayName = %s;
  const platform = %s;
  const sleep = (ms) => new Promise((resolve) => setTimeout(resolve, ms));
  const normalize = (value) => (value || '').replace(/\s+/g, ' ').trim().toLowerCase();
  const visible = (element) => {
    if (!element || element.disabled) return false;
    const style = getComputedStyle(element);
    const rect = element.getBoundingClientRect();
    return style.visibility !== 'hidden' && style.display !== 'none' && rect.width > 0 && rect.height > 0;
  };
  const elements = (selector) => {
    const result = [];
    const visit = (root) => {
      result.push(...root.querySelectorAll(selector));
      root.querySelectorAll('*').forEach((node) => {
        if (node.shadowRoot) visit(node.shadowRoot);
      });
    };
    visit(document);
    return result;
  };
  const textOf = (element) => normalize([
    element.innerText,
    element.textContent,
    element.getAttribute?.('aria-label'),
    element.getAttribute?.('title'),
    element.getAttribute?.('data-testid'),
    element.getAttribute?.('data-qa'),
    element.getAttribute?.('name')
  ].filter(Boolean).join(' '));
  const clickText = (needles, excluded = []) => {
    const target = elements('button, [role="button"], a, [role="link"]')
      .find((element) => visible(element)
        && needles.some((needle) => textOf(element).includes(needle))
        && !excluded.some((needle) => textOf(element).includes(needle)));
    if (!target) return false;
    target.click();
    return true;
  };
  const detectJoined = () => {
    const pageText = normalize(document.body?.innerText || '');
    const controls = elements('button, [role="button"], [aria-label], [title], [data-testid]')
      .filter((element) => visible(element))
      .map(textOf);
    const hasLeave = controls.some((text) => /покинуть|завершить звонок|завершить встречу|выйти из встречи|отключиться от|leave call|hang up|disconnect|end call|выйти из комнаты|leave meeting/.test(text));
    const meetingControls = controls.filter((text) => /микрофон|камер|участник|чат|настрой|share|демонстрац|screen|поделиться|record|запис|hand|рук|mute|unmute|video|audio|speaker|динамик|реакц|devices|устройств/.test(text));
    const visibleJoinButton = elements('button, [role="button"], a, [role="link"]')
      .some((element) => visible(element) && /присоединиться|подключиться|войти во встречу|войти в комнату|join meeting|join now|войти$/.test(textOf(element)));
    const visibleNameField = elements('input, textarea').some((input) => {
      if (!visible(input)) return false;
      const hint = normalize([
        input.placeholder,
        input.getAttribute('aria-label'),
        input.name,
        input.id
      ].filter(Boolean).join(' '));
      return /имя|фио|name|ник/.test(hint);
    });
    const activeVideos = elements('video').filter((video) => visible(video) && (video.videoWidth > 0 || video.readyState >= 2)).length;
    const inCallText = /вы в конференции|в эфире|встреча началась|connected to meeting|you are in the meeting|ожидание других участников|вы подключились|подключение установлено|в комнате|in the meeting|waiting for others/.test(pageText);
    const prejoinVisible = visibleNameField || visibleJoinButton;
    const mediaUsed = Boolean(window.__presentationTimerMediaUsed);
    const peerConnections = Number(window.__timerPeerCount || 0);
    const mediaReady = Boolean(window.__presentationTimerBridgeInstalled);
    if (mediaReady && mediaUsed && !prejoinVisible) return true;
    if (mediaReady && peerConnections > 0 && !prejoinVisible) return true;
    if (mediaReady && mediaUsed && meetingControls.length >= 1) return true;
    return hasLeave
      || inCallText
      || (!prejoinVisible && meetingControls.length >= 2)
      || (!prejoinVisible && activeVideos >= 1 && meetingControls.length >= 1);
  };

  window.__timerJoinAttempts = (window.__timerJoinAttempts || 0) + 1;
  const shouldAct = window.__timerJoinAttempts <= 12;

  const pageText = normalize(document.body?.innerText || '');
  if (/ссылка.*недействительна|встреча.*не найдена|конференция.*не найдена|мероприятие.*завершено|браузер не поддерживается/.test(pageText)) {
    return { joined: false, waiting: false, error: 'Встреча не найдена, завершена или браузер не поддерживается' };
  }

  clickText(['продолжить в браузере', 'открыть в браузере', 'войти через браузер', 'web version', 'use browser']);

  if (detectJoined()) {
    clickText(['включить микрофон', 'unmute', 'микрофон выключен', 'turn on microphone'], ['настрой']);
    return { joined: true, waiting: false, error: '' };
  }

  if (shouldAct) {
    const nameInput = elements('input, textarea').find((input) => {
      if (!visible(input)) return false;
      const hint = normalize([
        input.placeholder,
        input.getAttribute('aria-label'),
        input.name,
        input.id
      ].filter(Boolean).join(' '));
      return /имя|фио|name|ник/.test(hint);
    });
    if (nameInput && !nameInput.value) {
      const setter = Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')?.set;
      if (setter && nameInput instanceof HTMLInputElement) setter.call(nameInput, displayName);
      else nameInput.value = displayName;
      nameInput.dispatchEvent(new Event('input', { bubbles: true }));
      nameInput.dispatchEvent(new Event('change', { bubbles: true }));
    }
  }

  if (detectJoined()) {
    clickText(['включить микрофон', 'unmute', 'микрофон выключен', 'turn on microphone'], ['настрой']);
    return { joined: true, waiting: false, error: '' };
  }

  const waiting = /ожидайте.*допуск|организатор.*допуст|запрос.*отправлен|ожидание.*подключ|waiting for the host|waiting to be admitted/.test(pageText);
  if (!waiting && shouldAct) {
    clickText(['включить микрофон', 'unmute', 'микрофон выключен', 'turn on microphone'], ['настрой']);
    const joinWords = platform === 'mts-link'
      ? ['подключиться', 'войти в мероприятие', 'присоединиться', 'join']
      : ['присоединиться', 'подключиться', 'войти во встречу', 'продолжить', 'войти', 'join'];
    clickText(joinWords, ['создать', 'зарегистр', 'войти через', 'войти в аккаунт']);
    await sleep(400);
    if (detectJoined()) {
      return { joined: true, waiting: false, error: '' };
    }
  }

  return { joined: detectJoined(), waiting, error: '' };
})()`
