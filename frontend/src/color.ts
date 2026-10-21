import type { Color } from './types'

export function hsvToRgb(hue: number, saturation: number, value = 1): Color {
  const h = ((hue % 360) + 360) % 360
  const c = value * saturation
  const x = c * (1 - Math.abs(((h / 60) % 2) - 1))
  const m = value - c
  let rgb: [number, number, number]
  if (h < 60) rgb = [c, x, 0]
  else if (h < 120) rgb = [x, c, 0]
  else if (h < 180) rgb = [0, c, x]
  else if (h < 240) rgb = [0, x, c]
  else if (h < 300) rgb = [x, 0, c]
  else rgb = [c, 0, x]
  return { r: Math.round((rgb[0] + m) * 255), g: Math.round((rgb[1] + m) * 255), b: Math.round((rgb[2] + m) * 255) }
}

export function rgbToHsv(color: Color): { h: number; s: number; v: number } {
  const r = color.r / 255, g = color.g / 255, b = color.b / 255
  const max = Math.max(r, g, b), min = Math.min(r, g, b), delta = max - min
  let h = 0
  if (delta) {
    if (max === r) h = 60 * (((g - b) / delta) % 6)
    else if (max === g) h = 60 * ((b - r) / delta + 2)
    else h = 60 * ((r - g) / delta + 4)
  }
  if (h < 0) h += 360
  return { h, s: max === 0 ? 0 : delta / max, v: max }
}

export function hexOf(color: Color): string {
  return `#${[color.r, color.g, color.b].map((n) => n.toString(16).padStart(2, '0')).join('').toUpperCase()}`
}

export function parseHex(input: string): Color | null {
  const value = input.trim().replace(/^#/, '')
  if (!/^[0-9a-fA-F]{6}$/.test(value)) return null
  return { r: parseInt(value.slice(0, 2), 16), g: parseInt(value.slice(2, 4), 16), b: parseInt(value.slice(4, 6), 16) }
}
