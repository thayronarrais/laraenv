/**
 * How far a wheel event should move the tab strip horizontally.
 *
 * The strip overflows on one axis only, and Chromium will not translate a
 * wheel's deltaY into horizontal scrolling for it — only deltaX scrolls it,
 * which is what a trackpad swipe or shift+wheel produces. A plain mouse wheel
 * therefore did nothing and the strip had to be dragged by hand.
 *
 * deltaX is deliberately left alone: the browser is already scrolling the
 * strip by that amount, so adding it here would move the strip twice as far as
 * the gesture did.
 */
export function wheelScrollDelta(
  wheel: { deltaX: number; deltaY: number },
  strip: { scrollWidth: number; clientWidth: number },
): number {
  if (strip.scrollWidth <= strip.clientWidth) return 0;
  if (Math.abs(wheel.deltaY) <= Math.abs(wheel.deltaX)) return 0;
  return wheel.deltaY;
}
