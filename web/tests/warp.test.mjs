import assert from 'node:assert/strict'
import { test } from 'node:test'
import { build } from 'esbuild'

async function load(entry) {
  const { outputFiles } = await build({ entryPoints: [entry], bundle: true, write: false, platform: 'node', format: 'esm' })
  return import(`data:text/javascript;base64,${Buffer.from(outputFiles[0].text).toString('base64')}`)
}

const { WARPS, WARP_PANEL, SHEAR, WARP_ALIASES, warpShape } = await load('src/editor/warp.ts')

test('warp catalog covers the reference shapes and resolves aliases', () => {
  const keys = WARPS.map((w) => w.key)
  assert.deepEqual(keys, ['bulge', 'flag', 'pinch', 'trapezoid', 'taperLeft'])
  assert.equal(WARP_ALIASES.taperRight, 'trapezoid')
  assert.equal(warpShape('taperRight')?.key, 'trapezoid')
  assert.equal(warpShape('bulge')?.label, '鼓形')
  assert.equal(warpShape('pinch')?.label, '梯形')
  assert.equal(warpShape('missing'), undefined)
  for (const w of WARPS) {
    assert.ok(w.icon.includes('M') || w.icon.includes('m'), w.key)
  }
})

test('panel order matches 鼓形 → 斜切 → 旗帜 → 梯形 → 左高 → 右高', () => {
  assert.deepEqual(
    WARP_PANEL.map((w) => w.key),
    ['bulge', 'shear', 'flag', 'pinch', 'trapezoid', 'taperLeft'],
  )
  assert.equal(WARP_PANEL[1], SHEAR)
})
