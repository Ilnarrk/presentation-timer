import { useCallback, useEffect, useMemo, useState } from 'react';
import './styles.css';
import {
  ConfirmConferenceJoined,
  ConnectConference,
  DismissAlert,
  DisconnectConference,
  GetAudioDevices,
  GetConferencePlatforms,
  GetConferenceState,
  GetSettings,
  GetSounds,
  GetState,
  GoToQuestions,
  NextSpeaker,
  Pause,
  PreviewSound,
  Reset,
  SaveSettings,
  Start,
  TestConferenceSound,
} from '../wailsjs/go/main/App';
import { EventsOn } from '../wailsjs/runtime/runtime';
import { settings, timer } from '../wailsjs/go/models';

type Phase = timer.Snapshot['phase'];

interface TimerSnapshot {
  phase: Phase;
  isRunning: boolean;
  isPaused: boolean;
  remainingSeconds: number;
  overtimeSeconds: number;
  talkSeconds: number;
  questionsSeconds: number;
  nextReminderIn: number;
  alertActive: boolean;
}

interface AudioDevice {
  id: string;
  name: string;
}

interface SoundOption {
  id: string;
  label: string;
}

interface ConferenceState {
  phase: 'idle' | 'opening' | 'connecting' | 'waitingAdmission' | 'joined' | 'playing' | 'left' | 'error';
  platform: string;
  displayUrl: string;
  message: string;
  tested: boolean;
  updatedAt: number;
}

interface ConferencePlatform {
  id: string;
  label: string;
}

const initialConferenceState: ConferenceState = {
  phase: 'idle',
  platform: '',
  displayUrl: '',
  message: 'Участник не подключён',
  tested: false,
  updatedAt: 0,
};

const conferencePhaseLabels: Record<ConferenceState['phase'], string> = {
  idle: 'Не подключён',
  opening: 'Открытие браузера',
  connecting: 'Подключение к встрече',
  waitingAdmission: 'Ожидает допуска',
  joined: 'Подключён',
  playing: 'Передаёт звук',
  left: 'Отключён',
  error: 'Ошибка',
};

const phaseLabels: Record<Phase, string> = {
  idle: 'Ожидание',
  talk: 'Доклад',
  talkOvertime: 'Доклад — просрочка',
  questions: 'Вопросы',
  questionsOvertime: 'Вопросы — просрочка',
  completed: 'Завершено',
};

function formatClock(totalSeconds: number): string {
  const seconds = Math.max(0, totalSeconds);
  const minutes = Math.floor(seconds / 60);
  const rest = seconds % 60;
  return `${String(minutes).padStart(2, '0')}:${String(rest).padStart(2, '0')}`;
}

function formatOvertime(totalSeconds: number): string {
  const minutes = Math.floor(totalSeconds / 60);
  const rest = totalSeconds % 60;
  return `+${String(minutes).padStart(2, '0')}:${String(rest).padStart(2, '0')}`;
}

function App() {
  const [snapshot, setSnapshot] = useState<TimerSnapshot>({
    phase: 'idle',
    isRunning: false,
    isPaused: false,
    remainingSeconds: 0,
    overtimeSeconds: 0,
    talkSeconds: 600,
    questionsSeconds: 300,
    nextReminderIn: 0,
    alertActive: false,
  });
  const [talkMinutes, setTalkMinutes] = useState(10);
  const [talkSecondsPart, setTalkSecondsPart] = useState(0);
  const [questionsMinutes, setQuestionsMinutes] = useState(5);
  const [questionsSecondsPart, setQuestionsSecondsPart] = useState(0);
  const [soundId, setSoundId] = useState('chime');
  const [deviceId, setDeviceId] = useState('default');
  const [volume, setVolume] = useState(0.85);
  const [devices, setDevices] = useState<AudioDevice[]>([]);
  const [sounds, setSounds] = useState<SoundOption[]>([]);
  const [conferenceUrl, setConferenceUrl] = useState('');
  const [conferenceName, setConferenceName] = useState('Таймер');
  const [conferenceState, setConferenceState] = useState<ConferenceState>(initialConferenceState);
  const [conferencePlatforms, setConferencePlatforms] = useState<ConferencePlatform[]>([]);
  const [conferenceBusy, setConferenceBusy] = useState(false);
  const [error, setError] = useState('');
  const [saving, setSaving] = useState(false);
  const [settingsOpen, setSettingsOpen] = useState(false);
  const [connectionPromptOpen, setConnectionPromptOpen] = useState(false);

  const settingsLocked = snapshot.isRunning;
  const conferenceActive = ['opening', 'connecting', 'waitingAdmission', 'joined', 'playing'].includes(conferenceState.phase);
  const conferenceJoined = conferenceState.phase === 'joined' || conferenceState.phase === 'playing';

  const persistSettings = useCallback(async (next?: Partial<settings.Settings>) => {
    const payload = settings.Settings.createFrom({
      talkMinutes: next?.talkMinutes ?? talkMinutes,
      talkSeconds: next?.talkSeconds ?? talkSecondsPart,
      questionsMinutes: next?.questionsMinutes ?? questionsMinutes,
      questionsSeconds: next?.questionsSeconds ?? questionsSecondsPart,
      soundId: next?.soundId ?? soundId,
      deviceId: next?.deviceId ?? deviceId,
      volume: next?.volume ?? volume,
    });

    setSaving(true);
    try {
      await SaveSettings(payload);
      setError('');
    } catch (err) {
      setError(String(err));
    } finally {
      setSaving(false);
    }
  }, [
    deviceId,
    questionsMinutes,
    questionsSecondsPart,
    soundId,
    talkMinutes,
    talkSecondsPart,
    volume,
  ]);

  useEffect(() => {
    const bootstrap = async () => {
      const [
        initialState,
        initialSettings,
        initialSounds,
        initialDevices,
        initialConference,
        supportedPlatforms,
      ] = await Promise.all([
        GetState(),
        GetSettings(),
        GetSounds(),
        GetAudioDevices(),
        GetConferenceState(),
        GetConferencePlatforms(),
      ]);

      setSnapshot(initialState as TimerSnapshot);
      setTalkMinutes(initialSettings.talkMinutes);
      setTalkSecondsPart(initialSettings.talkSeconds);
      setQuestionsMinutes(initialSettings.questionsMinutes);
      setQuestionsSecondsPart(initialSettings.questionsSeconds);
      setSoundId(initialSettings.soundId);
      setDeviceId(initialSettings.deviceId);
      setVolume(initialSettings.volume);
      setSounds(initialSounds as SoundOption[]);
      setDevices(initialDevices as AudioDevice[]);
      setConferenceState(initialConference as ConferenceState);
      setConferencePlatforms(supportedPlatforms as ConferencePlatform[]);
      if (!['opening', 'connecting', 'waitingAdmission', 'joined', 'playing'].includes(initialConference.phase)) {
        setConnectionPromptOpen(true);
      }
    };

    bootstrap().catch((err) => setError(String(err)));
  }, []);

  useEffect(() => {
    const unsubscribe = EventsOn('timer:state', (state: TimerSnapshot) => {
      setSnapshot(state);
    });
    return () => unsubscribe();
  }, []);

  useEffect(() => {
    const unsubscribe = EventsOn('conference:state', (state: ConferenceState) => {
      setConferenceState(state);
      if (state.phase === 'error') setError(state.message);
    });
    return () => unsubscribe();
  }, []);

  const displayTime = useMemo(() => {
    if (snapshot.phase === 'talkOvertime' || snapshot.phase === 'questionsOvertime') {
      return formatOvertime(snapshot.overtimeSeconds);
    }
    if (snapshot.phase === 'idle') {
      return formatClock(talkMinutes * 60 + talkSecondsPart);
    }
    return formatClock(snapshot.remainingSeconds);
  }, [snapshot, talkMinutes, talkSecondsPart]);

  const statusClass = useMemo(() => {
    if (snapshot.alertActive) return 'status-alert';
    if (snapshot.phase.includes('Overtime')) return 'status-overtime';
    if (snapshot.isPaused) return 'status-paused';
    if (snapshot.isRunning) return 'status-running';
    return 'status-idle';
  }, [snapshot]);

  const handleStart = async () => {
    try {
      await persistSettings();
      await Start();
      setError('');
    } catch (err) {
      setError(String(err));
    }
  };

  const handleReset = () => {
    Reset();
    setError('');
  };

  const handleGoToQuestions = async () => {
    try {
      await GoToQuestions();
      setError('');
    } catch (err) {
      setError(String(err));
    }
  };

  const handleNextSpeaker = async () => {
    try {
      await NextSpeaker();
      setError('');
    } catch (err) {
      setError(String(err));
    }
  };

  const handleDismissAlert = () => {
    DismissAlert();
  };

  const handlePreview = async () => {
    try {
      await persistSettings();
      await PreviewSound(soundId);
    } catch (err) {
      setError(String(err));
    }
  };

  const handleConferenceConnect = async () => {
    if (!conferenceUrl.trim()) {
      setError('Укажите HTTPS-ссылку на встречу');
      return;
    }
    setConferenceBusy(true);
    try {
      const state = await ConnectConference(conferenceUrl.trim(), conferenceName.trim());
      setConferenceState(state as ConferenceState);
      setConnectionPromptOpen(false);
      setError('');
    } catch (err) {
      setError(String(err));
    } finally {
      setConferenceBusy(false);
    }
  };

  const handleConferenceDisconnect = async () => {
    setConferenceBusy(true);
    try {
      await DisconnectConference();
      const state = await GetConferenceState();
      setConferenceState(state as ConferenceState);
      setError('');
    } catch (err) {
      setError(String(err));
    } finally {
      setConferenceBusy(false);
    }
  };

  const handleConferenceConfirm = async () => {
    setConferenceBusy(true);
    try {
      await ConfirmConferenceJoined();
      const state = await GetConferenceState();
      setConferenceState(state as ConferenceState);
      setError('');
    } catch (err) {
      setError(String(err));
    } finally {
      setConferenceBusy(false);
    }
  };

  const handleConferenceTest = async () => {
    setConferenceBusy(true);
    try {
      await persistSettings();
      await TestConferenceSound(soundId);
      setError('');
    } catch (err) {
      setError(String(err));
    } finally {
      setConferenceBusy(false);
    }
  };

  const phaseDuration = snapshot.phase.startsWith('questions')
    ? questionsMinutes * 60 + questionsSecondsPart
    : talkMinutes * 60 + talkSecondsPart;
  const progress = snapshot.phase.includes('Overtime')
    ? 1
    : snapshot.phase === 'idle'
      ? 0
      : Math.min(1, Math.max(0, 1 - snapshot.remainingSeconds / Math.max(1, phaseDuration)));
  const ringLength = 854.5;

  const icon = (name: 'play' | 'pause' | 'questions' | 'next' | 'settings' | 'close') => {
    const paths = {
      play: <path d="M9 6.8v10.4c0 .8.9 1.3 1.6.8l8.2-5.2a.95.95 0 0 0 0-1.6L10.6 6c-.7-.5-1.6 0-1.6.8Z" />,
      pause: <><path d="M8 6.5h3v11H8z" /><path d="M14 6.5h3v11h-3z" /></>,
      questions: <><path d="M9.5 9a3 3 0 1 1 4.1 2.8c-1 .4-1.6 1-1.6 2" /><path d="M12 17.5h.01" /></>,
      next: <><path d="m7 6 7 6-7 6V6Z" /><path d="M16 6v12" /></>,
      settings: <><circle cx="12" cy="12" r="3" /><path d="M19.4 15a1.7 1.7 0 0 0 .3 1.9l.1.1-2.8 2.8-.1-.1a1.7 1.7 0 0 0-1.9-.3 1.7 1.7 0 0 0-1 1.6v.2h-4V21a1.7 1.7 0 0 0-1-1.6 1.7 1.7 0 0 0-1.9.3l-.1.1L4.2 17l.1-.1a1.7 1.7 0 0 0 .3-1.9A1.7 1.7 0 0 0 3 14H2.8v-4H3a1.7 1.7 0 0 0 1.6-1 1.7 1.7 0 0 0-.3-1.9L4.2 7 7 4.2l.1.1a1.7 1.7 0 0 0 1.9.3A1.7 1.7 0 0 0 10 3V2.8h4V3a1.7 1.7 0 0 0 1 1.6 1.7 1.7 0 0 0 1.9-.3l.1-.1L19.8 7l-.1.1a1.7 1.7 0 0 0-.3 1.9 1.7 1.7 0 0 0 1.6 1h.2v4H21a1.7 1.7 0 0 0-1.6 1Z" /></>,
      close: <><path d="m7 7 10 10" /><path d="M17 7 7 17" /></>,
    };
    return <svg viewBox="0 0 24 24" aria-hidden="true">{paths[name]}</svg>;
  };

  const connectionForm = (
    <>
      <label>
        Ссылка на встречу
        <input
          type="url"
          placeholder="https://..."
          value={conferenceUrl}
          disabled={conferenceActive}
          onChange={(event) => setConferenceUrl(event.target.value)}
          autoFocus
        />
      </label>
      <label>
        Имя участника
        <input
          type="text"
          maxLength={80}
          value={conferenceName}
          disabled={conferenceActive}
          onChange={(event) => setConferenceName(event.target.value)}
        />
      </label>
    </>
  );

  return (
    <div className="app-shell">
      <header className="topbar">
        <div className="brand-mark">T</div>
        <button className="icon-button quiet" aria-label="Открыть настройки" title="Настройки" onClick={() => setSettingsOpen(true)}>
          {icon('settings')}
        </button>
      </header>

      <main className="timer-stage">
        <button
          className={`conference-badge conference-${conferenceState.phase}`}
          onClick={() => setConnectionPromptOpen(true)}
          title="Настроить подключение к ВКС"
        >
          <span className="connection-dot" />
          <span>{conferencePhaseLabels[conferenceState.phase]}</span>
        </button>

        <section className={`timer-ring ${statusClass}`}>
          <svg className="progress-ring" viewBox="0 0 320 320" aria-hidden="true">
            <circle className="ring-track" cx="160" cy="160" r="136" />
            <circle
              className="ring-progress"
              cx="160"
              cy="160"
              r="136"
              strokeDasharray={ringLength}
              strokeDashoffset={ringLength * (1 - progress)}
            />
          </svg>
          <div className="timer-content">
            <span className="phase-name">{phaseLabels[snapshot.phase]}</span>
            <span className="timer-value">{displayTime}</span>
            <span className="timer-units">мин&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;сек</span>
            {snapshot.phase.includes('Overtime') && (
              <span className="timer-caption">Сигнал через {formatClock(snapshot.nextReminderIn)}</span>
            )}
          </div>
        </section>

        <nav className="controls" aria-label="Управление таймером">
          <button className="icon-button primary" onClick={handleStart} disabled={snapshot.isRunning && !snapshot.isPaused} aria-label="Запустить" title="Запустить">
            {icon('play')}
          </button>
          <button className="icon-button" onClick={() => Pause()} disabled={!snapshot.isRunning || snapshot.isPaused} aria-label="Пауза" title="Пауза">
            {icon('pause')}
          </button>
          <button className="icon-button" onClick={handleGoToQuestions} disabled={snapshot.phase !== 'talk' && snapshot.phase !== 'talkOvertime'} aria-label="Перейти к вопросам" title="К вопросам">
            {icon('questions')}
          </button>
          <button className="icon-button" onClick={handleNextSpeaker} disabled={snapshot.phase !== 'questions' && snapshot.phase !== 'questionsOvertime'} aria-label="Следующий докладчик" title="Следующий докладчик">
            {icon('next')}
          </button>
        </nav>

        {snapshot.alertActive && (
          <button className="alert-chip" onClick={handleDismissAlert}>
            Время вышло · закрыть уведомление
          </button>
        )}
        {error && <div className="error-toast">{error}</div>}
      </main>

      {connectionPromptOpen && (
        <div className="modal-backdrop" role="presentation">
          <section className="modal connection-modal" role="dialog" aria-modal="true" aria-labelledby="connection-title">
            <button className="icon-button quiet close-button" aria-label="Закрыть" onClick={() => setConnectionPromptOpen(false)}>
              {icon('close')}
            </button>
            <span className="modal-kicker">ВКС</span>
            <h2 id="connection-title">{conferenceActive ? 'Участник подключается' : 'Подключиться к встрече?'}</h2>
            <p className="modal-copy">{conferenceState.message}</p>
            {!conferenceActive && connectionForm}
            <div className="modal-actions">
              {!conferenceActive ? (
                <>
                  <button className="text-button secondary" onClick={() => setConnectionPromptOpen(false)}>Пропустить</button>
                  <button className="text-button primary" disabled={conferenceBusy} onClick={handleConferenceConnect}>Подключиться</button>
                </>
              ) : (
                <>
                  {(conferenceState.phase === 'connecting' || conferenceState.phase === 'waitingAdmission') && (
                    <button className="text-button secondary" disabled={conferenceBusy} onClick={handleConferenceConfirm}>Я уже подключён</button>
                  )}
                  <button className="text-button danger" disabled={conferenceBusy} onClick={handleConferenceDisconnect}>Отключить</button>
                </>
              )}
            </div>
          </section>
        </div>
      )}

      {settingsOpen && (
        <div className="modal-backdrop drawer-backdrop" onMouseDown={(event) => event.target === event.currentTarget && setSettingsOpen(false)}>
          <aside className="settings-drawer" role="dialog" aria-modal="true" aria-labelledby="settings-title">
            <div className="drawer-header">
              <div><span className="modal-kicker">Параметры</span><h2 id="settings-title">Настройки</h2></div>
              <button className="icon-button quiet" aria-label="Закрыть настройки" onClick={() => setSettingsOpen(false)}>{icon('close')}</button>
            </div>

            <div className="settings-section">
              <h3>Длительность</h3>
              <label>Доклад<div className="duration-inputs">
                <input type="number" min={0} max={180} value={talkMinutes} disabled={settingsLocked} onChange={(e) => setTalkMinutes(Number(e.target.value))} onBlur={() => persistSettings()} /><span>мин</span>
                <input type="number" min={0} max={59} value={talkSecondsPart} disabled={settingsLocked} onChange={(e) => setTalkSecondsPart(Number(e.target.value))} onBlur={() => persistSettings()} /><span>сек</span>
              </div></label>
              <label>Вопросы<div className="duration-inputs">
                <input type="number" min={0} max={60} value={questionsMinutes} disabled={settingsLocked} onChange={(e) => setQuestionsMinutes(Number(e.target.value))} onBlur={() => persistSettings()} /><span>мин</span>
                <input type="number" min={0} max={59} value={questionsSecondsPart} disabled={settingsLocked} onChange={(e) => setQuestionsSecondsPart(Number(e.target.value))} onBlur={() => persistSettings()} /><span>сек</span>
              </div></label>
              <button className="text-button secondary full-width" onClick={handleReset}>Сбросить таймер</button>
            </div>

            <div className="settings-section">
              <h3>Звук</h3>
              <label>Сигнал<select value={soundId} disabled={settingsLocked} onChange={async (e) => { const next = e.target.value; setSoundId(next); await persistSettings({ soundId: next }); }}>
                {sounds.map((sound) => <option key={sound.id} value={sound.id}>{sound.label}</option>)}
              </select></label>
              <label>Громкость<input type="range" min={0} max={1} step={0.05} value={volume} disabled={settingsLocked} onChange={(e) => setVolume(Number(e.target.value))} onMouseUp={() => persistSettings()} onTouchEnd={() => persistSettings()} /></label>
              <label>Устройство<select value={deviceId} disabled={settingsLocked} onChange={async (e) => { const next = e.target.value; setDeviceId(next); await persistSettings({ deviceId: next }); }}>
                {devices.map((device) => <option key={device.id} value={device.id}>{device.name}</option>)}
              </select></label>
              <button className="text-button secondary full-width" onClick={handlePreview}>Прослушать сигнал</button>
            </div>

            <div className="settings-section">
              <div className="section-heading"><h3>Подключение к ВКС</h3><span className={`mini-status conference-${conferenceState.phase}`}>{conferencePhaseLabels[conferenceState.phase]}</span></div>
              {!conferenceActive && connectionForm}
              <div className="stack-actions">
                {!conferenceActive
                  ? <button className="text-button primary full-width" disabled={conferenceBusy} onClick={handleConferenceConnect}>Подключиться</button>
                  : <button className="text-button danger full-width" disabled={conferenceBusy} onClick={handleConferenceDisconnect}>Отключить</button>}
                <button className="text-button secondary full-width" disabled={!conferenceJoined || conferenceBusy} onClick={handleConferenceTest}>Проверить звук в ВКС</button>
              </div>
              <p className="settings-hint">Поддерживаются {conferencePlatforms.map((platform) => platform.label).join(', ')}.</p>
            </div>
            <p className="saving-note">{saving ? 'Сохранение…' : 'Изменения сохраняются автоматически'}</p>
          </aside>
        </div>
      )}
    </div>
  );
}

export default App;
