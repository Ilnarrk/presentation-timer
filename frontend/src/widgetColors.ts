import type { CSSProperties } from 'react';

export type WidgetColorKey = keyof WidgetColorSettings;

export type WidgetColorSettings = {
  widgetColorIdle: string;
  widgetColorRunning: string;
  widgetColorPaused: string;
  widgetColorOvertime: string;
};

export const WIDGET_COLOR_PREVIEW_STATUS: Record<WidgetColorKey, string> = {
  widgetColorIdle: 'status-idle',
  widgetColorRunning: 'status-running',
  widgetColorPaused: 'status-paused',
  widgetColorOvertime: 'status-overtime',
};

export function widgetColorPreviewClass(key: WidgetColorKey | null, fallback: string): string {
  if (key) {
    return WIDGET_COLOR_PREVIEW_STATUS[key];
  }
  return fallback;
}

export const EMPTY_WIDGET_COLORS: WidgetColorSettings = {
  widgetColorIdle: '',
  widgetColorRunning: '',
  widgetColorPaused: '',
  widgetColorOvertime: '',
};

export function buildWidgetColorStyle(colors: WidgetColorSettings): CSSProperties {
  const style: Record<string, string> = {};
  if (colors.widgetColorIdle) {
    style['--widget-color-idle'] = colors.widgetColorIdle;
  }
  if (colors.widgetColorRunning) {
    style['--widget-color-running'] = colors.widgetColorRunning;
  }
  if (colors.widgetColorPaused) {
    style['--widget-color-paused'] = colors.widgetColorPaused;
  }
  if (colors.widgetColorOvertime) {
    style['--widget-color-overtime'] = colors.widgetColorOvertime;
  }
  return style as CSSProperties;
}

export function hasCustomWidgetColors(colors: WidgetColorSettings): boolean {
  return Boolean(
    colors.widgetColorIdle ||
    colors.widgetColorRunning ||
    colors.widgetColorPaused ||
    colors.widgetColorOvertime,
  );
}
