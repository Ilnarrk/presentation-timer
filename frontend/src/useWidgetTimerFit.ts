import { type RefObject, useLayoutEffect, useRef } from 'react';
import { TIMER_FIT, fitTextToBounds, getWorstCaseTimeText } from './timerFit';

type UseWidgetTimerFitOptions = {
  fontId: string;
  active: boolean;
  hasOvertime?: boolean;
};

type WidgetTimerFitRefs = {
  bodyRef: RefObject<HTMLDivElement | null>;
  timerRef: RefObject<HTMLSpanElement | null>;
};

export function useWidgetTimerFit({
  fontId,
  active,
  hasOvertime = false,
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

    const sampleText = getWorstCaseTimeText(hasOvertime);

    const measure = () => {
      body.style.removeProperty(TIMER_FIT.WIDGET_CSS_VAR);
      timer.style.removeProperty('font-size');

      const slot = timer.getBoundingClientRect();
      if (slot.width <= 0 || slot.height <= 0) {
        return;
      }

      const maxWidth = slot.width;
      const maxHeight = slot.height;
      const targetPx = maxHeight;

      fitTextToBounds({
        element: timer,
        container: body,
        sampleText,
        maxWidth,
        maxHeight,
        targetPx,
        cssVarName: TIMER_FIT.WIDGET_CSS_VAR,
        widthSafety: 1,
        intrinsic: true,
      });
    };

    const observer = new ResizeObserver(measure);
    observer.observe(body);
    measure();
    document.fonts?.ready.then(measure).catch(() => undefined);

    return () => observer.disconnect();
  }, [fontId, active, hasOvertime]);

  return { bodyRef, timerRef };
}
