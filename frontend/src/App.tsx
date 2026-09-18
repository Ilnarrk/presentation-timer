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
import { ConferenceWizard } from './ConferenceWizard';
import {
  conferencePhaseLabels,
  initialConferenceState,
  isConferenceActive,
  isConferenceConnecting,
  isConferenceJoined,
  loadRecentConferences,
  normalizeConferenceHistoryUrl,
  rememberConferenceConnection,
  wizardStepForOpen,
  type ConferenceAction,
  type ConferenceState,
  type ConferenceWizardStep,
  type RecentConference,
} from './conference';
import { DEFAULT_TIMER_FONT_ID, TIMER_FONT_OPTIONS, timerFontClass, timerFontStyle } from './timerFonts';
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

const phaseLabels: Record<Phase, string> = {
  idle: 'Ожидание',
  talk: 'Доклад',
  talkOvertime: 'Доклад — просрочка',
  questions: 'Обсуждение',
  questionsOvertime: 'Обсуждение — просрочка',
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

function getWidgetStatusLabel(phase: Phase, durationOpen: boolean, isTicking: boolean): string {
  if (durationOpen) {
    return 'Регламент';
  }
  if (phase === 'questions' || phase === 'questionsOvertime') {
    return 'Обсуждение';
  }
  if (isTicking && (phase === 'talk' || phase === 'talkOvertime')) {
    return 'Доклад';
  }
  return 'Регламент';
}

const MAX_WIDGET_DURATION = 180;

type DurationPreset = { minutes: number; seconds: number };

const DEFAULT_WIDGET_QUICK_PRESETS: DurationPreset[] = [
  { minutes: 5, seconds: 0 },
  { minutes: 10, seconds: 0 },
  { minutes: 15, seconds: 0 },
  { minutes: 20, seconds: 0 },
];

function durationPresetTotal(preset: DurationPreset): number {
  return preset.minutes * 60 + preset.seconds;
}

function normalizeDurationPreset(preset: DurationPreset): DurationPreset {
  let minutes = Math.max(0, Math.min(MAX_WIDGET_DURATION, Math.trunc(preset.minutes) || 0));
  let seconds = Math.max(0, Math.min(59, Math.trunc(preset.seconds) || 0));
  if (minutes === MAX_WIDGET_DURATION) {
    seconds = 0;
  }
  if (minutes === 0 && seconds === 0) {
    seconds = 1;
  }
  return { minutes, seconds };
}

function normalizeWidgetQuickPresets(value: unknown): DurationPreset[] {
  const source = Array.isArray(value) ? value : [];
  return DEFAULT_WIDGET_QUICK_PRESETS.map((fallback, index) => {
    const item = source[index] as DurationPreset | undefined;
    if (!item) {
      return fallback;
    }
    return normalizeDurationPreset({
      minutes: Number(item.minutes) || 0,
      seconds: Number(item.seconds) || 0,
    });
  });
}

function parseDurationInput(raw: string): DurationPreset | null {
  const value = raw.trim();
  if (value === '' || value.endsWith(':')) {
    return null;
  }
  if (value.includes(':')) {
    const parts = value.split(':');
    if (parts.length !== 2) {
      return null;
    }
    const minutes = Number(parts[0]);
    const seconds = Number(parts[1]);
    if (!Number.isInteger(minutes) || !Number.isInteger(seconds)) {
      return null;
    }
    return { minutes, seconds };
  }
  const minutes = Number(value);
  if (!Number.isInteger(minutes)) {
    return null;
  }
  return { minutes, seconds: 0 };
}

function isValidTalkDuration(preset: DurationPreset): boolean {
  const total = durationPresetTotal(preset);
  return preset.minutes >= 0 &&
    preset.minutes <= MAX_WIDGET_DURATION &&
    preset.seconds >= 0 &&
    preset.seconds <= 59 &&
    total >= 1 &&
    total <= MAX_WIDGET_DURATION * 60;
}

function formatDurationDraft(totalSeconds: number): string {
  const minutes = Math.floor(Math.max(0, totalSeconds) / 60);
  const seconds = Math.max(0, totalSeconds) % 60;
  if (seconds === 0) {
    return String(minutes);
  }
  return `${minutes}:${String(seconds).padStart(2, '0')}`;
}

function sanitizeDurationDraft(raw: string): string {
  let colonUsed = false;
  let out = '';
  for (const ch of raw) {
    if (ch >= '0' && ch <= '9') {
      out += ch;
    } else if (ch === ':' && !colonUsed) {
      out += ':';
      colonUsed = true;
    }
  }
  return out.slice(0, 6);
}

function formatPresetButton(preset: DurationPreset): { primary: string; secondary: string } {
  if (preset.seconds === 0) {
    return { primary: String(preset.minutes), secondary: 'мин' };
  }
  if (preset.minutes === 0) {
    return { primary: String(preset.seconds), secondary: 'сек' };
  }
  return { primary: `${preset.minutes}:${String(preset.seconds).padStart(2, '0')}`, secondary: '' };
}

const MAX_SPEAKERS = 50;
const MIN_TIMER_SCALE = 80;
const MAX_TIMER_SCALE = 140;
const DEFAULT_TIMER_SCALE = 115;
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

function formatAppError(err: unknown): string {
  const raw = String(err).replace(/^Error:\s*/i, '');
  switch (raw) {
    case 'duration must be greater than zero':
      return 'Длительность должна быть больше нуля';
    case 'invalid phase transition':
      return 'Недопустимое действие для текущей фазы';
    default:
      return raw;
  }
}

function PanelError({ message }: { message: string }) {
  return <div className="panel-error" role="alert">{message}</div>;
}

function useTimedMessage(message: string, clear: () => void, delayMs = 5000) {
  useEffect(() => {
    if (!message) return undefined;
    const timeout = window.setTimeout(clear, delayMs);
    return () => window.clearTimeout(timeout);
  }, [message, clear, delayMs]);
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
  const [soundId, setSoundId] = useState('');
  const [reminderSoundId, setReminderSoundId] = useState('');
  const [questionsSoundId, setQuestionsSoundId] = useState('');
  const [nextSoundId, setNextSoundId] = useState('');
  const [deviceId, setDeviceId] = useState('default');
  const [volume, setVolume] = useState(0.85);
  const [muteConferenceSound, setMuteConferenceSound] = useState(false);
  const [muteConferenceReceive, setMuteConferenceReceive] = useState(true);
  const [conferenceCameraEnabled, setConferenceCameraEnabled] = useState(true);
  const [devices, setDevices] = useState<AudioDevice[]>([]);
  const [sounds, setSounds] = useState<SoundOption[]>([]);
  const [conferenceUrl, setConferenceUrl] = useState('');
  const [conferenceName, setConferenceName] = useState('Таймер');
  const [conferenceState, setConferenceState] = useState<ConferenceState>(initialConferenceState);
  const [conferenceBusy, setConferenceBusy] = useState(false);
  const [settingsError, setSettingsError] = useState('');
  const [sessionError, setSessionError] = useState('');
  const [conferenceError, setConferenceError] = useState('');
  const [widgetDurationError, setWidgetDurationError] = useState('');
  const [saving, setSaving] = useState(false);
  const [importingSound, setImportingSound] = useState(false);
  const [previewingSoundId, setPreviewingSoundId] = useState('');
  const previewingRef = useRef(false);
  const [settingsOpen, setSettingsOpen] = useState(false);
  const [settingsTab, setSettingsTab] = useState<SettingsTab>('timer');
  const [aboutOpen, setAboutOpen] = useState(false);
  const [connectionPromptOpen, setConnectionPromptOpen] = useState(false);
  const [conferenceWizardStep, setConferenceWizardStep] = useState<ConferenceWizardStep>(1);
  const [conferenceTesting, setConferenceTesting] = useState(false);
  const [recentConferences, setRecentConferences] = useState<RecentConference[]>([]);
  const [conferenceAction, setConferenceAction] = useState<ConferenceAction>('idle');
  const connectionPromptOpenRef = useRef(connectionPromptOpen);
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
  const [timerFont, setTimerFont] = useState<TimerFont>(DEFAULT_TIMER_FONT_ID);
  const [widgetPlacement, setWidgetPlacement] = useState<WidgetPlacement>('free');
  const [appTheme, setAppTheme] = useState<AppTheme>('dark');
  const [widgetTheme, setWidgetTheme] = useState<WidgetTheme>('transparent');
  const [widgetShape, setWidgetShape] = useState<WidgetShape>('rounded');
  const [widgetQuickPresets, setWidgetQuickPresets] = useState<DurationPreset[]>(DEFAULT_WIDGET_QUICK_PRESETS);
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
  const conferenceActive = isConferenceActive(conferenceState.phase);
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
      widgetQuickPresets: next?.widgetQuickPresets ?? widgetQuickPresets,
    });

    setSaving(true);
    try {
      await SaveSettings(payload);
      const saved = settings.Settings.createFrom(await GetSettings());
      setMuteConferenceSound(saved.muteConferenceSound ?? false);
      setMuteConferenceReceive(saved.muteConferenceReceive ?? true);
      setConferenceCameraEnabled(saved.conferenceCameraEnabled ?? true);
      setTimerScalePercent(saved.timerScalePercent || DEFAULT_TIMER_SCALE);
      setTimerDisplayMode((saved.timerDisplayMode as TimerDisplayMode) || 'ring');
      setTimerFont((saved.timerFont as TimerFont) || DEFAULT_TIMER_FONT_ID);
      setWidgetPlacement((saved.widgetPlacement as WidgetPlacement) || 'free');
      setAppTheme((saved.appTheme as AppTheme) || 'dark');
      setWidgetTheme((saved.widgetTheme as WidgetTheme) || 'transparent');
      setWidgetShape((saved.widgetShape as WidgetShape) || 'rounded');
      setWidgetBackgroundTransparency(Math.min(MAX_WIDGET_BACKGROUND_TRANSPARENCY, Math.max(MIN_WIDGET_BACKGROUND_TRANSPARENCY, saved.widgetBackgroundTransparency ?? 0)));
      setWidgetColors({
        widgetColorIdle: saved.widgetColorIdle ?? '',
        widgetColorRunning: saved.widgetColorRunning ?? '',
        widgetColorPaused: saved.widgetColorPaused ?? '',
        widgetColorOvertime: saved.widgetColorOvertime ?? '',
      });
      setWidgetQuickPresets(normalizeWidgetQuickPresets(saved.widgetQuickPresets));
      setVolume(saved.volume);
      setDeviceId(saved.deviceId);
      setSettingsError('');
    } catch (err) {
      const message = formatAppError(err);
      setSettingsError(message);
      if (message.includes('Длительность')) {
        setSettingsTab('timer');
      }
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
    widgetQuickPresets,
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
      const availableSounds = initialSounds as SoundOption[];
      const soundIds = new Set(availableSounds.map((sound) => sound.id));
      const resolvedSoundId = initialSettings.soundId && soundIds.has(initialSettings.soundId)
        ? initialSettings.soundId
        : (availableSounds.find((sound) => sound.id.endsWith('alert.mp3') || sound.label === 'alert')?.id ?? availableSounds[0]?.id ?? '');
      setSoundId(resolvedSoundId);
      setReminderSoundId(initialSettings.reminderSoundId && soundIds.has(initialSettings.reminderSoundId) ? initialSettings.reminderSoundId : '');
      setQuestionsSoundId(initialSettings.questionsSoundId && soundIds.has(initialSettings.questionsSoundId) ? initialSettings.questionsSoundId : '');
      setNextSoundId(initialSettings.nextSoundId && soundIds.has(initialSettings.nextSoundId) ? initialSettings.nextSoundId : '');
      setDeviceId(initialSettings.deviceId);
      setVolume(initialSettings.volume);
      setMuteConferenceSound(initialSettings.muteConferenceSound ?? false);
      setMuteConferenceReceive(initialSettings.muteConferenceReceive ?? true);
      setConferenceCameraEnabled(initialSettings.conferenceCameraEnabled ?? true);
      setTimerScalePercent(initialSettings.timerScalePercent || DEFAULT_TIMER_SCALE);
      setTimerDisplayMode((initialSettings.timerDisplayMode as TimerDisplayMode) || 'ring');
      setTimerFont((initialSettings.timerFont as TimerFont) || DEFAULT_TIMER_FONT_ID);
      setWidgetPlacement((initialSettings.widgetPlacement as WidgetPlacement) || 'free');
      setAppTheme((initialSettings.appTheme as AppTheme) || 'dark');
      setWidgetTheme((initialSettings.widgetTheme as WidgetTheme) || 'transparent');
      setWidgetShape((initialSettings.widgetShape as WidgetShape) || 'rounded');
      setWidgetBackgroundTransparency(Math.min(MAX_WIDGET_BACKGROUND_TRANSPARENCY, Math.max(MIN_WIDGET_BACKGROUND_TRANSPARENCY, initialSettings.widgetBackgroundTransparency ?? 0)));
      setWidgetColors({
        widgetColorIdle: initialSettings.widgetColorIdle ?? '',
        widgetColorRunning: initialSettings.widgetColorRunning ?? '',
        widgetColorPaused: initialSettings.widgetColorPaused ?? '',
        widgetColorOvertime: initialSettings.widgetColorOvertime ?? '',
      });
      setWidgetQuickPresets(normalizeWidgetQuickPresets(initialSettings.widgetQuickPresets));
      setWidgetMode(await IsWidgetMode());
      setSounds(initialSounds as SoundOption[]);
      setDevices(initialDevices as AudioDevice[]);
      const conference = initialConference as ConferenceState;
      setConferenceState(conference);
      setRecentConferences(loadRecentConferences());
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
      if (!isConferenceActive(conference.phase)) {
        setConferenceWizardStep(1);
        setConnectionPromptOpen(true);
      } else {
        setConferenceWizardStep(wizardStepForOpen(conference.phase));
      }
    };

    bootstrap().catch((err) => setSettingsError(formatAppError(err)));
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
    connectionPromptOpenRef.current = connectionPromptOpen;
  }, [connectionPromptOpen]);

  useEffect(() => {
    const unsubscribeError = EventsOn('audio:error', (message: string) => {
      if (message.toLowerCase().includes('context canceled')) return;
      if (connectionPromptOpenRef.current) {
        setConferenceError(formatAppError(message));
        return;
      }
      setSettingsError(formatAppError(message));
      setSettingsTab('sound');
      setSettingsOpen(true);
    });
    return () => unsubscribeError();
  }, []);

  const clearSettingsError = useCallback(() => setSettingsError(''), []);
  const clearSessionError = useCallback(() => setSessionError(''), []);
  const clearConferenceError = useCallback(() => setConferenceError(''), []);
  const clearWidgetDurationError = useCallback(() => setWidgetDurationError(''), []);
  useTimedMessage(settingsError, clearSettingsError);
  useTimedMessage(sessionError, clearSessionError);
  useTimedMessage(connectionPromptOpen ? '' : conferenceError, clearConferenceError);
  useTimedMessage(widgetDurationError, clearWidgetDurationError);

  useEffect(() => {
    if (!settingsOpen) setSettingsError('');
  }, [settingsOpen]);

  useEffect(() => {
    if (!sessionPanelOpen && !templateModalOpen) setSessionError('');
  }, [sessionPanelOpen, templateModalOpen]);

  useEffect(() => {
    if (!connectionPromptOpen) setConferenceError('');
  }, [connectionPromptOpen]);

  const openConferenceWizard = useCallback(() => {
    if (isConferenceJoined(conferenceState.phase)) {
      setConferenceWizardStep(3);
    } else {
      setConferenceWizardStep(1);
    }
    setConnectionPromptOpen(true);
  }, [conferenceState.phase]);

  useEffect(() => {
    if (!connectionPromptOpen) return;
    if (isConferenceJoined(conferenceState.phase) && conferenceWizardStep === 1) {
      setConferenceWizardStep(2);
    }
    if (!isConferenceJoined(conferenceState.phase) && !isConferenceConnecting(conferenceState.phase) && conferenceWizardStep !== 1) {
      setConferenceWizardStep(1);
    }
  }, [conferenceState.phase, connectionPromptOpen, conferenceWizardStep]);

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

  const widgetStatusLabel = useMemo(
    () => getWidgetStatusLabel(
      snapshot.phase,
      widgetDurationOpen,
      snapshot.isRunning && !snapshot.isPaused,
    ),
    [snapshot.phase, snapshot.isRunning, snapshot.isPaused, widgetDurationOpen],
  );

  const widgetDurationSeconds = snapshot.talkSeconds;
  const widgetPresetSelected = widgetQuickPresets.some((preset) => durationPresetTotal(preset) === widgetDurationSeconds);
  const closeWidgetDuration = useCallback(() => {
    setWidgetDurationOpen(false);
    setWidgetDurationCustomOpen(false);
    setWidgetDurationInvalid(false);
    setWidgetDurationError('');
    void SetWidgetQuickTimeOpen(false);
  }, []);

  const handleStart = async () => {
    closeWidgetDuration();
    try {
      await persistSettings();
      await Start();
    } catch {
      // Ошибки настроек показываются в панели настроек.
    }
  };

  const handleReset = () => {
    Reset();
  };

  const handleGoToQuestions = async () => {
    closeWidgetDuration();
    try {
      await GoToQuestions();
    } catch {
      // Недопустимые действия таймера не показываем на главном экране.
    }
  };

  const handleNextSpeaker = async () => {
    closeWidgetDuration();
    try {
      await NextSpeaker();
    } catch {
      // Недопустимые действия таймера не показываем на главном экране.
    }
  };

  const openWidgetDuration = async () => {
    setWidgetDurationDraft(formatDurationDraft(widgetDurationSeconds));
    setWidgetDurationCustomOpen(false);
    setWidgetDurationInvalid(false);
    setWidgetDurationError('');
    try {
      await SetWidgetQuickTimeOpen(true);
      setWidgetDurationOpen(true);
    } catch (err) {
      setWidgetDurationError(formatAppError(err));
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

  const applyWidgetDuration = async (value: string | number | DurationPreset = widgetDurationDraft, keepOpen = false) => {
    let parsed: DurationPreset | null = null;
    if (typeof value === 'number') {
      parsed = { minutes: value, seconds: 0 };
    } else if (typeof value === 'object') {
      parsed = value;
    } else {
      parsed = parseDurationInput(value);
    }
    if (!parsed || !isValidTalkDuration(parsed)) {
      setWidgetDurationInvalid(true);
      return;
    }
    try {
      await SetTalkDurationOverride(parsed.minutes, parsed.seconds);
      setTalkMinutes(parsed.minutes);
      setTalkSecondsPart(parsed.seconds);
      setWidgetDurationDraft(formatDurationDraft(durationPresetTotal(parsed)));
      setWidgetDurationInvalid(false);
      await persistSettings({ talkMinutes: parsed.minutes, talkSeconds: parsed.seconds });
      if (!keepOpen) closeWidgetDuration();
      setWidgetDurationError('');
    } catch (err) {
      setWidgetDurationError(formatAppError(err));
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
      setSessionError('');
    } catch (err) {
      setSessionError(formatAppError(err));
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
      setSessionError('');
    } catch (err) {
      setSessionError(formatAppError(err));
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
      setSessionError('');
    } catch (err) {
      setSessionError(formatAppError(err));
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
    setSessionError('');
    return true;
  };

  const handleSaveSessionTemplate = async () => {
    setSessionBusy(true);
    try {
      const entry = templates.Entry.createFrom(await SaveSessionTemplate(sessionTemplate()));
      setSuccessMessage(`Шаблон «${entry.name}» сохранён`);
      setSessionError('');
    } catch (err) {
      setSessionError(formatAppError(err));
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
      setSessionError('');
    } catch (err) {
      setSessionError(formatAppError(err));
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
      setSessionError(formatAppError(err));
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
      setSessionError('');
    } catch (err) {
      setSessionError(formatAppError(err));
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
      setSettingsError('');
    } catch (err) {
      setSettingsError(formatAppError(err));
      setSettingsTab('sound');
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
      setSettingsError('');
    } catch (err) {
      setSettingsError(formatAppError(err));
      setSettingsTab('sound');
    } finally {
      setImportingSound(false);
    }
  };

  const handleConferenceConnect = async () => {
    setConferenceAction('validating');
    const normalized = normalizeConferenceHistoryUrl(conferenceUrl);
    if (!normalized) {
      setConferenceError('Проверьте ссылку и попробуйте снова');
      setConferenceAction('idle');
      return;
    }
    setConferenceBusy(true);
    try {
      const rawUrl = conferenceUrl.trim();
      const state = await ConnectConference(rawUrl, conferenceName.trim());
      setConferenceState(state as ConferenceState);
      setConferenceWizardStep(1);
      setConnectionPromptOpen(true);
      setConferenceError('');
      setRecentConferences(rememberConferenceConnection(
        rawUrl,
        (state as ConferenceState).platform || conferenceName.trim(),
      ));
    } catch (err) {
      setConferenceWizardStep(1);
      setConnectionPromptOpen(true);
      setConferenceError(formatAppError(err));
    } finally {
      setConferenceAction('idle');
      setConferenceBusy(false);
    }
  };

  const handleConferenceDisconnect = async () => {
    setConferenceAction('disconnecting');
    setConferenceBusy(true);
    try {
      await DisconnectConference();
      const state = await GetConferenceState();
      setConferenceState(state as ConferenceState);
      setConferenceWizardStep(1);
      setConferenceError('');
    } catch (err) {
      setConferenceError(formatAppError(err));
    } finally {
      setConferenceAction('idle');
      setConferenceBusy(false);
    }
  };

  const handleConferenceConfirm = async () => {
    setConferenceAction('confirming');
    setConferenceBusy(true);
    try {
      await ConfirmConferenceJoined();
      const state = await GetConferenceState();
      setConferenceState(state as ConferenceState);
      setConferenceError('');
    } catch (err) {
      const state = await GetConferenceState();
      setConferenceState(state as ConferenceState);
      if (!(state.phase === 'error' && state.message)) {
        setConferenceError(formatAppError(err));
      }
    } finally {
      setConferenceAction('idle');
      setConferenceBusy(false);
    }
  };

  const handleConferenceTest = async () => {
    if (conferenceBusy || conferenceTesting) return;
    setConferenceBusy(true);
    setConferenceTesting(true);
    try {
      await TestConferenceSound(soundId);
      const state = await GetConferenceState();
      setConferenceState(state as ConferenceState);
      setConferenceError('');
    } catch (err) {
      setConferenceError(formatAppError(err));
    } finally {
      setConferenceTesting(false);
      setConferenceBusy(false);
    }
  };

  const handleConferenceBrowserToggle = async () => {
    setConferenceBusy(true);
    try {
      const state = await SetConferenceBrowserVisible(!conferenceState.browserVisible);
      setConferenceState(state as ConferenceState);
      setConferenceError('');
    } catch (err) {
      setConferenceError(formatAppError(err));
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
      setConferenceError('');
    } catch (err) {
      setConferenceError(formatAppError(err));
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
    } catch {
      // Ошибки режима виджета не показываем на главном экране.
    }
  };

  const handleExitWidget = async () => {
    try {
      await ExitWidgetMode();
      setWidgetMode(false);
    } catch {
      // Ошибки режима виджета не показываем на главном экране.
    }
  };

  const handleConferenceDiagnostics = async () => {
    setConferenceBusy(true);
    try {
      const snapshot = await GetConferenceDiagnostics();
      await ClipboardSetText(snapshot);
      setConferenceError('');
      setConferenceState({
        ...conferenceState,
        message: 'Диагностика скопирована в буфер обмена',
      });
    } catch (err) {
      setConferenceError(formatAppError(err));
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
    hasOvertime: timerHasOvertime,
  });
  const { bodyRef: widgetPreviewBodyRef, timerRef: widgetPreviewTimerRef } = useWidgetTimerFit({
    fontId: timerFont,
    active: settingsOpen && settingsTab === 'interface',
    hasOvertime: timerHasOvertime,
  });

  const icon = (name: 'play' | 'playOutline' | 'pause' | 'questions' | 'next' | 'reset' | 'disconnect' | 'upload' | 'settings' | 'close' | 'browserShow' | 'browserHide' | 'eye' | 'eyeOff' | 'queue' | 'trash' | 'widget' | 'restore' | 'clock' | 'edit' | 'check' | 'sun' | 'moon') => {
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
      eye: <><path d="M2.5 12s3.5-6.5 9.5-6.5S21.5 12 21.5 12s-3.5 6.5-9.5 6.5S2.5 12 2.5 12Z" /><circle cx="12" cy="12" r="2.5" /></>,
      eyeOff: <><path d="M10.6 10.6a2.5 2.5 0 0 0 3.5 3.5" /><path d="M7.2 7.8C5.4 9.1 3.8 11 2.5 12c1.3 1 2.9 2.9 4.7 4.2" /><path d="M16.8 16.2c1.8-1.3 3.4-3.2 4.7-4.2-1.3-1-2.9-2.9-4.7-4.2" /><path d="m9.5 5.5 5 13" /></>,
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
            <span className="widget-phase">{widgetStatusLabel}</span>
            <span className="widget-timer" ref={widgetTimerRef} aria-label={`${widgetStatusLabel}: ${displayTime}`}>{displayTime}</span>
            <div className="widget-duration-control">
              <button
                className="widget-duration-trigger"
                type="button"
                onClick={() => void toggleWidgetDuration()}
                disabled={widgetIsRunning}
                aria-expanded={widgetDurationOpen}
                aria-controls="quick-time-panel"
                aria-label={`Время следующего докладчика: ${formatClock(widgetDurationSeconds)}`}
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
                <span className="quick-time-title">{icon('clock')} Регламент</span>
                <span className="quick-time-hints">Enter — применить&nbsp;&nbsp; Esc — закрыть</span>
              </header>
              {widgetDurationError && <p className="quick-time-panel-error" role="alert">{widgetDurationError}</p>}
              <div className="quick-time-presets" role="group" aria-label="Быстрый выбор времени">
                {widgetQuickPresets.map((preset, index) => {
                  const label = formatPresetButton(preset);
                  const selected = durationPresetTotal(preset) === widgetDurationSeconds;
                  return (
                    <button
                      key={`${preset.minutes}-${preset.seconds}-${index}`}
                      type="button"
                      className={`quick-time-preset${selected ? ' is-selected' : ''}`}
                      aria-pressed={selected}
                      onClick={() => void applyWidgetDuration(preset)}
                    >
                      <strong>{label.primary}</strong>{label.secondary ? <span>{label.secondary}</span> : null}
                    </button>
                  );
                })}
                {!widgetDurationCustomOpen ? (
                  <button type="button" className={`quick-time-preset quick-time-custom-trigger${!widgetPresetSelected ? ' is-selected' : ''}`} onClick={() => setWidgetDurationCustomOpen(true)}>
                    {!widgetPresetSelected ? (
                      <><strong>{formatDurationDraft(widgetDurationSeconds)}</strong><span>{widgetDurationSeconds % 60 === 0 ? 'мин' : ''}</span></>
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
                      inputMode="decimal"
                      value={widgetDurationDraft}
                      aria-invalid={widgetDurationInvalid}
                      placeholder="0:59"
                      onChange={(event) => {
                        const next = sanitizeDurationDraft(event.target.value);
                        setWidgetDurationDraft(next);
                        if (next === '' || next.endsWith(':')) {
                          setWidgetDurationInvalid(false);
                          return;
                        }
                        const parsed = parseDurationInput(next);
                        setWidgetDurationInvalid(!parsed || !isValidTalkDuration(parsed));
                      }}
                      autoFocus
                    />
                    <button type="submit" className="quick-time-confirm" aria-label="Применить время">
                      {icon('check')}
                    </button>
                    {widgetDurationInvalid && <span className="quick-time-error">0:01–{MAX_WIDGET_DURATION}:00</span>}
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
            <>
              <span className="topbar-left-divider" aria-hidden="true" />
              <button
                className={`icon-button quiet conference-browser-toggle${conferenceState.browserVisible ? ' is-visible' : ''}`}
                disabled={conferenceBusy}
                onClick={handleConferenceBrowserToggle}
                aria-label={conferenceState.browserVisible ? 'Скрыть окно браузера ВКС' : 'Показать окно браузера ВКС'}
                title={conferenceState.browserVisible ? 'Скрыть окно ВКС' : 'Показать окно ВКС'}
              >
                {icon(conferenceState.browserVisible ? 'eyeOff' : 'eye')}
              </button>
            </>
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
                    Доклад {formatClock(snapshot.talkSeconds)} · Обсуждение {formatClock(snapshot.questionsSeconds)}
                  </p>
                )}
              </div>
              <button className="icon-button quiet" aria-label="Закрыть панель сессии" onClick={() => setSessionPanelOpen(false)}>
                {icon('close')}
              </button>
            </div>

            {sessionError && <PanelError message={sessionError} />}

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
                        <span>Обсуждение — как в настройках</span>
                      </label>
                      {sessionUseDefaultQuestions ? (
                        <p className="settings-hint">из настроек: {formatClock(questionsMinutes * 60 + questionsSecondsPart)}</p>
                      ) : (
                        <label>Обсуждение<div className="duration-inputs">
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
          onClick={openConferenceWizard}
          title="Настроить подключение к ВКС"
        >
          <span className="connection-dot" />
          <span>{conferencePhaseLabels[conferenceState.phase]}</span>
        </button>
      </footer>

      {connectionPromptOpen && (
        <ConferenceWizard
          step={conferenceWizardStep}
          state={conferenceState}
          conferenceUrl={conferenceUrl}
          conferenceName={conferenceName}
          cameraEnabled={conferenceCameraEnabled}
          busy={conferenceBusy}
          error={conferenceError}
          recent={recentConferences}
          testing={conferenceTesting}
          action={conferenceAction}
          diagnostics={import.meta.env.DEV}
          onUrlChange={(value) => {
            setConferenceUrl(value);
            if (conferenceError) setConferenceError('');
          }}
          onNameChange={(value) => {
            setConferenceName(value);
            if (conferenceError) setConferenceError('');
          }}
          onSelectRecent={(value) => {
            setConferenceUrl(value);
            setConferenceError('');
          }}
          onCameraToggle={handleConferenceCameraToggle}
          onConnect={handleConferenceConnect}
          onSkip={() => setConnectionPromptOpen(false)}
          onCancelConnect={handleConferenceDisconnect}
          onManualConfirm={handleConferenceConfirm}
          onRetry={handleConferenceConnect}
          onEditDetails={handleConferenceDisconnect}
          onTestSound={handleConferenceTest}
          onDisconnect={handleConferenceDisconnect}
          onNext={() => setConferenceWizardStep(3)}
          onBackToCheck={() => setConferenceWizardStep(2)}
          onDone={() => setConnectionPromptOpen(false)}
          onDiagnostics={handleConferenceDiagnostics}
        />
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

            {settingsError && <PanelError message={settingsError} />}

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
                    <div className="widget-body" ref={widgetPreviewBodyRef}>
                      <span className="widget-phase">{widgetStatusLabel}</span>
                      <span className="widget-timer" ref={widgetPreviewTimerRef}>{displayTime}</span>
                    </div>
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
              <label>Обсуждение<div className="duration-inputs">
                <NumericInput max={60} value={questionsMinutes} disabled={settingsLocked} onChange={setQuestionsMinutes} onBlur={() => persistSettings()} /><span>мин</span>
                <NumericInput max={59} value={questionsSecondsPart} disabled={settingsLocked} onChange={setQuestionsSecondsPart} onBlur={() => persistSettings()} /><span>сек</span>
              </div></label>
              <label>Повтор сигнала при просрочке<div className="duration-inputs">
                <NumericInput max={60} value={reminderMinutes} disabled={settingsLocked} onChange={setReminderMinutes} onBlur={() => persistSettings()} /><span>мин</span>
                <NumericInput max={59} value={reminderSecondsPart} disabled={settingsLocked} onChange={setReminderSecondsPart} onBlur={() => persistSettings()} /><span>сек</span>
              </div></label>
              <h3 className="settings-subheading">Быстрый выбор в виджете</h3>
              <p className="settings-hint">Четыре кнопки в меню виджета. Можно задать минуты и секунды.</p>
              {widgetQuickPresets.map((preset, index) => (
                <label key={index}>Вариант {index + 1}<div className="duration-inputs">
                  <NumericInput
                    max={180}
                    value={preset.minutes}
                    disabled={settingsLocked}
                    onChange={(minutes) => {
                      const next = widgetQuickPresets.map((item, itemIndex) => itemIndex === index ? normalizeDurationPreset({ ...item, minutes }) : item);
                      setWidgetQuickPresets(next);
                    }}
                    onBlur={() => persistSettings()}
                  /><span>мин</span>
                  <NumericInput
                    max={59}
                    value={preset.seconds}
                    disabled={settingsLocked}
                    onChange={(seconds) => {
                      const next = widgetQuickPresets.map((item, itemIndex) => itemIndex === index ? normalizeDurationPreset({ ...item, seconds }) : item);
                      setWidgetQuickPresets(next);
                    }}
                    onBlur={() => persistSettings()}
                  /><span>сек</span>
                </div></label>
              ))}
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
            {sessionError && <PanelError message={sessionError} />}
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
