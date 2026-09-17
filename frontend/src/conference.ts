export type ConferencePhase =
  | 'idle'
  | 'opening'
  | 'connecting'
  | 'waitingAdmission'
  | 'joined'
  | 'playing'
  | 'left'
  | 'error';

export interface ConferenceState {
  phase: ConferencePhase;
  platform: string;
  displayUrl: string;
  message: string;
  tested: boolean;
  browserVisible: boolean;
  cameraEnabled: boolean;
  updatedAt: number;
}

export type ConferenceWizardStep = 1 | 2 | 3;

export type ConferenceAction = 'idle' | 'validating' | 'confirming' | 'disconnecting';

export type ConferenceUiState =
  | 'idle'
  | 'validating'
  | 'connecting'
  | 'waiting_for_user'
  | 'testing_audio'
  | 'connected'
  | 'error'
  | 'disconnecting';

export interface RecentConference {
  url: string;
  title: string;
  host: string;
}

export const initialConferenceState: ConferenceState = {
  phase: 'idle',
  platform: '',
  displayUrl: '',
  message: 'Участник не подключён',
  tested: false,
  browserVisible: false,
  cameraEnabled: false,
  updatedAt: 0,
};

export const conferencePhaseLabels: Record<ConferencePhase, string> = {
  idle: 'Не подключён',
  opening: 'Открытие браузера',
  connecting: 'Подключение к встрече',
  waitingAdmission: 'Ожидает допуска',
  joined: 'ВКС подключена',
  playing: 'ВКС подключена',
  left: 'Отключён',
  error: 'Ошибка',
};

const RECENT_KEY = 'presentation-timer.conference.recent';
const MAX_RECENT = 5;

export function isConferenceConnecting(phase: ConferencePhase): boolean {
  return phase === 'opening' || phase === 'connecting' || phase === 'waitingAdmission';
}

export function isConferenceJoined(phase: ConferencePhase): boolean {
  return phase === 'joined' || phase === 'playing';
}

export function isConferenceActive(phase: ConferencePhase): boolean {
  return isConferenceConnecting(phase) || isConferenceJoined(phase);
}

export function conferenceUiState(
  state: ConferenceState,
  action: ConferenceAction,
  testingAudio: boolean,
): ConferenceUiState {
  if (action === 'disconnecting') return 'disconnecting';
  if (action === 'validating') return 'validating';
  if (testingAudio || state.phase === 'playing') return 'testing_audio';
  if (state.phase === 'waitingAdmission') return 'waiting_for_user';
  if (state.phase === 'opening' || state.phase === 'connecting' || action === 'confirming') return 'connecting';
  if (state.phase === 'error') return 'error';
  if (isConferenceJoined(state.phase)) return 'connected';
  return 'idle';
}

/** Full HTTPS URL for history: keeps query/hash, rejects userinfo (same as backend Resolve). */
export function normalizeConferenceHistoryUrl(raw: string): { url: string; host: string } | null {
  try {
    const parsed = new URL(raw.trim());
    if (parsed.protocol !== 'https:' || !parsed.hostname) return null;
    if (parsed.username || parsed.password) return null;
    return {
      url: parsed.toString(),
      host: parsed.hostname,
    };
  } catch {
    return null;
  }
}

export function loadRecentConferences(): RecentConference[] {
  try {
    const raw = window.localStorage.getItem(RECENT_KEY);
    if (!raw) return [];
    const parsed = JSON.parse(raw) as unknown;
    if (!Array.isArray(parsed)) return [];
    return parsed
      .map((item) => {
        if (!item || typeof item !== 'object') return null;
        const record = item as Record<string, unknown>;
        const normalized = typeof record.url === 'string' ? normalizeConferenceHistoryUrl(record.url) : null;
        if (!normalized) return null;
        return {
          url: normalized.url,
          host: normalized.host,
          title: typeof record.title === 'string' && record.title.trim() ? record.title.trim() : normalized.host,
        };
      })
      .filter((item): item is RecentConference => item !== null)
      .slice(0, MAX_RECENT);
  } catch {
    return [];
  }
}

export function rememberConferenceConnection(rawUrl: string, title: string): RecentConference[] {
  const normalized = normalizeConferenceHistoryUrl(rawUrl);
  if (!normalized) return loadRecentConferences();
  const next: RecentConference = {
    url: normalized.url,
    host: normalized.host,
    title: title.trim() || normalized.host,
  };
  const rest = loadRecentConferences().filter((item) => item.url !== next.url);
  const list = [next, ...rest].slice(0, MAX_RECENT);
  try {
    window.localStorage.setItem(RECENT_KEY, JSON.stringify(list));
  } catch {
    // private mode / quota — keep in-memory only
  }
  return list;
}

export function wizardStepForOpen(phase: ConferencePhase): ConferenceWizardStep {
  return isConferenceJoined(phase) ? 3 : 1;
}
