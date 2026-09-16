const MIN_FONT_PX = 24;
const WORST_CASE_NORMAL = '88:88';
const WORST_CASE_OVERTIME = '+88:88';
const WIDGET_WORST_CASE = '+88:88';

export function getWorstCaseTimeText(hasOvertime: boolean): string {
  return hasOvertime ? WORST_CASE_OVERTIME : WORST_CASE_NORMAL;
}

export function getWidgetWorstCaseTimeText(): string {
  return WIDGET_WORST_CASE;
}

type FitTextToBoundsOptions = {
  element: HTMLElement;
  container: HTMLElement;
  sampleText: string;
  maxWidth: number;
  maxHeight: number;
  targetPx: number;
  cssVarName: string;
};

function elementFits(element: HTMLElement, maxWidth: number, maxHeight: number) {
  return element.scrollWidth <= maxWidth + 1 && element.scrollHeight <= maxHeight + 1;
}

export function fitTextToBounds({
  element,
  container,
  sampleText,
  maxWidth,
  maxHeight,
  targetPx,
  cssVarName,
}: FitTextToBoundsOptions): number {
  container.style.removeProperty(cssVarName);
  element.style.removeProperty('font-size');

  const originalText = element.textContent ?? '';
  element.textContent = sampleText;

  const maxPx = Math.max(MIN_FONT_PX, Math.floor(targetPx));
  let lo = MIN_FONT_PX;
  let hi = maxPx;
  let best = MIN_FONT_PX;

  while (lo <= hi) {
    const mid = Math.floor((lo + hi) / 2);
    element.style.fontSize = `${mid}px`;
    if (elementFits(element, maxWidth, maxHeight)) {
      best = mid;
      lo = mid + 1;
    } else {
      hi = mid - 1;
    }
  }

  element.textContent = originalText;
  element.style.removeProperty('font-size');
  container.style.setProperty(cssVarName, `${best}px`);
  return best;
}

export function measureSiblingHeight(content: HTMLElement, value: HTMLElement): number {
  let labelsHeight = 0;
  for (const child of content.children) {
    if (child === value) {
      continue;
    }
    labelsHeight += (child as HTMLElement).offsetHeight;
  }
  const style = getComputedStyle(content);
  const gap = Number.parseFloat(style.rowGap || style.gap || '0') || 0;
  const childCount = content.children.length;
  if (childCount > 1) {
    labelsHeight += gap * (childCount - 1);
  }
  return labelsHeight;
}

export const TIMER_FIT = {
  MIN_FONT_PX,
  RING_CONTENT_WIDTH_RATIO: 0.76,
  ringContentHeightRatio(hasCaption: boolean) {
    return hasCaption ? 0.58 : 0.52;
  },
  DIGITAL_WIDTH_RATIO: 0.96,
  DIGITAL_HEIGHT_RATIO: 0.82,
  VALUE_CSS_VAR: '--timer-value-fit-size',
  WIDGET_CSS_VAR: '--widget-timer-fit-size',
} as const;
