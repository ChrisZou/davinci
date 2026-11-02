/**
 * Envelope warps for text (稿定设计's 变形), as the panel offers them. The
 * renderer draws them (render/renderer.ts); the server validates the keys.
 *
 * A warp is a shape, a strength (−100…100; negative flips it) and a relative
 * height (−100…100): where the height change is anchored — −100 keeps the
 * bottom edge straight, 100 the top, 0 shrinks toward the middle. Wedge shapes
 * (trapezoid / taperLeft / taperRight) default to −100 when bias is unset.
 *
 * Keys:
 *   bulge      — capsule / drum (convex top+bottom)
 *   flag       — wave / S-curve top+bottom
 *   pinch      — classic short-top / long-bottom trapezoid
 *   trapezoid  — right-end narrow (左高右低); kept for saved documents; bias defaults −100
 *   taperRight — alias of trapezoid (same math)
 *   taperLeft  — left-end narrow (右高左低); bias defaults −100
 * Shear (skewY) sits in the panel among the shapes but is not a warp key.
 */

export interface WarpShape {
  key: string
  label: string
  /** The icon: an outline path in a 40×24 box. */
  icon: string
}

export const WARPS: WarpShape[] = [
  // Capsule: vertical sides, bowed top+bottom.
  { key: 'bulge', label: '鼓形', icon: 'M8 7 Q20 2 32 7 L32 17 Q20 22 8 17 Z' },
  // Flag / wave: S-curve top and bottom, constant height.
  { key: 'flag', label: '旗帜', icon: 'M4 8 C12 3 16 3 20 8 C24 13 28 13 36 8 L36 16 C28 21 24 21 20 16 C16 11 12 11 4 16 Z' },
  // Classic isosceles trapezoid: short top, long bottom.
  { key: 'pinch', label: '梯形', icon: 'M10 6 L30 6 L36 18 L4 18 Z' },
  // Legacy / right-narrow wedge (左高). Bottom flat, only top slopes.
  { key: 'trapezoid', label: '左高', icon: 'M5 4 L35 14 L35 20 L5 20 Z' },
  // Mirror wedge (右高): left short / right tall, bottom flat.
  { key: 'taperLeft', label: '右高', icon: 'M5 14 L35 4 L35 20 L5 20 Z' },
]

/**
 * 斜切 sits among the shapes in the panel, though it is a property of its
 * own (skewY, the right end lifted): one or the other, as on 稿定.
 */
export const SHEAR: WarpShape = { key: 'shear', label: '斜切', icon: 'M5 10 L35 4 L35 14 L5 20 Z' }
export const DEFAULT_SHEAR = 8

/**
 * Panel tile order (after the leading「无」): matches the 稿定-style reference
 * grid — bulge, shear, flag, pinch, left wedge, right wedge.
 */
export const WARP_PANEL: WarpShape[] = [
  WARPS[0], // bulge
  SHEAR,
  WARPS[1], // flag
  WARPS[2], // pinch
  WARPS[3], // trapezoid (左高)
  WARPS[4], // taperLeft (右高)
]

/** API aliases accepted by the server / renderer but not shown as extra tiles. */
export const WARP_ALIASES: Record<string, string> = {
  taperRight: 'trapezoid',
}


/** Wedge / taper keys: bottom stays flat when bias is unset (default −100). */
export const WEDGE_WARPS = new Set(['trapezoid', 'taperLeft', 'taperRight'])

/** Default relative-height for a warp key. Wedges keep the bottom edge straight. */
export function defaultWarpBias(key: unknown): number {
  const k = String(key ?? '')
  const resolved = WARP_ALIASES[k] ?? k
  return resolved === 'trapezoid' || resolved === 'taperLeft' ? -100 : 0
}

const BY_KEY = new Map(WARPS.map((w) => [w.key, w]))

export function warpShape(key: unknown): WarpShape | undefined {
  const k = String(key ?? '')
  const resolved = WARP_ALIASES[k] ?? k
  return BY_KEY.get(resolved)
}
