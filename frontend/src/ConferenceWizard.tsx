import { useState, type ReactNode } from 'react';
import {
  conferenceUiState,
  isConferenceConnecting,
  isConferenceJoined,
  type ConferenceAction,
  type ConferenceState,
  type ConferenceWizardStep,
  type RecentConference,
} from './conference';

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
  action: ConferenceAction;
  diagnostics?: boolean;
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
  onBackToCheck: () => void;
  onDone: () => void;
  onDiagnostics?: () => void;
}

function StepIndicator({ step }: { step: ConferenceWizardStep }) {
  return (
    <div className="wizard-progress" aria-label={`Шаг ${step} из 3`}>
      <div className="wizard-progress-meta">Шаг {step} из 3</div>
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

function LinkIcon() {
  return (
    <svg viewBox="0 0 24 24" aria-hidden="true">
      <path d="M10.6 13.4a4 4 0 0 0 5.7 0l2.1-2.1a4 4 0 1 0-5.7-5.7l-1.2 1.2m1.9 3.8a4 4 0 0 0-5.7 0l-2.1 2.1a4 4 0 1 0 5.7 5.7l1.2-1.2" />
    </svg>
  );
}

type CheckState = 'pending' | 'loading' | 'success' | 'error';

function CheckItem({
  state,
  title,
  description,
  children,
}: {
  state: CheckState;
  title: string;
  description: string;
  children?: ReactNode;
}) {
  return (
    <li className={`wizard-check-item is-${state}`}>
      <span className="wizard-check-marker" aria-hidden="true">
        {state === 'success' ? '✓' : state === 'error' ? '!' : ''}
      </span>
      <div className="wizard-check-copy">
        <strong>{title}</strong>
        <span>{description}</span>
        {children}
      </div>
    </li>
  );
}

function AudioWave({ active }: { active: boolean }) {
  return (
    <div className={`wizard-wave${active ? ' is-active' : ''}`} aria-hidden="true">
      <svg className="wizard-speaker" viewBox="0 0 24 24">
        <path d="M5 9v6h4l5 4V5L9 9H5Zm12.5-.5a5 5 0 0 1 0 7" />
      </svg>
      <span /><span /><span /><span /><span /><span /><span />
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
  action,
  diagnostics,
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
  onBackToCheck,
  onDone,
  onDiagnostics,
}: ConferenceWizardProps) {
  const [recentOpen, setRecentOpen] = useState(false);
  const connecting = isConferenceConnecting(state.phase);
  const joined = isConferenceJoined(state.phase);
  const uiState = conferenceUiState(state, action, testing);
  const errorText = error || (state.phase === 'error' ? state.message : '');
  const effectiveStep: ConferenceWizardStep = joined
    ? step
    : (connecting || state.phase === 'error' ? 2 : 1);
  const validationError = effectiveStep === 1 ? errorText : '';
  const connectionError = effectiveStep === 2 && !joined ? errorText : '';
  const audioError = effectiveStep === 2 && joined ? errorText : '';

  const connectionCheck: CheckState = state.phase === 'error'
    ? 'error'
    : joined
      ? 'success'
      : connecting
        ? 'loading'
        : 'pending';
  const audioCheck: CheckState = audioError
    ? 'error'
    : testing
      ? 'loading'
      : state.tested
        ? 'success'
        : 'pending';

  return (
    <div className="modal-backdrop" role="presentation">
      <section className="modal connection-modal wizard-modal" role="dialog" aria-modal="true" aria-labelledby="connection-title">
        <header className="wizard-card-header">
          <StepIndicator step={effectiveStep} />
          <div className="wizard-header">
            <h2 id="connection-title">
              {effectiveStep === 1 ? 'Подключение к ВКС' : effectiveStep === 2 ? 'Проверка подключения' : 'ВКС подключена'}
            </h2>
            <p className="modal-copy">
              {effectiveStep === 1
                ? 'Укажите ссылку на встречу и ваше имя, чтобы таймер мог присоединиться к конференции.'
                : effectiveStep === 2
                  ? 'Подключаемся к встрече и проверяем звук. Это займёт несколько секунд.'
                  : 'Таймер успешно присоединился к встрече и готов к работе.'}
            </p>
          </div>
        </header>

        <div className="wizard-body">
          {effectiveStep === 1 && (
            <div className="wizard-form">
              <label>
                Ссылка на встречу
                <span className={`wizard-url-input${validationError ? ' has-error' : ''}`}>
                  <span className="wizard-input-icon"><LinkIcon /></span>
                  <input
                    type="url"
                    placeholder="https://..."
                    value={conferenceUrl}
                    aria-invalid={Boolean(validationError)}
                    aria-describedby={validationError ? 'conference-url-error' : 'conference-url-help'}
                    onChange={(event) => onUrlChange(event.target.value)}
                    autoFocus
                  />
                </span>
                {validationError ? (
                  <span className="wizard-field-error" id="conference-url-error" role="alert">
                    Проверьте ссылку и попробуйте снова
                  </span>
                ) : (
                  <span className="wizard-field-help" id="conference-url-help">
                    SaluteJazz, Яндекс Телемост, Контур.Толк, МТС Линк, MINT и корпоративные ВКС
                  </span>
                )}
              </label>
              {recent.length > 0 && (
                <details className="wizard-recent" open={recentOpen} onToggle={(event) => setRecentOpen(event.currentTarget.open)}>
                  <summary>Недавние встречи <span aria-hidden="true">⌄</span></summary>
                  <ul>
                    {recent.map((item) => (
                      <li key={item.url}>
                        <button
                          type="button"
                          className="wizard-recent-item"
                          title={item.url}
                          onClick={() => {
                            onSelectRecent(item.url);
                            setRecentOpen(false);
                          }}
                        >
                          <span className="wizard-recent-title">{item.title}</span>
                          <span className="wizard-recent-url">{item.host}</span>
                        </button>
                      </li>
                    ))}
                  </ul>
                </details>
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
                <span className="wizard-help" title="Таймер будет передавать изображение отсчёта как виртуальную камеру" aria-label="Подробнее об отсчёте в камере">?</span>
              </label>
            </div>
          )}

          {effectiveStep === 2 && (
            <div className="wizard-check">
              <ol className="wizard-check-list">
                <CheckItem
                  state={connectionCheck}
                  title="Подключение к ВКС"
                  description={connectionError || (joined ? 'Соединение установлено' : state.message || 'Открываем встречу')}
                >
                  {uiState === 'waiting_for_user' && (
                    <div className="wizard-browser-action">
                      <strong>Требуется действие в браузере</strong>
                      <span>Завершите вход или дождитесь допуска организатора, затем подтвердите подключение.</span>
                      <button className="wizard-inline-action" type="button" disabled={busy} onClick={onManualConfirm}>
                        Я подключился
                      </button>
                    </div>
                  )}
                  {uiState === 'connecting' && (
                    <button className="wizard-inline-action" type="button" disabled={busy} onClick={onManualConfirm}>
                      Уже вошли? Подтвердить
                    </button>
                  )}
                </CheckItem>
                <CheckItem
                  state={joined ? audioCheck : 'pending'}
                  title="Проверка звука"
                  description={audioError || (testing
                    ? 'Воспроизводим тестовый сигнал...'
                    : state.tested
                      ? 'Тестовый сигнал отправлен в конференцию'
                      : 'Нужно вручную убедиться, что сигнал слышно')}
                >
                  {joined && <AudioWave active={testing} />}
                </CheckItem>
                <CheckItem
                  state={state.tested ? 'success' : 'pending'}
                  title="Готово"
                  description={state.tested ? 'Таймер готов к работе' : 'Проверяем, что таймер слышно'}
                />
              </ol>
              {connectionError && (
                <div className="wizard-error-card" role="alert">
                  <strong>Не удалось подключиться к встрече</strong>
                  <span>{connectionError}</span>
                </div>
              )}
              {diagnostics && onDiagnostics && (
                <button className="wizard-diagnostics-link" type="button" disabled={busy} onClick={onDiagnostics}>
                  Скопировать диагностику
                </button>
              )}
            </div>
          )}

          {joined && effectiveStep === 3 && (
            <div className="wizard-summary">
              <div className="wizard-success-icon" aria-hidden="true">✓</div>
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
                    <dt>Встреча</dt>
                    <dd className="wizard-url" title={state.displayUrl}>{state.displayUrl}</dd>
                  </div>
                )}
                <div>
                  <dt>Отсчёт в камере</dt>
                  <dd>{cameraEnabled ? '✓ Включён' : 'Выключен'}</dd>
                </div>
                <div>
                  <dt>Звук</dt>
                  <dd>{state.tested ? 'Проверен' : 'Не проверен'}</dd>
                </div>
              </dl>
              <button className="wizard-disconnect" disabled={busy} onClick={onDisconnect}>
                {uiState === 'disconnecting' ? 'Отключаем…' : 'Отключиться от ВКС'}
              </button>
            </div>
          )}
        </div>

        <footer className="wizard-footer">
          {effectiveStep === 1 ? (
            <>
              <button className="text-button secondary" disabled={busy} onClick={onSkip}>Пропустить</button>
              <button className="text-button primary" disabled={busy} onClick={onConnect}>
                {uiState === 'validating' ? 'Проверяем…' : 'Далее →'}
              </button>
            </>
          ) : effectiveStep === 2 ? (
            <>
              <button className="text-button secondary" disabled={busy} onClick={connectionError ? onEditDetails : onCancelConnect}>← Назад</button>
              {connectionError ? (
                <button className="text-button primary" disabled={busy} onClick={onRetry}>Повторить</button>
              ) : !joined ? (
                <button className="text-button primary" disabled>Проверяем...</button>
              ) : state.tested && !testing ? (
                <button className="text-button primary" disabled={busy} onClick={onNext}>Далее →</button>
              ) : (
                <button className="text-button primary" disabled={busy || testing} onClick={onTestSound}>
                  {testing ? 'Проверяем...' : 'Проверить звук'}
                </button>
              )}
            </>
          ) : (
            <>
              <button className="text-button secondary" disabled={busy} onClick={onBackToCheck}>← Назад</button>
              <button className="text-button primary" disabled={busy} onClick={onDone}>✓ Готово</button>
            </>
          )}
        </footer>
      </section>
    </div>
  );
}
