import type { TextStyle, ShapeStyle } from '../types'

/**
 * Cover-style text presets — the combinations 创哥 reaches for constantly
 * (yellow box, red box, outlined, shadowed). They are plain data so the same
 * list can be handed to AI: `applyTextPreset{id,preset:"yellow-box"}`.
 */
export interface TextPreset {
  key: string
  name: string
  style: TextStyle & ShapeStyle
}

export const textPresets: TextPreset[] = [
  {
    key: 'plain-white',
    name: '白色无衬线',
    style: { fill: '#ffffff', fontSize: 120, fontWeight: 700, lineHeight: 1.25 },
  },
  {
    key: 'yellow-box',
    name: '黄底黑字',
    style: {
      fill: '#111111',
      textBackgroundColor: '#ffd500',
      fontSize: 128,
      fontWeight: 800,
      padding: 24,
      lineHeight: 1.3,
    },
  },
  {
    key: 'red-box',
    name: '红底白字',
    style: {
      fill: '#ffffff',
      textBackgroundColor: '#e5322d',
      fontSize: 120,
      fontWeight: 700,
      padding: 24,
      lineHeight: 1.3,
    },
  },
  {
    key: 'outline-black',
    name: '黑描边',
    style: {
      fill: '#ffffff',
      stroke: '#000000',
      strokeWidth: 8,
      paintFirst: true,
      fontSize: 132,
      fontWeight: 900,
      lineHeight: 1.2,
    },
  },
  {
    key: 'soft-shadow',
    name: '柔和投影',
    style: {
      fill: '#ffffff',
      shadow: 'rgba(0,0,0,.55):24:4:6',
      fontSize: 128,
      fontWeight: 700,
      lineHeight: 1.25,
    },
  },
  {
    key: 'subtitle',
    name: '小字副标题',
    style: {
      fill: 'rgba(255,255,255,.92)',
      fontSize: 44,
      fontWeight: 500,
      charSpacing: 4,
      lineHeight: 1.4,
    },
  },
  {
    key: 'marker',
    name: '荧光笔',
    style: {
      fill: '#1a1a1a',
      textBackgroundColor: '#c6f135',
      fontSize: 104,
      fontWeight: 700,
      padding: 16,
      lineHeight: 1.35,
    },
  },
]

export function textPreset(key: string): TextPreset | undefined {
  return textPresets.find((p) => p.key === key)
}
