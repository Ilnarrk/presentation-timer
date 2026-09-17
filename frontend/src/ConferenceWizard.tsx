import type { ReactNode } from 'react';
import {
  conferencePhaseLabels,
  isConferenceConnecting,
  isConferenceJoined,
  type ConferenceState,
  type ConferenceWizardStep,
  type RecentConference,
} from './conference';

const STEP_TITLES = ['Подключение', 'Проверка', 'Состояние'] as const;

interface ConferenceWizardProps {
  step: ConferenceWizardStep;
  state: ConferenceState;
  conferenceUrl: string;
  conferenceName: string;
  cameraEnabled: boolean;
  busy: boolean;
  error: string;
  recent: RecentConference[];
  testing: boolean;
  diagnostics?: boolean;
  setupEditing?: boolean;
  onUrlChange: (value: string) => void;
  onNameChange: (value: string) => void;
  onSelectRecent: (url: string) => void;
  onCameraToggle: (enabled: boolean) => void;
  onConnect: () => void;
  onSkip: () => void;
  onCancelConnect: () => void;
  onManualConfirm: () => void;
  onRetry: () => void;
  onEditDetails: () => void;
  onTestSound: () => void;
  onDisconnect: () => void;
  onNext: () => void;
  onDone: () => void;
  onDiagnostics?: () => void;
}

function StepIndicator({ step }: { step: ConferenceWizardStep }) {
  return (
    <div className="wizard-progress" aria-label={`Шаг ${step} из 3, ${STEP_TITLES[step - 1]}`}>
      <div className="wizard-progress-meta">
        <span>Шаг {step} из 3</span>
        <span className="wizard-progress-name">{STEP_TITLES[step - 1]}</span>
      </div>
      <div className="wizard-progress-dots" aria-hidden="true">
        {[1, 2, 3].map((value) => (
          <span
            key={value}
            className={`wizard-dot${value === step ? ' is-current' : ''}${value < step ? ' is-done' : ''}`}
          />
        ))}
      </div>
    </div>
  );
}

function StatusLine({ tone, children }: { tone: 'wait' | 'ok' | 'error' | 'muted'; children: ReactNode }) {
  return (
    <div className={`wizard-status wizard-status-${tone}`}>
      <span className="connection-dot" />
      <span>{children}</span>
    </div>
  );
}

export function ConferenceWizard({
  step,
  state,
  conferenceUrl,
  conferenceName,
  cameraEnabled,
  busy,
  error,
  recent,
  testing,
  diagnostics,
  setupEditing,
  onUrlChange,
  onNameChange,
  onSelectRecent,
  onCameraToggle,
  onConnect,
  onSkip,
  onCancelConnect,
  onManualConfirm,
  onRetry,
  onEditDetails,
  onTestSound,
  onDisconnect,
  onNext,
  onDone,
  onDiagnostics,
}: ConferenceWizardProps) {
  const connecting = isConferenceConnecting(state.phase);
  const joined = isConferenceJoined(state.phase);
  const hasError = state.phase === 'error' || Boolean(error);
  const errorText = error || (state.phase === 'error' ? state.message : '');
  const showErrorBanner = Boolean(errorText);
  const errorBlocking = hasError && !connecting && !joined && !setupEditing;

  let title = 'Подключение к ВКС';
  let subtitle = 'Укажите ссылку на встречу и имя участника';
  if (errorBlocking) {
    title = 'Не удалось подключиться';
    subtitle = 'Проверьте ссылку и имя или повторите попытку. Введённые данные сохранены.';
  } else if (connecting) {
    title = 'Подключаемся к ВКС';
    subtitle = 'Встреча открыта. Если потребуется, завершите вход или дождитесь допуска организатора.';
  } else if (step === 2 && joined) {
    title = 'Проверьте звук';
    subtitle = 'Подключение установлено. Воспроизведите тестовый сигнал и убедитесь, что его слышно в конференции.';
  } else if (step === 3 && joined) {
    title = 'ВКС подключена';
    subtitle = 'Соединение активно. Можно вернуться к таймеру — ВКС останется подключённой.';
  }

  return (
    <div className="modal-backdrop" role="presentation">
      <section className="modal connection-modal wizard-modal" role="dialog" aria-modal="true" aria-labelledby="connection-title">
        <StepIndicator step={connecting || (!joined && step === 1) ? 1 : step} />
        <header className="wizard-header">
          <h2 id="connection-title">{title}</h2>
          <p className="modal-copy">{subtitle}</p>
        </header>

        <div className="wizard-body">
          {showErrorBanner && <div className="panel-error" role="alert">{errorText}</div>}

          {step === 1 && !connecting && !joined && !errorBlocking && (
            <div className="wizard-form">
              <label>
                Ссылка на встречу
                <input
                  type="url"
                  placeholder="https://..."
                  value={conferenceUrl}
                  onChange={(event) => onUrlChange(event.target.value)}
                  autoFocus
                />
              </label>
              {recent.length > 0 && (
                <div className="wizard-recent">
                  <span className="wizard-recent-label">Последние подключения</span>
                  <ul>
                    {recent.map((item) => (
                      <li key={item.url}>
                        <button type="button" className="wizard-recent-item" onClick={() => onSelectRecent(item.url)}>
                          <span className="wizard-recent-title">{item.title}</span>
                          <span className="wizard-recent-url">{item.url}</span>
                        </button>
                      </li>
                    ))}
                  </ul>
                </div>
              )}
              <label>
                Имя участника
                <input
                  type="text"
                  maxLength={80}
                  value={conferenceName}
                  onChange={(event) => onNameChange(event.target.value)}
                />
              </label>
              <label className="settings-checkbox conference-camera-toggle">
                <input
                  type="checkbox"
                  checked={cameraEnabled}
                  disabled={busy}
                  onChange={(event) => onCameraToggle(event.target.checked)}
                />
                <span>Показывать отсчёт в камере</span>
              </label>
            </div>
          )}

          {connecting && (
            <div className="wizard-connecting">
              <StatusLine tone="wait">Ожидаем подключения...</StatusLine>
              <p className="wizard-hint">{state.message || conferencePhaseLabels[state.phase]}</p>
            </div>
          )}

          {joined && step === 2 && (
            <div className="wizard-check">
              <StatusLine tone="ok">ВКС подключена</StatusLine>
              <div className="wizard-test-block">
                <button
                  className="text-button secondary"
                  disabled={busy}
                  onClick={onTestSound}
                >
                  {testing ? 'Воспроизводится…' : state.tested ? 'Повторить тест' : '▶ Проверить звук'}
                </button>
                {testing ? (
                  <p className="wizard-hint">Идёт воспроизведение тестового сигнала.</p>
                ) : state.tested ? (
                  <p className="wizard-success">✓ Тестовый сигнал воспроизведён</p>
                ) : (
                  <p className="wizard-hint">Рекомендуется, но можно продолжить без теста.</p>
                )}
              </div>
            </div>
          )}

          {joined && step === 3 && (
            <div className="wizard-summary">
              <StatusLine tone="ok">Подключение активно</StatusLine>
              <dl className="wizard-facts">
                <div>
                  <dt>Участник</dt>
                  <dd>{conferenceName.trim() || 'Таймер'}</dd>
                </div>
                <div>
                  <dt>Платформа</dt>
                  <dd>{state.platform || 'ВКС'}</dd>
                </div>
                {state.displayUrl && (
                  <div>
                    <dt>Адрес</dt>
                    <dd className="wizard-url">{state.displayUrl}</dd>
                  </div>
                )}
                <div>
                  <dt>Отсчёт в камере</dt>
                  <dd>{cameraEnabled ? '✓ Включён' : 'Выключен'}</dd>
                </div>
                <div>
                  <dt>Проверка звука</dt>
                  <dd>{state.tested ? '✓ Выполнена' : 'Не выполнялась'}</dd>
                </div>
              </dl>
              <button className="text-button secondary compact-button" disabled={busy} onClick={onTestSound}>
                {testing ? 'Воспроизводится…' : '▶ Проверить звук'}
              </button>
              {state.tested && !testing && (
                <p className="wizard-success">✓ Тестовый сигнал воспроизведён</p>
              )}
            </div>
          )}
        </div>

        <footer className="wizard-footer">
          {errorBlocking ? (
            <>
              <button className="text-button secondary" disabled={busy} onClick={onEditDetails}>Изменить данные</button>
              <button className="text-button primary" disabled={busy} onClick={onRetry}>Повторить</button>
            </>
          ) : connecting ? (
            <>
              <button className="text-button danger" disabled={busy} onClick={onCancelConnect}>Отменить подключение</button>
              <button className="text-button primary" disabled={busy} onClick={onManualConfirm}>Я подключился</button>
            </>
          ) : joined && step === 2 ? (
            <>
              <button className="text-button danger" disabled={busy} onClick={onDisconnect}>Отключиться от ВКС</button>
              <button className="text-button primary" disabled={busy} onClick={onNext}>Далее</button>
            </>
          ) : joined && step === 3 ? (
            <>
              <button className="text-button danger" disabled={busy} onClick={onDisconnect}>Отключиться от ВКС</button>
              <button className="text-button primary" onClick={onDone}>Готово</button>
            </>
          ) : (
            <>
              <button className="text-button secondary" onClick={onSkip}>Пропустить</button>
              <button className="text-button primary" disabled={busy} onClick={onConnect}>Подключиться</button>
            </>
          )}
          {diagnostics && onDiagnostics && (
            <button className="text-button secondary compact-button wizard-diagnostics" disabled={busy} onClick={onDiagnostics}>
              Диагностика
            </button>
          )}
        </footer>
      </section>
    </div>
  );
}
