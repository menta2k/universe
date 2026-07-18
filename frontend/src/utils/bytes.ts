/** Human-readable byte formatting (binary units: KiB, MiB, ...). */

const UNITS = ['B', 'KiB', 'MiB', 'GiB', 'TiB', 'PiB'] as const

/**
 * Format a byte count as a short human-readable string, e.g. 1536 -> "1.5 KiB".
 * Negative or non-finite inputs are coerced to 0.
 */
export function formatBytes(bytes: number): string {
  const value = Number.isFinite(bytes) && bytes > 0 ? bytes : 0
  if (value < 1024) return `${value} B`

  let size = value
  let unit = 0
  while (size >= 1024 && unit < UNITS.length - 1) {
    size /= 1024
    unit += 1
  }
  const rounded = size >= 100 ? Math.round(size) : Math.round(size * 10) / 10
  return `${rounded} ${UNITS[unit]}`
}
