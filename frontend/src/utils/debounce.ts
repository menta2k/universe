/** Minimal debounce helper (trailing edge) with a cancel handle. */

export interface Debounced<Args extends readonly unknown[]> {
  (...args: Args): void
  cancel(): void
}

export function debounce<Args extends readonly unknown[]>(
  fn: (...args: Args) => void,
  waitMs: number,
): Debounced<Args> {
  let timer: ReturnType<typeof setTimeout> | undefined

  const wrapped = (...args: Args): void => {
    clearTimeout(timer)
    timer = setTimeout(() => fn(...args), waitMs)
  }
  wrapped.cancel = (): void => {
    clearTimeout(timer)
  }
  return wrapped
}
