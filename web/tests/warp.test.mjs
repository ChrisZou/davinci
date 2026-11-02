import assert from 'node:assert/strict'
import { test } from 'node:test'
import { build } from 'esbuild'

async function load(entry) {
  const { outputFiles } = await build({ entryPoints: [entry], bundle: true, write: false, platform: 'node', format: 'esm' })
  return import(`data:text/javascript;base64,${Buffer.from(outputFiles[0].text).toString('base64')}`)
}

const { WARPS, WARP_PANEL, SHEAR, WARP_ALIASES, WEDGE_WARPS, defaultWarpBias, warpShape } = await load('src/editor/warp.ts')

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

test('wedge icons keep a flat bottom (only the top edge slopes)', () => {
  const leftHigh = WARPS.find((w) => w.key === 'trapezoid')
  const rightHigh = WARPS.find((w) => w.key === 'taperLeft')
  assert.equal(leftHigh.icon, 'M5 4 L35 14 L35 20 L5 20 Z')
  assert.equal(rightHigh.icon, 'M5 14 L35 4 L35 20 L5 20 Z')
})

test('wedge warps default bias to −100 (bottom flat); others to 0', () => {
  assert.equal(defaultWarpBias('trapezoid'), -100)
  assert.equal(defaultWarpBias('taperLeft'), -100)
  assert.equal(defaultWarpBias('taperRight'), -100)
  assert.equal(defaultWarpBias('bulge'), 0)
  assert.equal(defaultWarpBias('flag'), 0)
  assert.equal(defaultWarpBias('pinch'), 0)
  assert.ok(WEDGE_WARPS.has('trapezoid') && WEDGE_WARPS.has('taperLeft'))
})
