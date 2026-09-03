package conference

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
)

type Controller struct {
	mu            sync.Mutex
	state         *stateStore
	cancel        context.CancelFunc
	joinCancel    context.CancelFunc
	browserCancel context.CancelFunc
	browser       Browser
	runID         uint64
}

func NewController(onChange func(State)) *Controller {
	return &Controller{state: newStateStore(onChange)}
}

func (c *Controller) GetState() State {
	return c.state.get()
}

func (c *Controller) Connect(rawURL, displayName string) (State, error) {
	resolved, err := Resolve(rawURL)
	if err != nil {
		return c.GetState(), err
	}
	name := strings.TrimSpace(displayName)
	if name == "" {
		name = "Таймер"
	}
	if len([]rune(name)) > 80 {
		return c.GetState(), errors.New("имя участника не должно быть длиннее 80 символов")
	}

	c.mu.Lock()
	if c.cancel != nil {
		c.mu.Unlock()
		return c.GetState(), ErrAlreadyRunning
	}
	ctx, cancel := context.WithCancel(context.Background())
	c.runID++
	runID := c.runID
	c.cancel = cancel
	c.mu.Unlock()

	c.state.update(func(state *State) {
		*state = State{
			Phase:      PhaseOpening,
			Platform:   resolved.Adapter.Label(),
			DisplayURL: resolved.DisplayURL,
			Message:    "Открытие браузера",
		}
	})

	go c.run(ctx, runID, resolved, name)
	return c.GetState(), nil
}

func (c *Controller) run(ctx context.Context, runID uint64, resolved Resolved, displayName string) {
	browser, browserCancel, err := openBrowser(ctx, resolved.URL, resolved.Adapter.ID())
	if err != nil {
		if !errors.Is(err, context.Canceled) && c.isCurrentRun(runID) {
			c.fail(err)
		}
		c.clearSession(runID)
		return
	}

	c.mu.Lock()
	if c.runID != runID || c.cancel == nil {
		c.mu.Unlock()
		browserCancel()
		return
	}
	c.browser = browser
	c.browserCancel = browserCancel
	joinCtx, joinCancel := context.WithCancel(ctx)
	c.joinCancel = joinCancel
	c.mu.Unlock()
	go c.watchBrowser(ctx, runID, browser.Done())

	progress := func(phase Phase, message string) {
		c.updateIfCurrent(runID, func(state *State) {
			state.Phase = phase
			state.Message = message
		})
	}
	progress(PhaseConnecting, browser.Description()+". Выполняется вход во встречу")
	if err := resolved.Adapter.Join(joinCtx, browser, displayName, progress); err != nil {
		if errors.Is(err, context.Canceled) && c.isCurrentRun(runID) && c.GetState().Phase == PhaseJoined {
			<-ctx.Done()
			browserCancel()
			c.clearSession(runID)
			return
		}
		if !errors.Is(err, context.Canceled) && c.isCurrentRun(runID) {
			c.fail(err)
		}
		c.clearSession(runID)
		browserCancel()
		return
	}

	if !c.updateIfCurrent(runID, func(state *State) {
		state.Phase = PhaseJoined
		state.Message = "Участник подключён; выполните тест звука"
		state.Tested = false
	}) {
		browserCancel()
		return
	}

	<-ctx.Done()
	browserCancel()
	c.clearSession(runID)
}

func (c *Controller) Disconnect() {
	c.mu.Lock()
	cancel := c.cancel
	joinCancel := c.joinCancel
	browserCancel := c.browserCancel
	c.runID++
	c.cancel = nil
	c.joinCancel = nil
	c.browserCancel = nil
	c.browser = nil
	c.mu.Unlock()

	if cancel != nil {
		cancel()
	}
	if joinCancel != nil {
		joinCancel()
	}
	if browserCancel != nil {
		browserCancel()
	}
	c.state.update(func(state *State) {
		state.Phase = PhaseLeft
		state.Message = "Участник отключён"
		state.Tested = false
	})
}

func (c *Controller) ConfirmJoined() error {
	c.mu.Lock()
	browser := c.browser
	joinCancel := c.joinCancel
	runID := c.runID
	c.mu.Unlock()
	if browser == nil {
		state := c.GetState()
		if state.Phase == PhaseError && state.Message != "" {
			return errors.New(state.Message)
		}
		return ErrNotJoined
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var ready bool
	if err := browser.Evaluate(ctx, `Boolean(window.__presentationTimerBridgeInstalled && window.__timerPlayWav)`, &ready); err != nil {
		c.failSession(runID, "Окно браузера ВКС закрыто. Подключитесь заново")
		return fmt.Errorf("не удалось проверить вкладку ВКС: %w", err)
	}
	if !ready {
		return errors.New("аудиомост таймера не найден на открытой странице; обновите страницу и повторите")
	}
	if !c.updateIfCurrent(runID, func(state *State) {
		state.Phase = PhaseJoined
		state.Message = "Подключение подтверждено; выполните тест звука"
		state.Tested = false
	}) {
		state := c.GetState()
		if state.Message != "" {
			return errors.New(state.Message)
		}
		return ErrNotJoined
	}
	if joinCancel != nil {
		joinCancel()
	}
	return nil
}

func (c *Controller) GetDiagnostics() (string, error) {
	c.mu.Lock()
	browser := c.browser
	c.mu.Unlock()
	if browser == nil {
		return "", ErrNotJoined
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var snapshot string
	if err := browser.Evaluate(ctx, diagnosticsScript, &snapshot); err != nil {
		return "", fmt.Errorf("не удалось прочитать диагностику ВКС: %w", err)
	}

	payload := map[string]any{
		"snapshot": json.RawMessage(snapshot),
	}
	if attacher, ok := browser.(interface{ Attachments() []attachedTarget }); ok {
		payload["attached"] = attacher.Attachments()
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return snapshot, nil
	}
	return string(data), nil
}

func (c *Controller) TestSound(wav []byte) error {
	return c.playWAV(wav, true)
}

func (c *Controller) PlaySound(wav []byte) error {
	return c.playWAV(wav, false)
}

func (c *Controller) playWAV(wav []byte, markTested bool) error {
	c.mu.Lock()
	browser := c.browser
	runID := c.runID
	c.mu.Unlock()
	state := c.GetState()
	if browser == nil || (state.Phase != PhaseJoined && state.Phase != PhasePlaying) {
		return ErrNotJoined
	}
	if len(wav) == 0 {
		return errors.New("пустой звуковой сигнал")
	}

	c.state.update(func(state *State) {
		state.Phase = PhasePlaying
		state.Message = "Отправка звука участникам"
	})

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	expression := fmt.Sprintf(
		`window.__timerLastAudioError = ''; Boolean(window.__timerPlayWav && window.__timerPlayWav(%q))`,
		base64.StdEncoding.EncodeToString(wav),
	)
	var accepted bool
	err := browser.Evaluate(ctx, expression, &accepted)
	if err != nil {
		c.failSession(runID, "Соединение с браузером ВКС потеряно. Подключитесь заново")
		return fmt.Errorf("ошибка передачи звука в ВКС: %w", err)
	}

	if !c.updateIfCurrent(runID, func(state *State) {
		state.Phase = PhaseJoined
		if !accepted {
			state.Message = "Не удалось передать звук"
			return
		}
		state.Tested = state.Tested || markTested
		if markTested {
			state.Message = "Тестовый звук отправлен"
		} else {
			state.Message = "Сигнал отправлен участникам"
		}
	}) {
		state := c.GetState()
		if state.Message != "" {
			return errors.New(state.Message)
		}
		return ErrNotJoined
	}

	if !accepted {
		return errors.New("страница ВКС не приняла звуковой сигнал")
	}
	return nil
}

func (c *Controller) IsReady() bool {
	state := c.GetState()
	return state.Phase == PhaseJoined && state.Tested
}

func (c *Controller) IsConnected() bool {
	state := c.GetState()
	return state.Phase == PhaseJoined || state.Phase == PhasePlaying
}

func (c *Controller) fail(err error) {
	c.state.update(func(state *State) {
		state.Phase = PhaseError
		state.Message = err.Error()
		state.Tested = false
	})
}

func (c *Controller) watchBrowser(ctx context.Context, runID uint64, done <-chan struct{}) {
	if done == nil {
		return
	}
	select {
	case <-ctx.Done():
		return
	case <-done:
		if ctx.Err() == nil {
			c.failSession(runID, "Окно браузера ВКС закрыто. Подключитесь заново")
		}
	}
}

func (c *Controller) failSession(runID uint64, message string) {
	c.mu.Lock()
	if c.runID != runID || c.cancel == nil {
		c.mu.Unlock()
		return
	}
	cancel := c.cancel
	joinCancel := c.joinCancel
	browserCancel := c.browserCancel
	c.runID++
	c.cancel = nil
	c.joinCancel = nil
	c.browserCancel = nil
	c.browser = nil
	c.mu.Unlock()

	if cancel != nil {
		cancel()
	}
	if joinCancel != nil {
		joinCancel()
	}
	if browserCancel != nil {
		browserCancel()
	}
	c.state.update(func(state *State) {
		state.Phase = PhaseError
		state.Message = message
		state.Tested = false
	})
}

func (c *Controller) isCurrentRun(runID uint64) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.runID == runID && c.cancel != nil
}

func (c *Controller) updateIfCurrent(runID uint64, change func(*State)) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.runID != runID || c.cancel == nil {
		return false
	}
	c.state.update(change)
	return true
}

func (c *Controller) clearSession(runID uint64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.runID != runID {
		return
	}
	c.cancel = nil
	c.joinCancel = nil
	c.browserCancel = nil
	c.browser = nil
}
