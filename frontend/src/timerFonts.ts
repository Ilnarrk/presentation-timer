import manifest from './assets/fonts/manifest.json';

export type TimerFontManifestEntry = {
  id: string;
  label: string;
  family: string;
  file: string;
  format: 'opentype' | 'truetype' | 'woff' | 'woff2';
};

const fontFiles = import.meta.glob('./assets/fonts/*.{otf,ttf,woff,woff2}', {
  eager: true,
  query: '?url',
  import: 'default',
}) as Record<string, string>;

const entries = manifest as TimerFontManifestEntry[];

const fontUrlByFile = new Map<string, string>();
for (const [path, url] of Object.entries(fontFiles)) {
  const fileName = path.split('/').pop();
  if (fileName) {
    fontUrlByFile.set(fileName, url);
  }
}

export const TIMER_FONT_OPTIONS: Array<{ id: string; label: string }> = [
  { id: 'system', label: 'Системный' },
  ...entries.map((entry) => ({ id: entry.id, label: entry.label })),
];

const entryById = new Map(entries.map((entry) => [entry.id, entry]));

export function timerFontClass(fontId: string): string {
  if (fontId === 'system') {
    return 'timer-font-system';
  }
  return entryById.has(fontId) ? `timer-font-${fontId}` : 'timer-font-system';
}

export function timerFontFamily(fontId: string): string | null {
  if (fontId === 'system') {
    return null;
  }
  return entryById.get(fontId)?.family ?? null;
}

export function timerFontStyle(fontId: string): Record<string, string> {
  const family = timerFontFamily(fontId);
  if (!family) {
    return {};
  }
  return {
    '--timer-digit-font-family': `"${family}", "Courier New", monospace`,
  };
}

let fontsLoaded = false;

export function ensureTimerFontsLoaded(): void {
  if (fontsLoaded || typeof document === 'undefined') {
    return;
  }
  const rules = entries
    .map((entry) => {
      const url = fontUrlByFile.get(entry.file);
      if (!url) {
        console.warn(`Timer font file not found in build: ${entry.file}`);
        return '';
      }
      return `@font-face{font-family:"${entry.family}";src:url("${url}") format("${entry.format}");font-display:swap;}`;
    })
    .filter(Boolean)
    .join('\n');
  if (!rules) {
    fontsLoaded = true;
    return;
  }
  const style = document.createElement('style');
  style.dataset.timerFonts = 'true';
  style.textContent = rules;
  document.head.appendChild(style);
  fontsLoaded = true;
}
