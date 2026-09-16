import { type RefObject, useLayoutEffect, useRef } from 'react';
import { TIMER_FIT, fitTextToBounds, getWidgetWorstCaseTimeText } from './timerFit';

type UseWidgetTimerFitOptions = {
  fontId: string;
  active: boolean;
};

type WidgetTimerFitRefs = {
  bodyRef: RefObject<HTMLDivElement | null>;
  timerRef: RefObject<HTMLSpanElement | null>;
};

export function useWidgetTimerFit({
  fontId,
  active,
}: UseWidgetTimerFitOptions): WidgetTimerFitRefs {
  const bodyRef = useRef<HTMLDivElement>(null);
  const timerRef = useRef<HTMLSpanElement>(null);

  useLayoutEffect(() => {
    if (!active) {
      return undefined;
    }

    const body = bodyRef.current;
    const timer = timerRef.current;
    if (!body || !timer) {
      return undefined;
    }

    const sampleText = getWidgetWorstCaseTimeText();

    const measure = () => {
      body.style.removeProperty(TIMER_FIT.WIDGET_CSS_VAR);
      timer.style.removeProperty('font-size');

      const bodyRect = body.getBoundingClientRect();
      if (bodyRect.width <= 0 || bodyRect.height <= 0) {
        return;
      }

      const style = getComputedStyle(body);
      const paddingX = Number.parseFloat(style.paddingLeft) + Number.parseFloat(style.paddingRight);
      const paddingY = Number.parseFloat(style.paddingTop) + Number.parseFloat(style.paddingBottom);
      const maxWidth = Math.max(TIMER_FIT.MIN_FONT_PX, bodyRect.width - paddingX);
      const maxHeight = Math.max(TIMER_FIT.MIN_FONT_PX, bodyRect.height - paddingY);
      const targetPx = Math.min(maxHeight * 0.88, maxWidth / Math.max(sampleText.length * 0.52, 4));

      fitTextToBounds({
        element: timer,
        container: body,
        sampleText,
        maxWidth,
        maxHeight,
        targetPx,
        cssVarName: TIMER_FIT.WIDGET_CSS_VAR,
      });
    };

    const observer = new ResizeObserver(measure);
    observer.observe(body);
    measure();
    document.fonts?.ready.then(measure).catch(() => undefined);

    return () => observer.disconnect();
  }, [fontId, active]);

  return { bodyRef, timerRef };
}
