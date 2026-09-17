import { useCallback, useEffect, useMemo, useRef, useState, type KeyboardEvent, type MouseEvent } from 'react';
import './styles.css';
import { useTimerDisplayFit } from './useTimerDisplayFit';
import { useWidgetTimerFit } from './useWidgetTimerFit';
import {
  ConfirmConferenceJoined,
  ConnectConference,
  CreateSession,
  EndSession,
  DeleteSessionTemplate,
  DisconnectConference,
  EnterWidgetMode,
  ExitWidgetMode,
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
  IsWidgetMode,
  ListSessionTemplates,
  NextSpeaker,
  Pause,
  PreviewSound,
  Reset,
  ResetSession,
  SaveSessionTemplate,
  SaveSettings,
  SetTalkDurationOverride,
  SetWidgetQuickTimeOpen,
  SetConferenceBrowserVisible,
  SetConferenceCameraEnabled,
  Start,
  TestConferenceSound,
} from '../wailsjs/go/main/App';
import {
  EventsOn,
  BrowserOpenURL,
  ClipboardSetText,
  WindowSetBackgroundColour,
  WindowSetDarkTheme,
  WindowSetLightTheme,
} from '../wailsjs/runtime/runtime';
import { buildinfo, session, settings, templates, timer } from '../wailsjs/go/models';
import { TIMER_FONT_OPTIONS, timerFontClass, timerFontStyle } from './timerFonts';
import {
  buildWidgetColorStyle,
  EMPTY_WIDGET_COLORS,
  hasCustomWidgetColors,
  widgetColorPreviewClass,
  type WidgetColorKey,
  type WidgetColorSettings,
} from './widgetColors';

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
  cameraEnabled: boolean;
  updatedAt: number;
}

const initialConferenceState: ConferenceState = {
  phase: 'idle',
  platform: '',
  displayUrl: '',
  message: 'Участник не подключён',
  tested: false,
  browserVisible: false,
  cameraEnabled: false,
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
const MIN_TIMER_SCALE = 80;
const MAX_TIMER_SCALE = 140;
const DEFAULT_TIMER_SCALE = 115;
const MIN_WIDGET_DURATION = 1;
const MAX_WIDGET_DURATION = 180;
const MIN_WIDGET_BACKGROUND_TRANSPARENCY = 0;
const MAX_WIDGET_BACKGROUND_TRANSPARENCY = 100;

type WidgetPlacement = 'topRight' | 'topCenter' | 'topLeft' | 'free';
type AppTheme = 'dark' | 'light';
type WidgetTheme = 'dark' | 'light' | 'green' | 'transparent';
type WidgetShape = 'rounded' | 'rectangular';
const WIDGET_THEME_OPTIONS: Array<[WidgetTheme, string]> = [
  ['dark', 'Тёмная'],
  ['light', 'Светлая'],
  ['green', 'Зелёная'],
  ['transparent', 'Прозрачная'],
];
const APP_THEME_WINDOW_BACKGROUND: Record<AppTheme, [number, number, number, number]> = {
  dark: [8, 11, 18, 255],
  light: [255, 255, 255, 255],
};
const WIDGET_THEME_WINDOW_BACKGROUND: Record<WidgetTheme, [number, number, number, number]> = {
  dark: [8, 11, 18, 255],
  light: [255, 255, 255, 255],
  green: [8, 120, 77, 255],
  transparent: [0, 0, 0, 0],
};
type TimerDisplayMode = 'ring' | 'digital';
type TimerFont = string;
type SettingsTab = 'timer' | 'interface' | 'sound';
const settingsTabs: Array<[SettingsTab, string]> = [['timer', 'Таймер'], ['interface', 'Интерфейс'], ['sound', 'Звук']];

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
  const [conferenceCameraEnabled, setConferenceCameraEnabled] = useState(false);
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
  const [settingsTab, setSettingsTab] = useState<SettingsTab>('timer');
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
  const [timerScalePercent, setTimerScalePercent] = useState(DEFAULT_TIMER_SCALE);
  const [timerDisplayMode, setTimerDisplayMode] = useState<TimerDisplayMode>('ring');
  const [timerFont, setTimerFont] = useState<TimerFont>('system');
  const [widgetPlacement, setWidgetPlacement] = useState<WidgetPlacement>('topRight');
  const [appTheme, setAppTheme] = useState<AppTheme>('dark');
  const [widgetTheme, setWidgetTheme] = useState<WidgetTheme>('dark');
  const [widgetShape, setWidgetShape] = useState<WidgetShape>('rounded');
  const [widgetBackgroundTransparency, setWidgetBackgroundTransparency] = useState(0);
  const [widgetColors, setWidgetColors] = useState<WidgetColorSettings>(EMPTY_WIDGET_COLORS);
  const [widgetColorPreviewKey, setWidgetColorPreviewKey] = useState<WidgetColorKey | null>(null);
  const [widgetMode, setWidgetMode] = useState(false);
  const [widgetDurationOpen, setWidgetDurationOpen] = useState(false);
  const [widgetDurationDraft, setWidgetDurationDraft] = useState('10');
  const [widgetDurationCustomOpen, setWidgetDurationCustomOpen] = useState(false);
  const [widgetDurationInvalid, setWidgetDurationInvalid] = useState(false);
  const widgetDurationInputRef = useRef<HTMLInputElement>(null);
  const widgetDurationRef = useRef<HTMLDivElement>(null);

  const settingsLocked = snapshot.isRunning;
  const settingsLockMessage = useMemo(() => {
    if (!settingsLocked) return '';
    if (snapshot.isPaused) {
      return 'Таймер на паузе. Сбросьте таймер, чтобы изменить настройки и сессию.';
    }
    return 'Таймер запущен. Сбросьте таймер, чтобы изменить настройки и сессию.';
  }, [settingsLocked, snapshot.isPaused]);
  const handleSettingsTabKeyDown = (event: KeyboardEvent<HTMLButtonElement>, current: SettingsTab) => {
    const direction = event.key === 'ArrowRight' ? 1 : event.key === 'ArrowLeft' ? -1 : 0;
    if (!direction && event.key !== 'Home' && event.key !== 'End') return;
    event.preventDefault();
    const currentIndex = settingsTabs.findIndex(([tab]) => tab === current);
    const nextIndex = event.key === 'Home' ? 0 : event.key === 'End' ? settingsTabs.length - 1 : (currentIndex + direction + settingsTabs.length) % settingsTabs.length;
    const [nextTab] = settingsTabs[nextIndex];
    setSettingsTab(nextTab);
    document.getElementById(`settings-tab-${nextTab}`)?.focus();
  };
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
      conferenceCameraEnabled: next?.conferenceCameraEnabled ?? conferenceCameraEnabled,
      timerScalePercent: next?.timerScalePercent ?? timerScalePercent,
      timerDisplayMode: next?.timerDisplayMode ?? timerDisplayMode,
      timerFont: next?.timerFont ?? timerFont,
      widgetPlacement: next?.widgetPlacement ?? widgetPlacement,
      appTheme: next?.appTheme ?? appTheme,
      widgetTheme: next?.widgetTheme ?? widgetTheme,
      widgetShape: next?.widgetShape ?? widgetShape,
      widgetBackgroundTransparency: next?.widgetBackgroundTransparency ?? widgetBackgroundTransparency,
      widgetColorIdle: next?.widgetColorIdle ?? widgetColors.widgetColorIdle,
      widgetColorRunning: next?.widgetColorRunning ?? widgetColors.widgetColorRunning,
      widgetColorPaused: next?.widgetColorPaused ?? widgetColors.widgetColorPaused,
      widgetColorOvertime: next?.widgetColorOvertime ?? widgetColors.widgetColorOvertime,
    });

    setSaving(true);
    try {
      await SaveSettings(payload);
      const saved = settings.Settings.createFrom(await GetSettings());
      setMuteConferenceSound(saved.muteConferenceSound ?? false);
      setMuteConferenceReceive(saved.muteConferenceReceive ?? true);
      setConferenceCameraEnabled(saved.conferenceCameraEnabled ?? false);
      setTimerScalePercent(saved.timerScalePercent || DEFAULT_TIMER_SCALE);
      setTimerDisplayMode((saved.timerDisplayMode as TimerDisplayMode) || 'ring');
      setTimerFont((saved.timerFont as TimerFont) || 'system');
      setWidgetPlacement((saved.widgetPlacement as WidgetPlacement) || 'topRight');
      setAppTheme((saved.appTheme as AppTheme) || 'dark');
      setWidgetTheme((saved.widgetTheme as WidgetTheme) || 'dark');
      setWidgetShape((saved.widgetShape as WidgetShape) || 'rounded');
      setWidgetBackgroundTransparency(Math.min(MAX_WIDGET_BACKGROUND_TRANSPARENCY, Math.max(MIN_WIDGET_BACKGROUND_TRANSPARENCY, saved.widgetBackgroundTransparency ?? 0)));
      setWidgetColors({
        widgetColorIdle: saved.widgetColorIdle ?? '',
        widgetColorRunning: saved.widgetColorRunning ?? '',
        widgetColorPaused: saved.widgetColorPaused ?? '',
        widgetColorOvertime: saved.widgetColorOvertime ?? '',
      });
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
    conferenceCameraEnabled,
    timerScalePercent,
    timerDisplayMode,
    timerFont,
    widgetPlacement,
    appTheme,
    widgetTheme,
    widgetShape,
    widgetBackgroundTransparency,
    widgetColors,
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
      setConferenceCameraEnabled(initialSettings.conferenceCameraEnabled ?? false);
      setTimerScalePercent(initialSettings.timerScalePercent || DEFAULT_TIMER_SCALE);
      setTimerDisplayMode((initialSettings.timerDisplayMode as TimerDisplayMode) || 'ring');
      setTimerFont((initialSettings.timerFont as TimerFont) || 'system');
      setWidgetPlacement((initialSettings.widgetPlacement as WidgetPlacement) || 'topRight');
      setAppTheme((initialSettings.appTheme as AppTheme) || 'dark');
      setWidgetTheme((initialSettings.widgetTheme as WidgetTheme) || 'dark');
      setWidgetShape((initialSettings.widgetShape as WidgetShape) || 'rounded');
      setWidgetBackgroundTransparency(Math.min(MAX_WIDGET_BACKGROUND_TRANSPARENCY, Math.max(MIN_WIDGET_BACKGROUND_TRANSPARENCY, initialSettings.widgetBackgroundTransparency ?? 0)));
      setWidgetColors({
        widgetColorIdle: initialSettings.widgetColorIdle ?? '',
        widgetColorRunning: initialSettings.widgetColorRunning ?? '',
        widgetColorPaused: initialSettings.widgetColorPaused ?? '',
        widgetColorOvertime: initialSettings.widgetColorOvertime ?? '',
      });
      setWidgetMode(await IsWidgetMode());
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
    if (widgetMode) {
      const [r, g, b, a] = WIDGET_THEME_WINDOW_BACKGROUND[widgetTheme];
      WindowSetBackgroundColour(r, g, b, a);
      return;
    }
    if (appTheme === 'light') {
      WindowSetLightTheme();
    } else {
      WindowSetDarkTheme();
    }
    const [r, g, b, a] = APP_THEME_WINDOW_BACKGROUND[appTheme];
    WindowSetBackgroundColour(r, g, b, a);
  }, [widgetMode, widgetTheme, appTheme]);

  useEffect(() => {
    if (!widgetDurationOpen) return undefined;
    const handlePointerDown = (event: PointerEvent) => {
      if (!widgetDurationRef.current?.contains(event.target as Node)) {
        setWidgetDurationOpen(false);
        setWidgetDurationCustomOpen(false);
        void SetWidgetQuickTimeOpen(false);
      }
    };
    const handleKeyDown = (event: globalThis.KeyboardEvent) => {
      if (event.key === 'Escape') {
        if (widgetDurationCustomOpen) {
          setWidgetDurationCustomOpen(false);
          setWidgetDurationInvalid(false);
        } else {
          setWidgetDurationOpen(false);
          void SetWidgetQuickTimeOpen(false);
        }
      }
    };
    document.addEventListener('pointerdown', handlePointerDown);
    document.addEventListener('keydown', handleKeyDown);
    return () => {
      document.removeEventListener('pointerdown', handlePointerDown);
      document.removeEventListener('keydown', handleKeyDown);
    };
  }, [widgetDurationOpen, widgetDurationCustomOpen]);

  useEffect(() => {
    if (!widgetDurationCustomOpen) return;
    widgetDurationInputRef.current?.focus();
    widgetDurationInputRef.current?.select();
  }, [widgetDurationCustomOpen]);

  useEffect(() => {
    const unsubscribe = EventsOn('timer:state', (state: TimerSnapshot) => {
      setSnapshot(state);
    });
    return () => unsubscribe();
  }, []);

  useEffect(() => {
    const unsubscribe = EventsOn('conference:state', (state: ConferenceState) => {
      setConferenceState(state);
      setConferenceCameraEnabled(state.cameraEnabled ?? false);
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
    const unsubscribe = EventsOn('window:widget', (enabled: boolean) => {
      setWidgetMode(Boolean(enabled));
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
      return formatClock(snapshot.talkSeconds);
    }
    return formatClock(snapshot.remainingSeconds);
  }, [snapshot]);

  const statusClass = useMemo(() => {
    if (snapshot.alertActive) return 'status-alert';
    if (snapshot.phase.includes('Overtime')) return 'status-overtime';
    if (snapshot.isPaused) return 'status-paused';
    if (snapshot.isRunning) return 'status-running';
    return 'status-idle';
  }, [snapshot]);

  const widgetDurationMinutes = Math.max(1, Math.round(snapshot.talkSeconds / 60));
  const closeWidgetDuration = useCallback(() => {
    setWidgetDurationOpen(false);
    setWidgetDurationCustomOpen(false);
    setWidgetDurationInvalid(false);
    void SetWidgetQuickTimeOpen(false);
  }, []);

  const handleStart = async () => {
    closeWidgetDuration();
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
    closeWidgetDuration();
    try {
      await GoToQuestions();
      setError('');
    } catch (err) {
      setError(String(err));
    }
  };

  const handleNextSpeaker = async () => {
    closeWidgetDuration();
    try {
      await NextSpeaker();
      setError('');
    } catch (err) {
      setError(String(err));
    }
  };

  const openWidgetDuration = async () => {
    setWidgetDurationDraft(String(widgetDurationMinutes));
    setWidgetDurationCustomOpen(false);
    setWidgetDurationInvalid(false);
    try {
      await SetWidgetQuickTimeOpen(true);
      setWidgetDurationOpen(true);
      setError('');
    } catch (err) {
      setError(String(err));
    }
  };
  const toggleWidgetDuration = async () => {
    if (widgetDurationOpen) {
      closeWidgetDuration();
      return;
    }
    await openWidgetDuration();
  };

  useEffect(() => {
    if (widgetDurationOpen && snapshot.isRunning && !snapshot.isPaused) {
      closeWidgetDuration();
    }
  }, [widgetDurationOpen, snapshot.isRunning, snapshot.isPaused, closeWidgetDuration]);

  const applyWidgetDuration = async (value: string | number = widgetDurationDraft, keepOpen = false) => {
    const parsed = Number(value);
    if (!Number.isInteger(parsed) || parsed < MIN_WIDGET_DURATION || parsed > MAX_WIDGET_DURATION) {
      setWidgetDurationInvalid(true);
      return;
    }
    try {
      await SetTalkDurationOverride(parsed);
      setTalkMinutes(parsed);
      setTalkSecondsPart(0);
      setWidgetDurationDraft(String(parsed));
      setWidgetDurationInvalid(false);
      await persistSettings({ talkMinutes: parsed, talkSeconds: 0 });
      if (!keepOpen) closeWidgetDuration();
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

  const handleConferenceCameraToggle = async (enabled: boolean) => {
    setConferenceBusy(true);
    try {
      const state = await SetConferenceCameraEnabled(enabled);
      setConferenceState(state as ConferenceState);
      setConferenceCameraEnabled(state.cameraEnabled ?? enabled);
      setError('');
    } catch (err) {
      setError(String(err));
    } finally {
      setConferenceBusy(false);
    }
  };

  const handleEnterWidget = async () => {
    try {
      setSettingsOpen(false);
      setSessionPanelOpen(false);
      setConnectionPromptOpen(false);
      setAboutOpen(false);
      setTemplateModalOpen(false);
      await EnterWidgetMode();
      setWidgetMode(true);
      setError('');
    } catch (err) {
      setError(String(err));
    }
  };

  const handleExitWidget = async () => {
    try {
      await ExitWidgetMode();
      setWidgetMode(false);
      setError('');
    } catch (err) {
      setError(String(err));
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

  const widgetColorStyle = useMemo(() => buildWidgetColorStyle(widgetColors), [widgetColors]);
  const widgetTransparencyDisabled = widgetTheme === 'transparent';
  const widgetTransparencyDisplay = widgetTransparencyDisabled
    ? MAX_WIDGET_BACKGROUND_TRANSPARENCY
    : widgetBackgroundTransparency;
  const widgetTransparencyStyle = useMemo(() => {
    if (widgetTheme === 'transparent') {
      return {
        '--widget-bg-alpha': '0',
        '--widget-blur': '0px',
      } as React.CSSProperties;
    }
    return {
      '--widget-bg-alpha': String(1 - widgetBackgroundTransparency / 100),
      '--widget-blur': `${Math.round(12 * (1 - widgetBackgroundTransparency / 100))}px`,
    } as React.CSSProperties;
  }, [widgetBackgroundTransparency, widgetTheme]);
  const widgetPreviewStatusClass = useMemo(
    () => widgetColorPreviewClass(widgetColorPreviewKey, statusClass),
    [widgetColorPreviewKey, statusClass],
  );
  const shellFontStyle = useMemo(() => timerFontStyle(timerFont), [timerFont]);
  const timerScaleStyle = useMemo(() => {
    const scaledSize = 68 * timerScalePercent / 100;
    return {
      '--timer-size-width': `${scaledSize}cqw`,
      '--timer-size-height': `${scaledSize}cqh`,
    } as React.CSSProperties;
  }, [timerScalePercent]);
  const timerHasCaption = snapshot.phase.includes('Overtime');
  const timerHasOvertime = snapshot.phase.includes('Overtime');
  const {
    viewportRef: timerViewportRef,
    ringRef: timerRingRef,
    contentRef: timerContentRef,
    valueRef: timerValueRef,
  } = useTimerDisplayFit({
    active: !widgetMode,
    mode: timerDisplayMode,
    scalePercent: timerScalePercent,
    fontId: timerFont,
    hasCaption: timerHasCaption,
    hasOvertime: timerHasOvertime,
  });
  const { bodyRef: widgetBodyRef, timerRef: widgetTimerRef } = useWidgetTimerFit({
    fontId: timerFont,
    active: widgetMode,
  });
  const { bodyRef: widgetPreviewBodyRef, timerRef: widgetPreviewTimerRef } = useWidgetTimerFit({
    fontId: timerFont,
    active: settingsOpen && settingsTab === 'interface',
  });

  const icon = (name: 'play' | 'playOutline' | 'pause' | 'questions' | 'next' | 'reset' | 'disconnect' | 'upload' | 'settings' | 'close' | 'browserShow' | 'browserHide' | 'queue' | 'trash' | 'widget' | 'restore' | 'clock' | 'edit' | 'check' | 'sun' | 'moon') => {
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
      widget: <><rect x="4.5" y="4.5" width="15" height="15" rx="2.5" /><rect x="13" y="6.5" width="5.5" height="4.5" rx="1" /></>,
      restore: <><rect x="6.5" y="6.5" width="12" height="12" rx="1.8" /><path d="M9.5 6.5v-.7A1.8 1.8 0 0 1 11.3 4h6.9A1.8 1.8 0 0 1 20 5.8v6.9a1.8 1.8 0 0 1-1.5 1.8" /></>,
      clock: <><circle cx="12" cy="12" r="8" /><path d="M12 7v5l3 2" /></>,
      edit: <><path d="m5 16-.8 4 4-.8L19 8.4a2.1 2.1 0 0 0-3-3L5 16Z" /><path d="m14.5 7.5 3 3" /></>,
      check: <path d="m6.5 12.5 4 4 8.5-8.5" />,
      sun: <><circle cx="12" cy="12" r="4" /><path d="M12 2.5v2.2M12 19.3v2.2M4.5 12H2.3M21.7 12h-2.2M5.8 5.8 4.3 4.3M19.7 19.7l-1.5-1.5M18.2 5.8l1.5-1.5M5.8 18.2l-1.5 1.5" /></>,
      moon: <path d="M20 14.2A7.5 7.5 0 0 1 9.8 4 6.5 6.5 0 1 0 20 14.2Z" />,
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

  const widgetIsPaused = snapshot.isRunning && snapshot.isPaused;
  const widgetIsRunning = snapshot.isRunning && !snapshot.isPaused;
  const widgetActionLabel = widgetIsRunning ? 'Поставить на паузу' : widgetIsPaused ? 'Продолжить' : 'Запустить';
  const widgetAction = widgetIsRunning ? 'pause' : 'play';
  const widgetBgTranslucentClass = widgetBackgroundTransparency > 0 && widgetTheme !== 'transparent' ? 'widget-bg-translucent' : '';

  if (widgetMode) {
    return (
      <div
        className={`app-shell widget-mode ${statusClass} ${timerFontClass(timerFont)} widget-theme-${widgetTheme} widget-shape-${widgetShape} ${widgetBgTranslucentClass}`}
        style={{ ...widgetColorStyle, ...widgetTransparencyStyle, ...shellFontStyle }}
      >
        <div className="widget-stack" ref={widgetDurationRef}>
          <div
            className={`widget-chrome${widgetPlacement === 'free' ? ' widget-draggable' : ''}`}
            style={widgetPlacement === 'free' ? { '--wails-draggable': 'drag' } as React.CSSProperties : undefined}
          >
          <div className="widget-controls" aria-label="Управление таймером">
            <button
              className={`widget-primary widget-action-${widgetAction}`}
              onClick={widgetIsRunning ? () => Pause() : handleStart}
              aria-label={widgetActionLabel}
              title={widgetActionLabel}
            >
              {icon(widgetAction)}
            </button>
            <button className="widget-secondary widget-secondary-compact" onClick={handleNextSpeaker} disabled={!['talk', 'talkOvertime', 'questions', 'questionsOvertime'].includes(snapshot.phase)} aria-label="Следующий докладчик" title="Следующий докладчик">
              {icon('next')}
            </button>
            <button className="widget-secondary widget-secondary-compact" onClick={handleGoToQuestions} disabled={snapshot.phase !== 'talk' && snapshot.phase !== 'talkOvertime'} aria-label="Перейти к вопросам" title="К вопросам">
              {icon('questions')}
            </button>
          </div>
          <div className="widget-body" ref={widgetBodyRef}>
            <span className="widget-timer" ref={widgetTimerRef} aria-label={`${phaseLabels[snapshot.phase]}: ${displayTime}`}>{displayTime}</span>
            <div className="widget-duration-control">
              <button
                className="widget-duration-trigger"
                type="button"
                onClick={() => void toggleWidgetDuration()}
                disabled={widgetIsRunning}
                aria-expanded={widgetDurationOpen}
                aria-controls="quick-time-panel"
                aria-label={`Время следующего докладчика: ${widgetDurationMinutes} минут`}
              >
                <span className="widget-duration-mark" aria-hidden="true" />
              </button>
            </div>
          </div>
          <button
            className="icon-button quiet widget-restore"
            onClick={handleExitWidget}
            aria-label="Развернуть таймер"
            title="Развернуть"
          >
            {icon('restore')}
          </button>
          </div>
          {widgetDurationOpen && (
            <section id="quick-time-panel" className="quick-time-panel" aria-label="Время следующего докладчика">
              <header className="quick-time-header">
                <span className="quick-time-title">{icon('clock')} Следующий докладчик</span>
                <span className="quick-time-hints">Enter — применить&nbsp;&nbsp; Esc — закрыть</span>
              </header>
              <div className="quick-time-presets" role="group" aria-label="Быстрый выбор времени">
                {[5, 10, 15, 20].map((minutes) => (
                  <button
                    key={minutes}
                    type="button"
                    className={`quick-time-preset${widgetDurationMinutes === minutes ? ' is-selected' : ''}`}
                    aria-pressed={widgetDurationMinutes === minutes}
                    onClick={() => void applyWidgetDuration(minutes)}
                  >
                    <strong>{minutes}</strong><span>мин</span>
                  </button>
                ))}
                {!widgetDurationCustomOpen ? (
                  <button type="button" className={`quick-time-preset quick-time-custom-trigger${![5, 10, 15, 20].includes(widgetDurationMinutes) ? ' is-selected' : ''}`} onClick={() => setWidgetDurationCustomOpen(true)}>
                    {![5, 10, 15, 20].includes(widgetDurationMinutes) ? (
                      <><strong>{widgetDurationMinutes}</strong><span>мин</span></>
                    ) : (
                      <span>Своё время</span>
                    )}
                  </button>
                ) : (
                  <form className="quick-time-preset quick-time-custom" onSubmit={(event) => { event.preventDefault(); void applyWidgetDuration(); }}>
                    <input
                      ref={widgetDurationInputRef}
                      id="widget-duration-minutes"
                      type="text"
                      inputMode="numeric"
                      pattern="[0-9]*"
                      value={widgetDurationDraft}
                      aria-invalid={widgetDurationInvalid}
                      onChange={(event) => {
                        const next = event.target.value.replace(/\D/g, '').slice(0, 3);
                        setWidgetDurationDraft(next);
                        const parsed = Number(next);
                        const valid = next === '' || (Number.isInteger(parsed) && parsed >= MIN_WIDGET_DURATION && parsed <= MAX_WIDGET_DURATION);
                        setWidgetDurationInvalid(next !== '' && !valid);
                      }}
                      autoFocus
                    />
                    <button type="submit" className="quick-time-confirm" aria-label="Применить время">
                      {icon('check')}
                    </button>
                    {widgetDurationInvalid && <span className="quick-time-error">{MIN_WIDGET_DURATION}–{MAX_WIDGET_DURATION}</span>}
                  </form>
                )}
              </div>
            </section>
          )}
        </div>
      </div>
    );
  }

  return (
    <div
      className={`app-shell timer-display-${timerDisplayMode} ${statusClass} ${timerFontClass(timerFont)} app-theme-${appTheme}${sessionPanelOpen ? ' has-session-panel' : ''}`}
      style={{ ...timerScaleStyle, ...shellFontStyle, ...widgetColorStyle }}
    >
      <header className="topbar">
        <div className="topbar-left">
          <button
            className="icon-button quiet widget-mode-toggle"
            aria-label="Перейти в режим виджета"
            title="Режим виджета"
            onClick={handleEnterWidget}
          >
            {icon('widget')}
          </button>
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
          <button className="icon-button quiet" aria-label="Открыть настройки" title="Настройки" onClick={() => { setSettingsTab('timer'); setSettingsOpen(true); }}>
            {icon('settings')}
          </button>
        </div>
      </header>

      <main className="timer-stage">
        <div className="timer-viewport" ref={timerViewportRef}>
          <section
            ref={timerRingRef}
            className={`timer-ring ${statusClass}`}
            aria-label={`${phaseLabels[snapshot.phase]}: ${displayTime}`}
          >
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
            <div className="timer-content" ref={timerContentRef}>
              <span className="phase-name">{phaseLabels[snapshot.phase]}</span>
              <span className="timer-value" ref={timerValueRef}>{displayTime}</span>
              <span className="timer-units"><span>мин</span><i aria-hidden="true" /><span>сек</span></span>
              {snapshot.phase.includes('Overtime') && (
                <span className="timer-caption">Сигнал через {formatClock(snapshot.nextReminderIn)}</span>
              )}
            </div>
          </section>
        </div>

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
        <button
          className={`conference-badge conference-${conferenceState.phase}`}
          onClick={() => setConnectionPromptOpen(true)}
          title="Настроить подключение к ВКС"
        >
          <span className="connection-dot" />
          <span>{conferencePhaseLabels[conferenceState.phase]}</span>
        </button>
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
            <label className="settings-checkbox conference-camera-toggle">
              <input
                type="checkbox"
                checked={conferenceCameraEnabled}
                disabled={conferenceBusy}
                onChange={(event) => handleConferenceCameraToggle(event.target.checked)}
              />
              <span>Показывать отсчёт в камере</span>
            </label>
            
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

            <div className="settings-tabs" role="tablist" aria-label="Разделы настроек">
              {settingsTabs.map(([tab, label]) => (
                <button
                  key={tab}
                  id={`settings-tab-${tab}`}
                  type="button"
                  role="tab"
                  aria-selected={settingsTab === tab}
                  aria-controls={`settings-panel-${tab}`}
                  tabIndex={settingsTab === tab ? 0 : -1}
                  className={settingsTab === tab ? 'is-selected' : ''}
                  onClick={() => setSettingsTab(tab)}
                  onKeyDown={(event) => handleSettingsTabKeyDown(event, tab)}
                >{label}</button>
              ))}
            </div>

            {settingsLocked && settingsTab !== 'interface' && <SettingsLockBanner message={settingsLockMessage} />}

            <div className="settings-section" id="settings-panel-interface" role="tabpanel" aria-labelledby="settings-tab-interface" hidden={settingsTab !== 'interface'}>
              <div className="section-heading">
                <h3 id="settings-interface-title">Интерфейс</h3>
                <div className="theme-scheme-toggle" role="group" aria-label="Тема оформления">
                  <button
                    type="button"
                    className={appTheme === 'light' ? 'is-selected' : ''}
                    aria-pressed={appTheme === 'light'}
                    aria-label="Светлая тема"
                    title="Светлая тема"
                    onClick={async () => {
                      if (appTheme === 'light') return;
                      setAppTheme('light');
                      await persistSettings({ appTheme: 'light' });
                    }}
                  >
                    {icon('sun')}
                  </button>
                  <button
                    type="button"
                    className={appTheme === 'dark' ? 'is-selected' : ''}
                    aria-pressed={appTheme === 'dark'}
                    aria-label="Тёмная тема"
                    title="Тёмная тема"
                    onClick={async () => {
                      if (appTheme === 'dark') return;
                      setAppTheme('dark');
                      await persistSettings({ appTheme: 'dark' });
                    }}
                  >
                    {icon('moon')}
                  </button>
                </div>
              </div>
              <label>
                Вид таймера
                <select value={timerDisplayMode} onChange={async (event) => { const next = event.target.value as TimerDisplayMode; setTimerDisplayMode(next); await persistSettings({ timerDisplayMode: next }); }}>
                  <option value="ring">Круговой</option>
                  <option value="digital">Цифровые часы</option>
                </select>
              </label>
              <label>
                Шрифт цифр
                <select value={timerFont} onChange={async (event) => { const next = event.target.value; setTimerFont(next); await persistSettings({ timerFont: next }); }}>
                  {TIMER_FONT_OPTIONS.map((option) => (
                    <option key={option.id} value={option.id}>{option.label}</option>
                  ))}
                </select>
              </label>
              <label>
                Размер таймера
                <div className="digit-size-row">
                  <input
                    type="range"
                    min={MIN_TIMER_SCALE}
                    max={MAX_TIMER_SCALE}
                    step={5}
                    value={timerScalePercent}
                    onChange={(event) => setTimerScalePercent(Number(event.target.value))}
                    onMouseUp={() => persistSettings()}
                    onTouchEnd={() => persistSettings()}
                  />
                  <span className="digit-size-value">{timerScalePercent}%</span>
                </div>
              </label>
              <fieldset className="widget-option-group widget-color-group">
                <legend className="widget-color-legend-row">
                  <span>Цвета цифр</span>
                  <button
                    type="button"
                    className="text-button secondary compact-button widget-color-reset"
                    disabled={!hasCustomWidgetColors(widgetColors)}
                    onClick={async () => {
                      setWidgetColors(EMPTY_WIDGET_COLORS);
                      setWidgetColorPreviewKey(null);
                      await persistSettings({
                        widgetColorIdle: '',
                        widgetColorRunning: '',
                        widgetColorPaused: '',
                        widgetColorOvertime: '',
                      });
                    }}
                  >
                    Сброс
                  </button>
                </legend>
                <div className="widget-color-grid">
                  {([
                    ['widgetColorIdle', 'Ожидание', '#8ec5ff'],
                    ['widgetColorRunning', 'Доклад', '#69e0b0'],
                    ['widgetColorPaused', 'Пауза', '#ffd271'],
                    ['widgetColorOvertime', 'Просрочка', '#ff8794'],
                  ] as const).map(([key, label, fallback]) => (
                    <label
                      key={key}
                      className={`widget-color-field${widgetColorPreviewKey === key ? ' is-active' : ''}`}
                    >
                      <span>{label}</span>
                      <input
                        type="color"
                        value={widgetColors[key] || fallback}
                        onFocus={() => setWidgetColorPreviewKey(key)}
                        onChange={async (event) => {
                          setWidgetColorPreviewKey(key);
                          const next = { ...widgetColors, [key]: event.target.value };
                          setWidgetColors(next);
                          await persistSettings({ [key]: event.target.value });
                        }}
                      />
                    </label>
                  ))}
                </div>
              </fieldset>

              <div className={`widget-preview-stage preview-${widgetPlacement}`}>
                <div
                  className={`widget-preview ${widgetPreviewStatusClass} ${timerFontClass(timerFont)} widget-theme-${widgetTheme} widget-shape-${widgetShape} ${widgetBgTranslucentClass}`}
                  style={{ ...widgetColorStyle, ...widgetTransparencyStyle, ...shellFontStyle }}
                  aria-label="Предпросмотр виджета"
                >
                  <div className="widget-chrome">
                    <div className="widget-controls preview-controls" aria-hidden="true">
                      <span className={`widget-primary widget-action-${widgetAction} preview-control`}>{icon(widgetAction)}</span>
                      <span className="widget-secondary widget-secondary-compact preview-control">{icon('next')}</span>
                      <span className="widget-secondary widget-secondary-compact preview-control">{icon('questions')}</span>
                    </div>
                    <div className="widget-body" ref={widgetPreviewBodyRef}><span className="widget-timer" ref={widgetPreviewTimerRef}>{displayTime}</span></div>
                    <span className="widget-restore preview-restore" aria-hidden="true">{icon('restore')}</span>
                  </div>
                </div>
              </div>

              <fieldset className="widget-option-group">
                <legend>Положение</legend>
                <div className="widget-choice-grid placement-choices">
                  {([
                    ['topLeft', 'Слева'],
                    ['topCenter', 'По центру'],
                    ['topRight', 'Справа'],
                    ['free', 'Свободно'],
                  ] as const).map(([value, label]) => (
                    <button
                      key={value}
                      type="button"
                      className={widgetPlacement === value ? 'is-selected' : ''}
                      aria-pressed={widgetPlacement === value}
                      onClick={async () => { setWidgetPlacement(value); await persistSettings({ widgetPlacement: value }); }}
                    >
                      <span className={`placement-glyph placement-${value}`} aria-hidden="true"><i /></span>
                      <span>{label}</span>
                    </button>
                  ))}
                </div>
              </fieldset>

              <fieldset className="widget-option-group">
                <legend>Цветовая схема виджета</legend>
                <div className="widget-choice-grid theme-choices">
                  {WIDGET_THEME_OPTIONS.map(([value, label]) => (
                    <button
                      key={value}
                      type="button"
                      className={widgetTheme === value ? 'is-selected' : ''}
                      aria-pressed={widgetTheme === value}
                      onClick={async () => { setWidgetTheme(value); await persistSettings({ widgetTheme: value }); }}
                    >
                      <span className={`theme-swatch theme-${value}`} aria-hidden="true" />
                      <span>{label}</span>
                    </button>
                  ))}
                </div>
              </fieldset>

              <label className="widget-transparency-control">
                <span className="widget-transparency-label"><span>Прозрачность фона</span><output>{widgetTransparencyDisplay}%</output></span>
                <span className="widget-transparency-scale" aria-hidden="true"><span>Матовое</span><span>Прозрачное</span></span>
                <input
                  type="range"
                  min={MIN_WIDGET_BACKGROUND_TRANSPARENCY}
                  max={MAX_WIDGET_BACKGROUND_TRANSPARENCY}
                  step="1"
                  value={widgetTransparencyDisplay}
                  disabled={widgetTransparencyDisabled}
                  aria-label="Прозрачность фона"
                  onChange={async (event) => {
                    if (widgetTheme === 'transparent') return;
                    const next = Number(event.target.value);
                    setWidgetBackgroundTransparency(next);
                    await persistSettings({ widgetBackgroundTransparency: next });
                  }}
                />
              </label>

              <fieldset className="widget-option-group">
                <legend>Форма</legend>
                <div className="widget-choice-grid shape-choices">
                  {([
                    ['rectangular', 'Прямоугольная'],
                    ['rounded', 'Скруглённая'],
                  ] as const).map(([value, label]) => (
                    <button
                      key={value}
                      type="button"
                      className={widgetShape === value ? 'is-selected' : ''}
                      aria-pressed={widgetShape === value}
                      onClick={async () => { setWidgetShape(value); await persistSettings({ widgetShape: value }); }}
                    >
                      <span className={`shape-swatch shape-${value}`} aria-hidden="true" />
                      <span>{label}</span>
                    </button>
                  ))}
                </div>
              </fieldset>
              <p className="settings-hint">В свободном режиме виджет можно перетаскивать и изменять его размер за края окна.</p>
            </div>

            <div className="settings-section" id="settings-panel-timer" role="tabpanel" aria-labelledby="settings-tab-timer" hidden={settingsTab !== 'timer'}>
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

            <div className="settings-section" id="settings-panel-sound" role="tabpanel" aria-labelledby="settings-tab-sound" hidden={settingsTab !== 'sound'}>
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
