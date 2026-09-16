import { type RefObject, useLayoutEffect, useRef } from 'react';
import {
  TIMER_FIT,
  fitTextToBounds,
  getWorstCaseTimeText,
  measureSiblingHeight,
} from './timerFit';

type TimerDisplayMode = 'ring' | 'digital';

type UseTimerDisplayFitOptions = {
  active: boolean;
  mode: TimerDisplayMode;
  scalePercent: number;
  fontId: string;
  hasCaption: boolean;
  hasOvertime: boolean;
};

type TimerDisplayFitRefs = {
  viewportRef: RefObject<HTMLDivElement | null>;
  ringRef: RefObject<HTMLElement | null>;
  contentRef: RefObject<HTMLDivElement | null>;
  valueRef: RefObject<HTMLSpanElement | null>;
};

export function useTimerDisplayFit({
  active,
  mode,
  scalePercent,
  fontId,
  hasCaption,
  hasOvertime,
}: UseTimerDisplayFitOptions): TimerDisplayFitRefs {
  const viewportRef = useRef<HTMLDivElement>(null);
  const ringRef = useRef<HTMLElement>(null);
  const contentRef = useRef<HTMLDivElement>(null);
  const valueRef = useRef<HTMLSpanElement>(null);

  useLayoutEffect(() => {
    if (!active) {
      return undefined;
    }

    const viewport = viewportRef.current;
    const ring = ringRef.current;
    const content = contentRef.current;
    const value = valueRef.current;
    if (!viewport || !content || !value) {
      return undefined;
    }

    const sampleText = getWorstCaseTimeText(hasOvertime);

    const measure = () => {
      content.style.removeProperty(TIMER_FIT.VALUE_CSS_VAR);
      value.style.removeProperty('font-size');

      const viewportRect = viewport.getBoundingClientRect();
      if (viewportRect.width <= 0 || viewportRect.height <= 0) {
        return;
      }

      const labelsHeight = measureSiblingHeight(content, value);

      if (mode === 'ring') {
        const ringRect = (ring ?? content).getBoundingClientRect();
        const ringSize = Math.max(ringRect.width, ringRect.height);
        if (ringSize <= 0) {
          return;
        }

        const maxWidth = ringSize * TIMER_FIT.RING_CONTENT_WIDTH_RATIO;
        const maxHeight = ringSize * TIMER_FIT.ringContentHeightRatio(hasCaption) - labelsHeight;
        const targetPx = ringSize * 0.24 * (scalePercent / 100);
        fitTextToBounds({
          element: value,
          container: content,
          sampleText,
          maxWidth,
          maxHeight: Math.max(TIMER_FIT.MIN_FONT_PX, maxHeight),
          targetPx,
          cssVarName: TIMER_FIT.VALUE_CSS_VAR,
        });
        return;
      }

      const maxWidth = viewportRect.width * TIMER_FIT.DIGITAL_WIDTH_RATIO;
      const maxHeight = viewportRect.height * TIMER_FIT.DIGITAL_HEIGHT_RATIO - labelsHeight;
      const sampleLength = sampleText.length;
      const targetPx = Math.min(
        viewportRect.width / Math.max(sampleLength * 0.56, 4),
        viewportRect.height * 0.72,
      ) * (scalePercent / 100);

      fitTextToBounds({
        element: value,
        container: content,
        sampleText,
        maxWidth,
        maxHeight: Math.max(TIMER_FIT.MIN_FONT_PX, maxHeight),
        targetPx,
        cssVarName: TIMER_FIT.VALUE_CSS_VAR,
      });
    };

    const observer = new ResizeObserver(measure);
    observer.observe(viewport);
    if (ring) {
      observer.observe(ring);
    }
    observer.observe(content);

    measure();
    document.fonts?.ready.then(measure).catch(() => undefined);

    return () => observer.disconnect();
  }, [active, mode, scalePercent, fontId, hasCaption, hasOvertime]);

  return { viewportRef, ringRef, contentRef, valueRef };
}
