import { useCallback, useEffect, useMemo, useRef, useState, type MouseEvent } from 'react';
import './styles.css';
import {
  ConfirmConferenceJoined,
  ConnectConference,
  CreateSession,
  EndSession,
  DismissAlert,
  DeleteSessionTemplate,
  DisconnectConference,
  GetAppInfo,
  GetAudioDevices,
  GetConferenceDiagnostics,
  GetConferenceState,
  GetSessionState,
  GetSessionTemplate,
  GetSettings,
  GetSounds,
  GetState,
  GoToQuestions,
  ImportSound,
  ListSessionTemplates,
  NextSpeaker,
  Pause,
  PreviewSound,
  Reset,
  ResetSession,
  SaveSessionTemplate,
  SaveSettings,
  SetConferenceBrowserVisible,
  Start,
  TestConferenceSound,
} from '../wailsjs/go/main/App';
import { EventsOn, BrowserOpenURL, ClipboardSetText } from '../wailsjs/runtime/runtime';
import { buildinfo, session, settings, templates, timer } from '../wailsjs/go/models';

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

interface SessionSpeaker {
  index: number;
  name: string;
  talkSeconds: number;
  questionsSeconds: number;
  status: 'pending' | 'active' | 'done';
}

interface SessionState {
  active: boolean;
  totalBudgetSeconds: number;
  usedSeconds: number;
  remainingSeconds: number;
  currentIndex: number;
  speakers: SessionSpeaker[];
}

interface AppInfo {
  name: string;
  version: string;
  url: string;
  urlLabel: string;
}

interface AudioDevice {
  id: string;
  name: string;
}

interface SoundOption {
  id: string;
  label: string;
  source?: string;
}

interface ConferenceState {
  phase: 'idle' | 'opening' | 'connecting' | 'waitingAdmission' | 'joined' | 'playing' | 'left' | 'error';
  platform: string;
  displayUrl: string;
  message: string;
  tested: boolean;
  browserVisible: boolean;
  updatedAt: number;
}

const initialConferenceState: ConferenceState = {
  phase: 'idle',
  platform: '',
  displayUrl: '',
  message: 'Участник не подключён',
  tested: false,
  browserVisible: false,
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

const MAX_SPEAKERS = 50;

const initialSessionState: SessionState = {
  active: false,
  totalBudgetSeconds: 0,
  usedSeconds: 0,
  remainingSeconds: 0,
  currentIndex: 0,
  speakers: [],
};

function padSpeakerNames(names: string[] | undefined, count: number): string[] {
  return Array.from({ length: Math.max(0, count) }, (_, index) => names?.[index] ?? '');
}

function sessionBudgetSeconds(totalMinutes: number, totalSeconds: number): number {
  return totalMinutes * 60 + totalSeconds;
}

function sessionBudgetFromHoursMinutes(hours: number, minutes: number): number {
  return hours * 3600 + minutes * 60;
}

function hoursMinutesFromBudget(totalMinutes: number, totalSeconds: number) {
  const budget = sessionBudgetSeconds(totalMinutes, totalSeconds);
  return {
    hours: Math.floor(budget / 3600),
    minutes: Math.floor((budget % 3600) / 60),
  };
}

function budgetToTemplateParts(budgetSeconds: number) {
  return {
    totalMinutes: Math.floor(budgetSeconds / 60),
    totalSeconds: budgetSeconds % 60,
  };
}

function parseNumberInput(value: string, max?: number): number {
  const digits = value.replace(/\D/g, '');
  if (digits === '') return 0;
  let parsed = parseInt(digits.replace(/^0+/, '') || '0', 10);
  if (Number.isNaN(parsed) || parsed < 0) parsed = 0;
  if (max !== undefined) parsed = Math.min(parsed, max);
  return parsed;
}

interface ConfirmDialogState {
  title: string;
  message: string;
}

interface NumericInputProps {
  value: number;
  onChange: (value: number) => void;
  max?: number;
  disabled?: boolean;
  onBlur?: () => void;
}

function NumericInput({ value, onChange, max, disabled, onBlur }: NumericInputProps) {
  return (
    <input
      type="text"
      inputMode="numeric"
      pattern="[0-9]*"
      disabled={disabled}
      value={String(value)}
      onChange={(event) => onChange(parseNumberInput(event.target.value, max))}
      onBlur={onBlur}
    />
  );
}

function speakerTimeClass(seconds: number, limitSeconds: number, visible: boolean): string {
  if (!visible) return '';
  return seconds <= limitSeconds ? 'session-time-ok' : 'session-time-over';
}

function applySessionTemplateFields(template: session.Template) {
  const tmpl = session.Template.createFrom(template);
  const { hours, minutes } = hoursMinutesFromBudget(tmpl.totalMinutes || 0, tmpl.totalSeconds || 0);
  return {
    sessionTotalHours: hours,
    sessionTotalMinutes: minutes,
    sessionSpeakerCount: tmpl.speakerCount || 0,
    sessionSpeakerNames: padSpeakerNames(tmpl.speakerNames, tmpl.speakerCount || 0),
    sessionTalkMinutes: tmpl.talkMinutes || 0,
    sessionTalkSeconds: tmpl.talkSeconds || 0,
    sessionQuestionsMinutes: tmpl.questionsMinutes || 0,
    sessionQuestionsSeconds: tmpl.questionsSeconds || 0,
    sessionUseDefaultTalk: tmpl.useDefaultTalk !== false,
    sessionUseDefaultQuestions: tmpl.useDefaultQuestions !== false,
  };
}

function SettingsLockBanner({ message }: { message: string }) {
  return <div className="settings-lock-banner" role="status">{message}</div>;
}

function templateEntryDescription(entry: templates.Entry): string {
  const tmpl = session.Template.createFrom(entry.template);
  const budget = sessionBudgetSeconds(tmpl.totalMinutes || 0, tmpl.totalSeconds || 0);
  const speakers = tmpl.speakerCount || 0;
  return `${formatClock(budget)} общее · ${speakers} ${speakers === 1 ? 'докладчик' : speakers < 5 ? 'докладчика' : 'докладчиков'}`;
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
  const [reminderMinutes, setReminderMinutes] = useState(2);
  const [reminderSecondsPart, setReminderSecondsPart] = useState(0);
  const [soundId, setSoundId] = useState('chime');
  const [reminderSoundId, setReminderSoundId] = useState('');
  const [questionsSoundId, setQuestionsSoundId] = useState('');
  const [nextSoundId, setNextSoundId] = useState('');
  const [deviceId, setDeviceId] = useState('default');
  const [volume, setVolume] = useState(0.85);
  const [muteConferenceSound, setMuteConferenceSound] = useState(false);
  const [muteConferenceReceive, setMuteConferenceReceive] = useState(true);
  const [devices, setDevices] = useState<AudioDevice[]>([]);
  const [sounds, setSounds] = useState<SoundOption[]>([]);
  const [conferenceUrl, setConferenceUrl] = useState('');
  const [conferenceName, setConferenceName] = useState('Таймер');
  const [conferenceState, setConferenceState] = useState<ConferenceState>(initialConferenceState);
  const [conferenceBusy, setConferenceBusy] = useState(false);
  const [error, setError] = useState('');
  const [saving, setSaving] = useState(false);
  const [importingSound, setImportingSound] = useState(false);
  const [previewingSoundId, setPreviewingSoundId] = useState('');
  const previewingRef = useRef(false);
  const [settingsOpen, setSettingsOpen] = useState(false);
  const [aboutOpen, setAboutOpen] = useState(false);
  const [connectionPromptOpen, setConnectionPromptOpen] = useState(false);
  const [sessionTotalHours, setSessionTotalHours] = useState(0);
  const [sessionTotalMinutes, setSessionTotalMinutes] = useState(0);
  const [sessionSpeakerCount, setSessionSpeakerCount] = useState(0);
  const [sessionSpeakerNames, setSessionSpeakerNames] = useState<string[]>([]);
  const [sessionTalkMinutes, setSessionTalkMinutes] = useState(0);
  const [sessionTalkSeconds, setSessionTalkSeconds] = useState(0);
  const [sessionQuestionsMinutes, setSessionQuestionsMinutes] = useState(0);
  const [sessionQuestionsSeconds, setSessionQuestionsSeconds] = useState(0);
  const [sessionUseDefaultTalk, setSessionUseDefaultTalk] = useState(true);
  const [sessionUseDefaultQuestions, setSessionUseDefaultQuestions] = useState(true);
  const [sessionState, setSessionState] = useState<SessionState>(initialSessionState);
  const [sessionPanelOpen, setSessionPanelOpen] = useState(false);
  const [sessionBusy, setSessionBusy] = useState(false);
  const [confirmDialog, setConfirmDialog] = useState<ConfirmDialogState | null>(null);
  const confirmResolveRef = useRef<((confirmed: boolean) => void) | null>(null);
  const [successMessage, setSuccessMessage] = useState('');
  const [templateModalOpen, setTemplateModalOpen] = useState(false);
  const [templateEntries, setTemplateEntries] = useState<templates.Entry[]>([]);
  const [selectedTemplateId, setSelectedTemplateId] = useState('');
  const [appInfo, setAppInfo] = useState<AppInfo>({
    name: 'Таймер докладов',
    version: '1.0.0',
    url: '',
    urlLabel: '',
  });

  const settingsLocked = snapshot.isRunning;
  const settingsLockMessage = useMemo(() => {
    if (!settingsLocked) return '';
    if (snapshot.isPaused) {
      return 'Таймер на паузе. Сбросьте таймер, чтобы изменить настройки и сессию.';
    }
    return 'Таймер запущен. Сбросьте таймер, чтобы изменить настройки и сессию.';
  }, [settingsLocked, snapshot.isPaused]);
  const conferenceActive = ['opening', 'connecting', 'waitingAdmission', 'joined', 'playing'].includes(conferenceState.phase);
  const conferenceJoined = conferenceState.phase === 'joined' || conferenceState.phase === 'playing';
  const sessionBudgetSecondsValue = sessionBudgetFromHoursMinutes(sessionTotalHours, sessionTotalMinutes);
  const canCreateSession = sessionBudgetSecondsValue > 0 && sessionSpeakerCount >= 1;

  const sessionTemplate = useCallback(() => {
    const budget = budgetToTemplateParts(sessionBudgetSecondsValue);
    return session.Template.createFrom({
    totalMinutes: budget.totalMinutes,
    totalSeconds: budget.totalSeconds,
    speakerCount: sessionSpeakerCount,
    speakerNames: padSpeakerNames(sessionSpeakerNames, sessionSpeakerCount),
    talkMinutes: sessionTalkMinutes,
    talkSeconds: sessionTalkSeconds,
    questionsMinutes: sessionQuestionsMinutes,
    questionsSeconds: sessionQuestionsSeconds,
    useDefaultTalk: sessionUseDefaultTalk,
    useDefaultQuestions: sessionUseDefaultQuestions,
  });
  }, [
    sessionBudgetSecondsValue,
    sessionSpeakerCount,
    sessionSpeakerNames,
    sessionTalkMinutes,
    sessionTalkSeconds,
    sessionQuestionsMinutes,
    sessionQuestionsSeconds,
    sessionUseDefaultTalk,
    sessionUseDefaultQuestions,
  ]);

  const persistSettings = useCallback(async (next?: Partial<settings.Settings>) => {
    const payload = settings.Settings.createFrom({
      talkMinutes: next?.talkMinutes ?? talkMinutes,
      talkSeconds: next?.talkSeconds ?? talkSecondsPart,
      questionsMinutes: next?.questionsMinutes ?? questionsMinutes,
      questionsSeconds: next?.questionsSeconds ?? questionsSecondsPart,
      reminderMinutes: next?.reminderMinutes ?? reminderMinutes,
      reminderSeconds: next?.reminderSeconds ?? reminderSecondsPart,
      soundId: next?.soundId ?? soundId,
      reminderSoundId: next?.reminderSoundId ?? reminderSoundId,
      questionsSoundId: next?.questionsSoundId ?? questionsSoundId,
      nextSoundId: next?.nextSoundId ?? nextSoundId,
      deviceId: next?.deviceId ?? deviceId,
      volume: next?.volume ?? volume,
      muteConferenceSound: next?.muteConferenceSound ?? muteConferenceSound,
      muteConferenceReceive: next?.muteConferenceReceive ?? muteConferenceReceive,
    });

    setSaving(true);
    try {
      await SaveSettings(payload);
      const saved = settings.Settings.createFrom(await GetSettings());
      setMuteConferenceSound(saved.muteConferenceSound ?? false);
      setMuteConferenceReceive(saved.muteConferenceReceive ?? true);
      setVolume(saved.volume);
      setDeviceId(saved.deviceId);
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
    reminderMinutes,
    reminderSecondsPart,
    soundId,
    reminderSoundId,
    questionsSoundId,
    nextSoundId,
    talkMinutes,
    talkSecondsPart,
    volume,
    muteConferenceSound,
    muteConferenceReceive,
  ]);

  useEffect(() => {
    const bootstrap = async () => {
      const [
        initialState,
        initialSettings,
        initialSounds,
        initialDevices,
        initialConference,
        initialAppInfo,
        initialSessionTemplate,
        initialSessionState,
      ] = await Promise.all([
        GetState(),
        GetSettings(),
        GetSounds(),
        GetAudioDevices(),
        GetConferenceState(),
        GetAppInfo(),
        GetSessionTemplate(),
        GetSessionState(),
      ]);

      setSnapshot(initialState as TimerSnapshot);
      setTalkMinutes(initialSettings.talkMinutes);
      setTalkSecondsPart(initialSettings.talkSeconds);
      setQuestionsMinutes(initialSettings.questionsMinutes);
      setQuestionsSecondsPart(initialSettings.questionsSeconds);
      setReminderMinutes(initialSettings.reminderMinutes);
      setReminderSecondsPart(initialSettings.reminderSeconds);
      setSoundId(initialSettings.soundId);
      setReminderSoundId(initialSettings.reminderSoundId ?? '');
      setQuestionsSoundId(initialSettings.questionsSoundId);
      setNextSoundId(initialSettings.nextSoundId);
      setDeviceId(initialSettings.deviceId);
      setVolume(initialSettings.volume);
      setMuteConferenceSound(initialSettings.muteConferenceSound ?? false);
      setMuteConferenceReceive(initialSettings.muteConferenceReceive ?? true);
      setSounds(initialSounds as SoundOption[]);
      setDevices(initialDevices as AudioDevice[]);
      setConferenceState(initialConference as ConferenceState);
      setAppInfo(buildinfo.Info.createFrom(initialAppInfo));
      const template = session.Template.createFrom(initialSessionTemplate);
      const fields = applySessionTemplateFields(template);
      setSessionTotalHours(fields.sessionTotalHours);
      setSessionTotalMinutes(fields.sessionTotalMinutes);
      setSessionSpeakerCount(fields.sessionSpeakerCount);
      setSessionSpeakerNames(fields.sessionSpeakerNames);
      setSessionTalkMinutes(fields.sessionTalkMinutes);
      setSessionTalkSeconds(fields.sessionTalkSeconds);
      setSessionQuestionsMinutes(fields.sessionQuestionsMinutes);
      setSessionQuestionsSeconds(fields.sessionQuestionsSeconds);
      setSessionUseDefaultTalk(fields.sessionUseDefaultTalk);
      setSessionUseDefaultQuestions(fields.sessionUseDefaultQuestions);
      setSessionState(initialSessionState as SessionState);
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

  useEffect(() => {
    const unsubscribe = EventsOn('session:state', (state: SessionState) => {
      setSessionState(state);
    });
    return () => unsubscribe();
  }, []);

  useEffect(() => {
    const unsubscribeError = EventsOn('audio:error', (message: string) => {
      setError(String(message));
    });
    const unsubscribeMuted = EventsOn('audio:muted', (message: string) => {
      setError(String(message));
    });
    return () => {
      unsubscribeError();
      unsubscribeMuted();
    };
  }, []);

  useEffect(() => {
    if (!successMessage) return undefined;
    const timeout = window.setTimeout(() => setSuccessMessage(''), 3000);
    return () => window.clearTimeout(timeout);
  }, [successMessage]);

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

  const askConfirm = useCallback((title: string, message: string) => new Promise<boolean>((resolve) => {
    confirmResolveRef.current = resolve;
    setConfirmDialog({ title, message });
  }), []);

  const closeConfirm = (confirmed: boolean) => {
    confirmResolveRef.current?.(confirmed);
    confirmResolveRef.current = null;
    setConfirmDialog(null);
  };

  const handleDismissAlert = () => {
    DismissAlert();
  };

  const handleCreateSession = async () => {
    if (!canCreateSession || sessionBusy || settingsLocked) return;
    if (sessionState.active && !await askConfirm('Заменить текущую сессию?', 'Накопленное время будет сброшено.')) {
      return;
    }
    setSessionBusy(true);
    try {
      const next = await CreateSession(sessionTemplate());
      setSessionState(next as SessionState);
      setSessionPanelOpen(true);
      setError('');
    } catch (err) {
      setError(String(err));
    } finally {
      setSessionBusy(false);
    }
  };

  const handleResetSession = async () => {
    if (!sessionState.active || sessionBusy) return;
    if (!await askConfirm('Сбросить сессию?', 'Накопленное время будет обнулено, очередь начнётся с первого докладчика.')) {
      return;
    }
    setSessionBusy(true);
    try {
      const next = await ResetSession();
      setSessionState(next as SessionState);
      setError('');
    } catch (err) {
      setError(String(err));
    } finally {
      setSessionBusy(false);
    }
  };

  const handleEndSession = async () => {
    if (!sessionState.active || sessionBusy) return;
    if (!await askConfirm('Завершить сессию?', 'После завершения можно создать новую.')) {
      return;
    }
    setSessionBusy(true);
    try {
      const next = await EndSession();
      setSessionState(next as SessionState);
      setError('');
    } catch (err) {
      setError(String(err));
    } finally {
      setSessionBusy(false);
    }
  };

  const applySessionTemplate = async (tmpl: session.Template) => {
    if (sessionState.active && !await askConfirm('Заменить текущую сессию?', 'Накопленное время будет сброшено.')) {
      return false;
    }
    const fields = applySessionTemplateFields(tmpl);
    setSessionTotalHours(fields.sessionTotalHours);
    setSessionTotalMinutes(fields.sessionTotalMinutes);
    setSessionSpeakerCount(fields.sessionSpeakerCount);
    setSessionSpeakerNames(fields.sessionSpeakerNames);
    setSessionTalkMinutes(fields.sessionTalkMinutes);
    setSessionTalkSeconds(fields.sessionTalkSeconds);
    setSessionQuestionsMinutes(fields.sessionQuestionsMinutes);
    setSessionQuestionsSeconds(fields.sessionQuestionsSeconds);
    setSessionUseDefaultTalk(fields.sessionUseDefaultTalk);
    setSessionUseDefaultQuestions(fields.sessionUseDefaultQuestions);
    const next = await CreateSession(tmpl);
    setSessionState(next as SessionState);
    setSessionPanelOpen(true);
    setError('');
    return true;
  };

  const handleSaveSessionTemplate = async () => {
    setSessionBusy(true);
    try {
      const entry = templates.Entry.createFrom(await SaveSessionTemplate(sessionTemplate()));
      setSuccessMessage(`Шаблон «${entry.name}» сохранён`);
      setError('');
    } catch (err) {
      setError(String(err));
    } finally {
      setSessionBusy(false);
    }
  };

  const handleOpenTemplateModal = async () => {
    if (sessionBusy) return;
    setSessionBusy(true);
    try {
      const entries = (await ListSessionTemplates()).map((entry) => templates.Entry.createFrom(entry));
      setTemplateEntries(entries);
      setSelectedTemplateId(entries[0]?.id ?? '');
      setTemplateModalOpen(true);
      setError('');
    } catch (err) {
      setError(String(err));
    } finally {
      setSessionBusy(false);
    }
  };

  const handleLoadSelectedTemplate = async () => {
    if (settingsLocked || sessionBusy || !selectedTemplateId) return;
    const entry = templateEntries.find((item) => item.id === selectedTemplateId);
    if (!entry) return;
    setSessionBusy(true);
    try {
      const applied = await applySessionTemplate(session.Template.createFrom(entry.template));
      if (applied) {
        setTemplateModalOpen(false);
      }
    } catch (err) {
      setError(String(err));
    } finally {
      setSessionBusy(false);
    }
  };

  const handleDeleteTemplate = async (entry: templates.Entry, event: MouseEvent) => {
    event.stopPropagation();
    if (!await askConfirm('Удалить шаблон?', `«${entry.name}» будет удалён без возможности восстановления.`)) {
      return;
    }
    setSessionBusy(true);
    try {
      await DeleteSessionTemplate(entry.id);
      const entries = (await ListSessionTemplates()).map((item) => templates.Entry.createFrom(item));
      setTemplateEntries(entries);
      setSelectedTemplateId(entries.find((item) => item.id === selectedTemplateId)?.id ?? entries[0]?.id ?? '');
      setError('');
    } catch (err) {
      setError(String(err));
    } finally {
      setSessionBusy(false);
    }
  };

  const handleSpeakerCountChange = (value: number) => {
    const count = Math.min(MAX_SPEAKERS, Math.max(0, value));
    setSessionSpeakerCount(count);
    setSessionSpeakerNames((prev) => padSpeakerNames(prev, count));
  };

  const handleSpeakerNameChange = (index: number, value: string) => {
    setSessionSpeakerNames((prev) => {
      const next = padSpeakerNames(prev, sessionSpeakerCount);
      next[index] = value;
      return next;
    });
  };

  const handlePreview = async (previewSoundId: string) => {
    if (!previewSoundId || previewingRef.current) return;
    previewingRef.current = true;
    setPreviewingSoundId(previewSoundId);
    try {
      await PreviewSound(previewSoundId);
      setError('');
    } catch (err) {
      setError(String(err));
    } finally {
      previewingRef.current = false;
      setPreviewingSoundId('');
    }
  };

  const handleImportSound = async () => {
    setImportingSound(true);
    try {
      const imported = await ImportSound();
      if (imported?.id) {
        const updatedSounds = await GetSounds();
        setSounds(updatedSounds as SoundOption[]);
        setSoundId(imported.id);
        await persistSettings({ soundId: imported.id });
      }
      setError('');
    } catch (err) {
      setError(String(err));
    } finally {
      setImportingSound(false);
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
      setConnectionPromptOpen(true);
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
      const state = await GetConferenceState();
      setConferenceState(state as ConferenceState);
      setError(state.phase === 'error' && state.message ? state.message : String(err));
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

  const handleConferenceBrowserToggle = async () => {
    setConferenceBusy(true);
    try {
      const state = await SetConferenceBrowserVisible(!conferenceState.browserVisible);
      setConferenceState(state as ConferenceState);
      setError('');
    } catch (err) {
      setError(String(err));
    } finally {
      setConferenceBusy(false);
    }
  };

  const handleConferenceDiagnostics = async () => {
    setConferenceBusy(true);
    try {
      const snapshot = await GetConferenceDiagnostics();
      await ClipboardSetText(snapshot);
      setError('');
      setConferenceState({
        ...conferenceState,
        message: 'Диагностика скопирована в буфер обмена',
      });
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

  const icon = (name: 'play' | 'playOutline' | 'pause' | 'questions' | 'next' | 'reset' | 'disconnect' | 'upload' | 'settings' | 'close' | 'browserShow' | 'browserHide' | 'queue' | 'trash') => {
    const paths = {
      play: <path d="M9 6.8v10.4c0 .8.9 1.3 1.6.8l8.2-5.2a.95.95 0 0 0 0-1.6L10.6 6c-.7-.5-1.6 0-1.6.8Z" />,
      playOutline: <path d="M9 7.2v9.6L17.8 12 9 7.2Z" />,
      pause: <><path d="M8 6.5h3v11H8z" /><path d="M14 6.5h3v11h-3z" /></>,
      questions: <><path d="M9.5 9a3 3 0 1 1 4.1 2.8c-1 .4-1.6 1-1.6 2" /><path d="M12 17.5h.01" /></>,
      next: <><path d="m7 6 7 6-7 6V6Z" /><path d="M16 6v12" /></>,
      reset: <><path d="M4.9 7.5A8 8 0 1 1 4 14" /><path d="M4 4v4h4" /></>,
      disconnect: <><path d="M8 5v6" /><path d="M16 5v6" /><path d="M6 10h12v2a6 6 0 0 1-12 0v-2Z" /><path d="M12 18v3" /></>,
      upload: <><path d="M12 16V4" /><path d="m7.5 8.5 4.5-4.5 4.5 4.5" /><path d="M5 14v5h14v-5" /></>,
      settings: <><circle cx="12" cy="12" r="3" /><path d="M19.4 15a1.7 1.7 0 0 0 .3 1.9l.1.1-2.8 2.8-.1-.1a1.7 1.7 0 0 0-1.9-.3 1.7 1.7 0 0 0-1 1.6v.2h-4V21a1.7 1.7 0 0 0-1-1.6 1.7 1.7 0 0 0-1.9.3l-.1.1L4.2 17l.1-.1a1.7 1.7 0 0 0 .3-1.9A1.7 1.7 0 0 0 3 14H2.8v-4H3a1.7 1.7 0 0 0 1.6-1 1.7 1.7 0 0 0-.3-1.9L4.2 7 7 4.2l.1.1a1.7 1.7 0 0 0 1.9.3A1.7 1.7 0 0 0 10 3V2.8h4V3a1.7 1.7 0 0 0 1 1.6 1.7 1.7 0 0 0 1.9-.3l.1-.1L19.8 7l-.1.1a1.7 1.7 0 0 0-.3 1.9 1.7 1.7 0 0 0 1.6 1h.2v4H21a1.7 1.7 0 0 0-1.6 1Z" /></>,
      close: <><path d="m7 7 10 10" /><path d="M17 7 7 17" /></>,
      browserShow: <><rect x="3.5" y="5.5" width="17" height="13" rx="2" /><path d="M3.5 9.5h17" /><circle cx="6.5" cy="7.5" r="0.8" fill="currentColor" stroke="none" /><circle cx="9" cy="7.5" r="0.8" fill="currentColor" stroke="none" /></>,
      browserHide: <><rect x="3.5" y="5.5" width="17" height="13" rx="2" /><path d="M3.5 9.5h17" /><path d="M8 15h8" /></>,
      queue: <><path d="M8 7h11" /><path d="M8 12h11" /><path d="M8 17h11" /><circle cx="5" cy="7" r="1" fill="currentColor" stroke="none" /><circle cx="5" cy="12" r="1" fill="currentColor" stroke="none" /><circle cx="5" cy="17" r="1" fill="currentColor" stroke="none" /></>,
      trash: <><path d="M5 7h14" /><path d="M9.5 7V5.5h5V7" /><path d="M8 7l.7 11.5h6.6L16 7" /></>,
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
    <div className={`app-shell${sessionPanelOpen ? ' has-session-panel' : ''}`}>
      <header className="topbar">
        <div className="topbar-left">
          {conferenceActive && (
            <button
              className={`icon-button quiet conference-browser-toggle${conferenceState.browserVisible ? ' is-visible' : ''}`}
              disabled={conferenceBusy}
              onClick={handleConferenceBrowserToggle}
              aria-label={conferenceState.browserVisible ? 'Скрыть окно браузера ВКС' : 'Показать окно браузера ВКС'}
              title={conferenceState.browserVisible ? 'Скрыть окно ВКС' : 'Показать окно ВКС'}
            >
              {icon(conferenceState.browserVisible ? 'browserHide' : 'browserShow')}
            </button>
          )}
        </div>
        <div className="topbar-right">
          <button
            className={`icon-button quiet${sessionPanelOpen ? ' is-active' : ''}`}
            aria-label={sessionPanelOpen ? 'Скрыть панель сессии' : 'Открыть панель сессии'}
            title={sessionPanelOpen ? 'Скрыть сессию' : 'Сессия'}
            onClick={() => setSessionPanelOpen((open) => !open)}
          >
            {icon('queue')}
          </button>
          <button className="icon-button quiet" aria-label="Открыть настройки" title="Настройки" onClick={() => setSettingsOpen(true)}>
            {icon('settings')}
          </button>
        </div>
      </header>

      <main className="timer-stage">
        <div className="conference-toolbar">
          <button
            className={`conference-badge conference-${conferenceState.phase}`}
            onClick={() => setConnectionPromptOpen(true)}
            title="Настроить подключение к ВКС"
          >
            <span className="connection-dot" />
            <span>{conferencePhaseLabels[conferenceState.phase]}</span>
          </button>
        </div>

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
          <button className="icon-button control-play" onClick={handleStart} disabled={snapshot.isRunning && !snapshot.isPaused} aria-label="Запустить" title="Запустить">
            {icon('play')}
          </button>
          <button className="icon-button control-pause" onClick={() => Pause()} disabled={!snapshot.isRunning || snapshot.isPaused} aria-label="Пауза" title="Пауза">
            {icon('pause')}
          </button>
          <button className="icon-button control-questions" onClick={handleGoToQuestions} disabled={snapshot.phase !== 'talk' && snapshot.phase !== 'talkOvertime'} aria-label="Перейти к вопросам" title="К вопросам">
            {icon('questions')}
          </button>
          <button className="icon-button control-next" onClick={handleNextSpeaker} disabled={!['talk', 'talkOvertime', 'questions', 'questionsOvertime'].includes(snapshot.phase)} aria-label="Следующий докладчик" title="Следующий докладчик">
            {icon('next')}
          </button>
          <button className="icon-button control-reset" onClick={handleReset} aria-label="Сбросить таймер" title="Сбросить таймер">
            {icon('reset')}
          </button>
        </nav>

        {snapshot.alertActive && (
          <button className="alert-chip" onClick={handleDismissAlert}>
            Время вышло · закрыть уведомление
          </button>
        )}
        {error && <div className="error-toast">{error}</div>}
        {successMessage && <div className="success-toast">{successMessage}</div>}
      </main>

      {sessionPanelOpen && (
        <div className="session-backdrop">
          <aside className="session-drawer" role="dialog" aria-modal="true" aria-labelledby="session-title">
            <div className="drawer-header">
              <div>
                <h2 id="session-title">Сессия</h2>
                {sessionState.active && (
                  <p className="session-duration-hint">
                    Доклад {formatClock(snapshot.talkSeconds)} · Вопросы {formatClock(snapshot.questionsSeconds)}
                  </p>
                )}
              </div>
              <button className="icon-button quiet" aria-label="Закрыть панель сессии" onClick={() => setSessionPanelOpen(false)}>
                {icon('close')}
              </button>
            </div>

            {!sessionState.active ? (
              <>
                {settingsLocked && <SettingsLockBanner message={settingsLockMessage} />}
                <div className="session-setup">
                  <div className="session-setup-fields">
                    <label>Общее время<div className="duration-inputs">
                      <NumericInput max={99} value={sessionTotalHours} disabled={settingsLocked} onChange={setSessionTotalHours} /><span>ч</span>
                      <NumericInput max={59} value={sessionTotalMinutes} disabled={settingsLocked} onChange={setSessionTotalMinutes} /><span>мин</span>
                    </div></label>
                    <label>Количество докладчиков
                      <NumericInput
                        value={sessionSpeakerCount}
                        max={MAX_SPEAKERS}
                        disabled={settingsLocked}
                        onChange={handleSpeakerCountChange}
                      />
                    </label>

                    <div className="session-duration-block">
                      <h3>Длительность</h3>
                      <label className="settings-checkbox">
                        <input
                          type="checkbox"
                          checked={sessionUseDefaultTalk}
                          disabled={settingsLocked}
                          onChange={(e) => setSessionUseDefaultTalk(e.target.checked)}
                        />
                        <span>Доклад — как в настройках</span>
                      </label>
                      {sessionUseDefaultTalk ? (
                        <p className="settings-hint">из настроек: {formatClock(talkMinutes * 60 + talkSecondsPart)}</p>
                      ) : (
                        <label>Доклад<div className="duration-inputs">
                          <NumericInput max={180} value={sessionTalkMinutes} disabled={settingsLocked} onChange={setSessionTalkMinutes} /><span>мин</span>
                          <NumericInput max={59} value={sessionTalkSeconds} disabled={settingsLocked} onChange={setSessionTalkSeconds} /><span>сек</span>
                        </div></label>
                      )}
                      <label className="settings-checkbox">
                        <input
                          type="checkbox"
                          checked={sessionUseDefaultQuestions}
                          disabled={settingsLocked}
                          onChange={(e) => setSessionUseDefaultQuestions(e.target.checked)}
                        />
                        <span>Вопросы — как в настройках</span>
                      </label>
                      {sessionUseDefaultQuestions ? (
                        <p className="settings-hint">из настроек: {formatClock(questionsMinutes * 60 + questionsSecondsPart)}</p>
                      ) : (
                        <label>Вопросы<div className="duration-inputs">
                          <NumericInput max={60} value={sessionQuestionsMinutes} disabled={settingsLocked} onChange={setSessionQuestionsMinutes} /><span>мин</span>
                          <NumericInput max={59} value={sessionQuestionsSeconds} disabled={settingsLocked} onChange={setSessionQuestionsSeconds} /><span>сек</span>
                        </div></label>
                      )}
                    </div>

                    {sessionSpeakerCount > 0 && (
                      <div className="speaker-names speaker-names-flex">
                        <span className="speaker-names-label">Имена докладчиков</span>
                        <div className="speaker-names-list">
                          {Array.from({ length: sessionSpeakerCount }, (_, index) => (
                            <input
                              key={index}
                              type="text"
                              maxLength={80}
                              placeholder={`Докладчик ${index + 1}`}
                              value={sessionSpeakerNames[index] ?? ''}
                              disabled={settingsLocked}
                              onChange={(e) => handleSpeakerNameChange(index, e.target.value)}
                            />
                          ))}
                        </div>
                      </div>
                    )}
                  </div>
                </div>
                <footer className="session-drawer-footer session-setup-footer">
                  <div className="session-footer-actions">
                    <button
                      className="text-button primary compact-button"
                      disabled={!canCreateSession || sessionBusy || settingsLocked}
                      title={settingsLocked ? settingsLockMessage : undefined}
                      onClick={handleCreateSession}
                    >
                      Создать сессию
                    </button>
                    <button
                      className="text-button secondary compact-button"
                      disabled={sessionBusy || settingsLocked}
                      title={settingsLocked ? settingsLockMessage : undefined}
                      onClick={handleOpenTemplateModal}
                    >
                      Загрузить из шаблона
                    </button>
                  </div>
                  <button
                    type="button"
                    className="about-link session-save-template"
                    disabled={sessionBusy}
                    onClick={handleSaveSessionTemplate}
                  >
                    Сохранить шаблон
                  </button>
                </footer>
              </>
            ) : (
              <>
                <div className="session-drawer-body">
                  <div className="session-budget-card">
                    <span className="session-budget-label">Время сессии</span>
                    <span className="session-budget">
                      {formatClock(sessionState.usedSeconds)} / {formatClock(sessionState.totalBudgetSeconds)}
                    </span>
                  </div>
                  <div className="session-list-head" aria-hidden="true">
                    <span />
                    <span>#</span>
                    <span>Докладчик</span>
                    <span>Докл.</span>
                    <span>Вопр.</span>
                  </div>
                  <ul className="session-speaker-list">
                    {sessionState.speakers?.map((speaker) => {
                      const showTimes = speaker.status !== 'pending' || speaker.talkSeconds > 0 || speaker.questionsSeconds > 0;
                      const talkClass = speakerTimeClass(speaker.talkSeconds, snapshot.talkSeconds, showTimes);
                      const questionsClass = speakerTimeClass(speaker.questionsSeconds, snapshot.questionsSeconds, showTimes);
                      return (
                        <li key={speaker.index} className={`session-speaker session-speaker-${speaker.status}`}>
                          <span className="session-speaker-dot" aria-hidden="true" />
                          <span className="session-speaker-index">{speaker.index + 1}</span>
                          <span className="session-speaker-name">{speaker.name}</span>
                          <span className={`session-speaker-time${talkClass ? ` ${talkClass}` : ''}`}>
                            {showTimes ? formatClock(speaker.talkSeconds) : '—'}
                          </span>
                          <span className={`session-speaker-time${questionsClass ? ` ${questionsClass}` : ''}`}>
                            {showTimes ? formatClock(speaker.questionsSeconds) : '—'}
                          </span>
                        </li>
                      );
                    })}
                  </ul>
                </div>
                <footer className="session-drawer-footer">
                  <div className={`session-remaining${sessionState.usedSeconds > sessionState.totalBudgetSeconds ? ' session-remaining-over' : ''}`}>
                    {sessionState.usedSeconds > sessionState.totalBudgetSeconds
                      ? `Превышение ${formatClock(sessionState.usedSeconds - sessionState.totalBudgetSeconds)}`
                      : `Осталось ${formatClock(sessionState.remainingSeconds)}`}
                  </div>
                  <div className="session-footer-actions">
                    <button
                      className="text-button secondary compact-button"
                      disabled={sessionBusy}
                      onClick={handleResetSession}
                    >
                      Сбросить сессию
                    </button>
                    <button
                      className="text-button primary compact-button"
                      disabled={sessionBusy}
                      onClick={handleEndSession}
                    >
                      Завершить сессию
                    </button>
                  </div>
                </footer>
              </>
            )}
          </aside>
        </div>
      )}

      <footer className="app-footer-bar">
        <span className="app-version" title={appInfo.name}>v{appInfo.version}</span>
      </footer>

      {connectionPromptOpen && (
        <div className="modal-backdrop" role="presentation">
          <section className="modal connection-modal" role="dialog" aria-modal="true" aria-labelledby="connection-title">
            <button className="icon-button quiet close-button" aria-label="Закрыть" onClick={() => setConnectionPromptOpen(false)}>
              {icon('close')}
            </button>
            <span className="modal-kicker">ВКС</span>
            <h2 id="connection-title">Подключение к ВКС</h2>
            <p className="modal-copy">{conferenceState.message}</p>
            {!conferenceActive && connectionForm}
            <div className="connection-footer">
              <div className={`modal-actions${conferenceActive ? ' conference-active-actions' : ''}`}>
                {!conferenceActive ? (
                  <>
                    <button className="text-button secondary" onClick={() => setConnectionPromptOpen(false)}>Пропустить</button>
                    <button className="text-button primary" disabled={conferenceBusy} onClick={handleConferenceConnect}>Подключиться</button>
                  </>
                ) : conferenceActive ? (
                  <div className="conference-icon-actions">
                    {conferenceJoined ? (
                      <>
                        <button
                          className="icon-button conference-test-button"
                          disabled={conferenceBusy}
                          onClick={handleConferenceTest}
                          aria-label="Проверить звук в ВКС"
                          title="Проверить звук в ВКС"
                        >
                          {icon('play')}
                        </button>
                        <button
                          className="icon-button conference-disconnect-button"
                          disabled={conferenceBusy}
                          onClick={handleConferenceDisconnect}
                          aria-label="Отключиться от ВКС"
                          title="Отключиться"
                        >
                          {icon('disconnect')}
                        </button>
                      </>
                    ) : (
                      <>
                        {(conferenceState.phase === 'connecting' || conferenceState.phase === 'waitingAdmission') && (
                          <button className="text-button secondary compact-button" disabled={conferenceBusy} onClick={handleConferenceConfirm}>Я уже подключён</button>
                        )}
                      </>
                    )}
                    {import.meta.env.DEV && (
                      <button
                        className="text-button secondary compact-button"
                        disabled={conferenceBusy}
                        onClick={handleConferenceDiagnostics}
                      >
                        Диагностика
                      </button>
                    )}
                  </div>
                ) : null}
              </div>
              {conferenceJoined && (
                <div className={`conference-test-state ${conferenceState.tested ? 'is-ready' : ''}`}>
                  <span className="connection-dot" />
                  {conferenceState.tested ? 'Звук проверен' : 'Проверьте звук перед запуском'}
                </div>
              )}
            </div>
          </section>
        </div>
      )}

      {settingsOpen && (
        <div className="modal-backdrop drawer-backdrop" onMouseDown={(event) => event.target === event.currentTarget && setSettingsOpen(false)}>
          <aside className="settings-drawer" role="dialog" aria-modal="true" aria-labelledby="settings-title">
            <div className="drawer-header">
              <div><h2 id="settings-title">Настройки</h2></div>
              <button className="icon-button quiet" aria-label="Закрыть настройки" onClick={() => setSettingsOpen(false)}>{icon('close')}</button>
            </div>

            {settingsLocked && <SettingsLockBanner message={settingsLockMessage} />}

            <div className="settings-section">
              <h3>Длительность</h3>
              <label>Доклад<div className="duration-inputs">
                <NumericInput max={180} value={talkMinutes} disabled={settingsLocked} onChange={setTalkMinutes} onBlur={() => persistSettings()} /><span>мин</span>
                <NumericInput max={59} value={talkSecondsPart} disabled={settingsLocked} onChange={setTalkSecondsPart} onBlur={() => persistSettings()} /><span>сек</span>
              </div></label>
              <label>Вопросы<div className="duration-inputs">
                <NumericInput max={60} value={questionsMinutes} disabled={settingsLocked} onChange={setQuestionsMinutes} onBlur={() => persistSettings()} /><span>мин</span>
                <NumericInput max={59} value={questionsSecondsPart} disabled={settingsLocked} onChange={setQuestionsSecondsPart} onBlur={() => persistSettings()} /><span>сек</span>
              </div></label>
              <label>Повтор сигнала при просрочке<div className="duration-inputs">
                <NumericInput max={60} value={reminderMinutes} disabled={settingsLocked} onChange={setReminderMinutes} onBlur={() => persistSettings()} /><span>мин</span>
                <NumericInput max={59} value={reminderSecondsPart} disabled={settingsLocked} onChange={setReminderSecondsPart} onBlur={() => persistSettings()} /><span>сек</span>
              </div></label>
            </div>

            <div className="settings-section">
              <h3>Звук</h3>
              <label>Сигнал окончания времени<div className="sound-picker-row">
                <select value={soundId} disabled={settingsLocked} onChange={async (e) => { const next = e.target.value; setSoundId(next); await persistSettings({ soundId: next }); }}>
                  {sounds.map((sound) => <option key={sound.id} value={sound.id}>{sound.label}</option>)}
                </select>
                <button className={`icon-button sound-preview-button${previewingSoundId === soundId ? ' is-playing' : ''}`} disabled={previewingSoundId !== ''} onClick={() => handlePreview(soundId)} aria-label={previewingSoundId === soundId ? 'Сигнал воспроизводится' : 'Прослушать выбранный сигнал'} title={previewingSoundId === soundId ? 'Воспроизводится' : 'Прослушать'}>
                  {icon('playOutline')}
                </button>
              </div></label>
              <label>Сигнал при просрочке<div className="sound-picker-row">
                <select value={reminderSoundId} disabled={settingsLocked} onChange={async (e) => { const next = e.target.value; setReminderSoundId(next); await persistSettings({ reminderSoundId: next }); }}>
                  <option value="">Как сигнал окончания</option>
                  {sounds.map((sound) => <option key={sound.id} value={sound.id}>{sound.label}</option>)}
                </select>
                <button className={`icon-button sound-preview-button${previewingSoundId === reminderSoundId && reminderSoundId ? ' is-playing' : ''}`} disabled={!reminderSoundId || previewingSoundId !== ''} onClick={() => handlePreview(reminderSoundId)} aria-label={previewingSoundId === reminderSoundId ? 'Сигнал просрочки воспроизводится' : 'Прослушать сигнал просрочки'} title={previewingSoundId === reminderSoundId ? 'Воспроизводится' : 'Прослушать'}>
                  {icon('playOutline')}
                </button>
              </div></label>
              <label>«Время вопросов» в ВКС<div className="sound-picker-row">
                <select value={questionsSoundId} disabled={settingsLocked} onChange={async (e) => { const next = e.target.value; setQuestionsSoundId(next); await persistSettings({ questionsSoundId: next }); }}>
                  <option value="">Выключено</option>
                  {sounds.map((sound) => <option key={sound.id} value={sound.id}>{sound.label}</option>)}
                </select>
                <button className={`icon-button sound-preview-button${previewingSoundId === questionsSoundId && questionsSoundId ? ' is-playing' : ''}`} disabled={!questionsSoundId || previewingSoundId !== ''} onClick={() => handlePreview(questionsSoundId)} aria-label={previewingSoundId === questionsSoundId ? 'Звук вопросов воспроизводится' : 'Прослушать звук вопросов'} title={previewingSoundId === questionsSoundId ? 'Воспроизводится' : 'Прослушать'}>
                  {icon('playOutline')}
                </button>
              </div></label>
              <label>«Следующий докладчик» в ВКС<div className="sound-picker-row">
                <select value={nextSoundId} disabled={settingsLocked} onChange={async (e) => { const next = e.target.value; setNextSoundId(next); await persistSettings({ nextSoundId: next }); }}>
                  <option value="">Выключено</option>
                  {sounds.map((sound) => <option key={sound.id} value={sound.id}>{sound.label}</option>)}
                </select>
                <button className={`icon-button sound-preview-button${previewingSoundId === nextSoundId && nextSoundId ? ' is-playing' : ''}`} disabled={!nextSoundId || previewingSoundId !== ''} onClick={() => handlePreview(nextSoundId)} aria-label={previewingSoundId === nextSoundId ? 'Звук следующего докладчика воспроизводится' : 'Прослушать звук следующего докладчика'} title={previewingSoundId === nextSoundId ? 'Воспроизводится' : 'Прослушать'}>
                  {icon('playOutline')}
                </button>
              </div></label>
              <button className="compact-import-button" disabled={importingSound || settingsLocked} onClick={handleImportSound}>
                {icon('upload')}
                {importingSound ? 'Импорт…' : 'Добавить аудио'}
              </button>
              <p className="settings-hint">Поддерживаются WAV, MP3 и OGG до 20 МБ и 5 минут.</p>
              <label>Устройство<select value={deviceId} disabled={settingsLocked} onChange={async (e) => { const next = e.target.value; setDeviceId(next); await persistSettings({ deviceId: next }); }}>
                {devices.map((device) => <option key={device.id} value={device.id}>{device.name}</option>)}
              </select></label>
              <label>Громкость<input type="range" min={0} max={1} step={0.05} value={volume} disabled={settingsLocked} onChange={(e) => setVolume(Number(e.target.value))} onMouseUp={() => persistSettings()} onTouchEnd={() => persistSettings()} /></label>
              <label className="settings-checkbox">
                <input
                  type="checkbox"
                  checked={muteConferenceSound}
                  disabled={settingsLocked}
                  onChange={async (e) => {
                    const next = e.target.checked;
                    setMuteConferenceSound(next);
                    await persistSettings({ muteConferenceSound: next });
                  }}
                />
                <span>Не воспроизводить сигналы таймера на этом компьютере</span>
              </label>
              <p className="settings-hint">Сигналы таймера будут передаваться только через участника ВКС, без дублирования на колонках.</p>
              <label className="settings-checkbox">
                <input
                  type="checkbox"
                  checked={muteConferenceReceive}
                  disabled={settingsLocked}
                  onChange={async (e) => {
                    const next = e.target.checked;
                    setMuteConferenceReceive(next);
                    await persistSettings({ muteConferenceReceive: next });
                  }}
                />
                <span>Не воспроизводить звук участников в окне ВКС таймера</span>
              </label>
              <p className="settings-hint">Отключает голоса докладчиков в окне браузера таймера. Рекомендуется, если модератор уже слушает встречу в основном браузере.</p>
            </div>

            <button type="button" className="about-link" onClick={() => setAboutOpen(true)}>
              О программе
            </button>            
          </aside>
        </div>
      )}

      {aboutOpen && (
        <div className="modal-backdrop about-backdrop" role="presentation" onMouseDown={(event) => event.target === event.currentTarget && setAboutOpen(false)}>
          <section className="modal about-modal" role="dialog" aria-modal="true" aria-labelledby="about-title">
            <button className="icon-button quiet close-button" aria-label="Закрыть" onClick={() => setAboutOpen(false)}>
              {icon('close')}
            </button>
            <div className="about-modal-body">
              <h2 id="about-title">{appInfo.name}</h2>
              <p className="about-version">Версия {appInfo.version}</p>
              {appInfo.url && (
                <button
                  type="button"
                  className="about-url-link"
                  onClick={() => BrowserOpenURL(appInfo.url)}
                >
                  {appInfo.urlLabel || appInfo.url}
                </button>
              )}
            </div>
          </section>
        </div>
      )}

      {templateModalOpen && (
        <div
          className="modal-backdrop template-backdrop"
          role="presentation"
          onMouseDown={(event) => event.target === event.currentTarget && setTemplateModalOpen(false)}
        >
          <section className="modal template-modal" role="dialog" aria-modal="true" aria-labelledby="template-title">
            <button className="icon-button quiet close-button" aria-label="Закрыть" onClick={() => setTemplateModalOpen(false)}>
              {icon('close')}
            </button>
            <h2 id="template-title">Загрузить шаблон</h2>
            {settingsLocked ? (
              <SettingsLockBanner message={settingsLockMessage} />
            ) : templateEntries.length === 0 ? (
              <p className="modal-copy template-empty">Нет сохранённых шаблонов</p>
            ) : (
              <div className="template-table-wrap">
                <table className="template-table">
                  <thead>
                    <tr>
                      <th scope="col">Шаблон</th>
                      <th scope="col">Параметры</th>
                      <th scope="col" className="template-table-actions-head"><span className="sr-only">Действия</span></th>
                    </tr>
                  </thead>
                  <tbody>
                    {templateEntries.map((entry) => (
                      <tr
                        key={entry.id}
                        className={selectedTemplateId === entry.id ? 'is-selected' : undefined}
                        onClick={() => setSelectedTemplateId(entry.id)}
                      >
                        <td className="template-table-name">{entry.name}</td>
                        <td className="template-table-meta">{templateEntryDescription(entry)}</td>
                        <td className="template-table-actions">
                          <button
                            type="button"
                            className="icon-button quiet template-delete-button"
                            aria-label={`Удалить шаблон ${entry.name}`}
                            disabled={sessionBusy}
                            onClick={(event) => handleDeleteTemplate(entry, event)}
                          >
                            {icon('trash')}
                          </button>
                        </td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            )}
            <div className="modal-actions template-actions">
              <button type="button" className="text-button secondary compact-button" onClick={() => setTemplateModalOpen(false)}>Отмена</button>
              <button
                type="button"
                className="text-button primary compact-button"
                disabled={settingsLocked || sessionBusy || !selectedTemplateId}
                onClick={handleLoadSelectedTemplate}
              >
                Загрузить
              </button>
            </div>
          </section>
        </div>
      )}

      {confirmDialog && (
        <div
          className="modal-backdrop confirm-backdrop"
          role="presentation"
          onMouseDown={(event) => event.target === event.currentTarget && closeConfirm(false)}
        >
          <section className="modal confirm-modal" role="dialog" aria-modal="true" aria-labelledby="confirm-title">
            <h2 id="confirm-title">{confirmDialog.title}</h2>
            <p className="modal-copy confirm-copy">{confirmDialog.message}</p>
            <div className="modal-actions">
              <button type="button" className="text-button secondary compact-button" onClick={() => closeConfirm(false)}>Отмена</button>
              <button type="button" className="text-button primary compact-button" onClick={() => closeConfirm(true)}>Подтвердить</button>
            </div>
          </section>
        </div>
      )}
    </div>
  );
}

export default App;
