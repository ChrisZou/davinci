import type { CanvasPreset } from '../api'

/**
 * Canvas sizes offered when creating a project. Keys match the server's
 * `canvasPresets` exactly so an AI can say `setCanvasSize({preset:"xhs-3-4"})`
 * and get the same numbers a human gets from the picker.
 */
export const presets: CanvasPreset[] = [
  { key: 'xhs-3-4', name: '小红书封面 3:4', width: 1242, height: 1656 },
  { key: 'xhs-1-1', name: '小红书方图 1:1', width: 1080, height: 1080 },
  { key: 'bili-16-9', name: 'B站封面 16:9', width: 1920, height: 1080 },
  { key: 'wx-9-16', name: '视频号竖屏 9:16', width: 1080, height: 1920 },
  { key: 'mp-900-383', name: '公众号头图 900:383', width: 900, height: 383 },
]

export function preset(key: string): CanvasPreset | undefined {
  return presets.find((p) => p.key === key)
}
