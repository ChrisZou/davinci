/**
 * Envelope warps for text (稿定设计's 变形), as the panel offers them. The
 * renderer draws them (render/renderer.ts); the server validates the keys.
 *
 * A warp is a shape, a strength (−100…100; negative flips it) and a relative
 * height (−100…100): where the height change is anchored — −100 keeps the
 * bottom edge straight, 100 the top, 0 shrinks toward the middle.
 */

export interface WarpShape {
  key: string
  label: string
  /** The icon: an outline path in a 40×24 box. */
  icon: string
}

export const WARPS: WarpShape[] = [{ key: 'trapezoid', label: '梯形', icon: 'M5 4 L35 9 L35 15 L5 20 Z' }]

/**
 * 斜切 sits among the shapes in the panel, though it is a property of its
 * own (skewY, the right end lifted): one or the other, as on 稿定.
 */
export const SHEAR: WarpShape = { key: 'shear', label: '斜切', icon: 'M5 10 L35 4 L35 14 L5 20 Z' }
export const DEFAULT_SHEAR = 8

const BY_KEY = new Map(WARPS.map((w) => [w.key, w]))

export function warpShape(key: unknown): WarpShape | undefined {
  return BY_KEY.get(String(key ?? ''))
}
