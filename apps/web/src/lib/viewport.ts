/** Helpers for boxes that must sit exactly on the screen on iOS.
 *
 * A `position: fixed` box is laid out against the browser's layout viewport,
 * not against what is visible. On iOS the two drift apart: the soft keyboard
 * shrinks the visible area without moving the layout viewport, and locking a
 * scrolled page snaps the content to the top while the layout viewport stays
 * where it was scrolled to. Either way a box glued to `bottom: 0` ends up off
 * the screen. `window.visualViewport` reports the visible rectangle in layout
 * viewport coordinates, which is exactly the correction such a box needs. */

/** Keeps a `position: fixed; inset: 0` element covering the visible viewport:
 * its height follows `visualViewport.height` and it is shifted by
 * `visualViewport.offsetTop`, so its bottom edge is the bottom of the screen
 * (or the top of the keyboard) rather than the bottom of the layout viewport.
 * `onChange` is told about every update with the viewport it was sized to.
 * Returns a cleanup that stops following and clears the inline styles. Where
 * `visualViewport` is unavailable the element is left alone and the cleanup is
 * a no-op. */
export function followVisualViewport(
  el: HTMLElement,
  onChange?: (viewport: VisualViewport) => void,
): () => void {
  const vv = window.visualViewport
  if (!vv) return () => {}
  const apply = () => {
    el.style.height = `${vv.height}px`
    el.style.transform = `translateY(${vv.offsetTop}px)`
    onChange?.(vv)
  }
  apply()
  vv.addEventListener('resize', apply)
  vv.addEventListener('scroll', apply)
  return () => {
    vv.removeEventListener('resize', apply)
    vv.removeEventListener('scroll', apply)
    el.style.height = ''
    el.style.transform = ''
  }
}

/** Reports whether a scroll gesture that started on `target` should be left to
 * the browser: true when some element from `target` up to and including
 * `within` can scroll vertically on its own, false when the gesture would only
 * reach the page behind. */
export function scrollsWithin(target: EventTarget | null, within: HTMLElement): boolean {
  let el = target instanceof Element ? target : null
  while (el) {
    if (el instanceof HTMLElement && el.scrollHeight > el.clientHeight) {
      const { overflowY } = getComputedStyle(el)
      if (overflowY === 'auto' || overflowY === 'scroll') return true
    }
    if (el === within) return false
    el = el.parentElement
  }
  return false
}

/** Stops touch and wheel gestures on `overlay` from scrolling the page behind
 * it, while letting `panel` (and anything scrollable inside it) scroll as
 * usual. This replaces `overflow: hidden` on the body, which on iOS resets the
 * page's scroll position and unmoors every fixed box on it. Returns a cleanup
 * that removes the listeners. */
export function holdPageStill(overlay: HTMLElement, panel: HTMLElement): () => void {
  const guard = (e: Event) => {
    if (!scrollsWithin(e.target, panel)) e.preventDefault()
  }
  overlay.addEventListener('touchmove', guard, { passive: false })
  overlay.addEventListener('wheel', guard, { passive: false })
  return () => {
    overlay.removeEventListener('touchmove', guard)
    overlay.removeEventListener('wheel', guard)
  }
}
