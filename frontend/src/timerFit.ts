const MIN_FONT_PX = 24;
const WIDTH_FIT_SAFETY = 0.94;
const OVERTIME_WIDTH_FACTOR = 0.96;
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
  widthSafety?: number;
  intrinsic?: boolean;
};

function elementFits(
  element: HTMLElement,
  maxWidth: number,
  maxHeight: number,
  widthSafety: number,
  intrinsic: boolean,
) {
  const safeWidth = maxWidth * widthSafety;
  if (intrinsic) {
    return element.offsetWidth <= safeWidth + 1 && element.offsetHeight <= maxHeight + 1;
  }
  return element.scrollWidth <= safeWidth + 1 && element.scrollHeight <= maxHeight + 1;
}

export function applyWidthFitLimit(maxWidth: number, hasOvertime: boolean): number {
  return hasOvertime ? maxWidth * OVERTIME_WIDTH_FACTOR : maxWidth;
}

export function fitTextToBounds({
  element,
  container,
  sampleText,
  maxWidth,
  maxHeight,
  targetPx,
  cssVarName,
  widthSafety = WIDTH_FIT_SAFETY,
  intrinsic = false,
}: FitTextToBoundsOptions): number {
  container.style.removeProperty(cssVarName);
  element.style.removeProperty('font-size');

  const originalText = element.textContent ?? '';
  const probe = intrinsic ? (element.cloneNode(false) as HTMLElement) : element;
  if (intrinsic) {
    const computed = getComputedStyle(element);
    probe.textContent = sampleText;
    probe.style.position = 'absolute';
    probe.style.left = '0';
    probe.style.top = '0';
    probe.style.visibility = 'hidden';
    probe.style.pointerEvents = 'none';
    probe.style.display = 'inline-block';
    probe.style.width = 'auto';
    probe.style.height = 'auto';
    probe.style.maxWidth = 'none';
    probe.style.flex = 'none';
    probe.style.overflow = 'hidden';
    probe.style.whiteSpace = 'nowrap';
    probe.style.lineHeight = '1';
    probe.style.fontFamily = computed.fontFamily;
    probe.style.fontWeight = computed.fontWeight;
    probe.style.letterSpacing = computed.letterSpacing;
    probe.style.fontVariantNumeric = computed.fontVariantNumeric;
    container.appendChild(probe);
  } else {
    probe.textContent = sampleText;
  }
  const maxPx = Math.max(MIN_FONT_PX, Math.floor(targetPx));
  let lo = MIN_FONT_PX;
  let hi = maxPx;
  let best = MIN_FONT_PX;

  while (lo <= hi) {
    const mid = Math.floor((lo + hi) / 2);
    probe.style.fontSize = `${mid}px`;
    if (elementFits(probe, maxWidth, maxHeight, widthSafety, intrinsic)) {
      best = mid;
      lo = mid + 1;
    } else {
      hi = mid - 1;
    }
  }

  if (intrinsic) {
    probe.remove();
  } else {
    element.textContent = originalText;
    element.style.removeProperty('font-size');
  }
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
