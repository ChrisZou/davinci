import assert from 'node:assert/strict'
import { test } from 'node:test'
import { readFileSync } from 'node:fs'
import { createRequire } from 'node:module'
import { build } from 'esbuild'
import CanvasKitInit from 'canvaskit-wasm/bin/canvaskit.js'

const require = createRequire(import.meta.url)
const ck = await CanvasKitInit({ wasmBinary: readFileSync(require.resolve('canvaskit-wasm/bin/canvaskit.wasm')) })
const { outputFiles } = await build({ entryPoints: ['src/render/renderer.ts'], bundle: true, write: false, platform: 'node', format: 'esm' })
const { Renderer } = await import(`data:text/javascript;base64,${Buffer.from(outputFiles[0].text).toString('base64')}`)
const renderer = new Renderer(ck, { font: async () => null, image: async () => null }, ['test'])
// Fixed glyph metrics, with ink disabled, isolate the actual background pixels.
renderer.face = () => ({ typeface: {}, weight: 400, italic: false })
renderer.fonts.get = (_, size) => ({ getGlyphIDs: () => new Uint16Array([1]), getGlyphWidths: () => new Float32Array([size]) })
const layer = (style = {}, props = {}) => ({ id: 'text', type: 'text', text: 'AB', x: 100, y: 100, width: 200, height: 45.2, style: { fontSize: 40, lineHeight: 1.2, fill: 'none', textBackgroundColor: '#ff0000', ...style }, ...props })

function pixels(l, options = {}) {
  const bytes = renderer.encode({ canvas: { width: 500, height: 400 }, layers: [l] }, { transparent: true, ...options })
  const image = ck.MakeImageFromEncoded(bytes)
  const width = image.width(), height = image.height()
  const data = image.readPixels(0, 0, { width, height, colorType: ck.ColorType.RGBA_8888, alphaType: ck.AlphaType.Unpremul, colorSpace: ck.ColorSpace.SRGB })
  image.delete()
  const alpha = (x, y) => data[(y * width + x) * 4 + 3]
  const filled = []
  for (let y = 0; y < height; y++) for (let x = 0; x < width; x++) if (alpha(x, y) > 0) filled.push([x, y])
  const bounds = { left: Math.min(...filled.map(([x]) => x)), right: Math.max(...filled.map(([x]) => x)), top: Math.min(...filled.map(([, y]) => y)), bottom: Math.max(...filled.map(([, y]) => y)) }
  return { alpha, width, height, bounds, count: filled.length }
}

test('padding expands all four sides for every alignment without changing text layout', () => {
  for (const textAlign of ['left', 'center', 'right']) {
    const l = layer({ textAlign })
    const measurement = renderer.measure(l)
    const original = pixels(l).bounds
    l.style.padding = 12
    const padded = pixels(l).bounds
    assert.deepEqual(padded, { left: original.left - 12, right: original.right + 12, top: original.top - 12, bottom: original.bottom + 12 })
    assert.deepEqual(renderer.measure(l), measurement)
  }
  for (const padding of [-20, NaN, Infinity]) assert.deepEqual(pixels(layer({ padding })).bounds, pixels(layer()).bounds)
  assert.equal(pixels(layer({ padding: 30, textBackgroundColor: '' })).count, 0)
})

test('multiline translucent backgrounds do not darken at padded overlaps; blank lines stay empty', () => {
  const l = layer({ paddingX: 12, paddingY: 12, textBackgroundColor: '#ff000080' }, { text: 'AB\nA' })
  const p = pixels(l)
  assert.equal(p.alpha(110, 110), 128)
  assert.equal(p.alpha(110, 150), 128)
  assert.equal(p.alpha(110, 180), 128)
  assert.equal(p.alpha(170, 180), 0)
  const blank = pixels(layer({ padding: 4 }, { text: 'AB\n\nA' }))
  assert.equal(blank.alpha(110, 175), 0)
})

test('horizontal and vertical padding stay independent, including explicit zero over legacy padding', () => {
  for (const textAlign of ['left', 'center', 'right']) {
    const original = pixels(layer({ textAlign })).bounds
    for (const [x, y] of [[24, 0], [0, 16], [24, 8]]) {
      const l = layer({ textAlign, padding: 12, paddingX: x, paddingY: y })
      assert.deepEqual(pixels(l).bounds, { left: original.left - x, right: original.right + x, top: original.top - y, bottom: original.bottom + y })
      assert.deepEqual(renderer.measure(l), renderer.measure(layer({ textAlign })))
    }
    assert.deepEqual(pixels(layer({ textAlign, padding: 12, paddingX: 0 })).bounds, {
      ...original, top: original.top - 12, bottom: original.bottom + 12,
    })
  }
})

test('large padding remains visible with warping and invalidates the flattened-text cache', () => {
  const l = layer({ warp: 'trapezoid', warpAmount: 50, padding: 0 })
  const original = pixels(l)
  l.style.padding = 60
  const padded = pixels(l)
  assert.ok(padded.bounds.left <= original.bounds.left - 59)
  assert.ok(padded.bounds.top <= original.bounds.top - 59)
  assert.ok(padded.bounds.bottom >= original.bounds.bottom + 59)
  assert.ok(padded.alpha(50, 60) > 240)
})

test('selected-layer and group PNG exports retain the full padded background', () => {
  for (const warpAmount of [0, 50, -50]) {
    for (const padding of [{ padding: 30 }, { paddingX: 50, paddingY: 0 }, { paddingX: 0, paddingY: 50 }, { paddingX: 24, paddingY: 8 }]) {
      const l = layer({ ...padding, warp: 'trapezoid', warpAmount }, { text: 'ABCDE' })
      const full = pixels(l)
      const cropped = pixels(l, { ids: [l.id] })
      // Skia's warped strip edges can cover a few different subpixels after translation.
      assert.ok(Math.abs(cropped.count - full.count) <= full.count * 0.002, JSON.stringify({ warpAmount, full: full.bounds, cropped: cropped.bounds, fullCount: full.count, croppedCount: cropped.count }))
      const group = { id: 'group', type: 'group', x: 100, y: 100, width: 201, height: 46.2, children: [l] }
      const fullGroup = pixels(group)
      const croppedGroup = pixels(group, { ids: [group.id] })
      assert.ok(Math.abs(croppedGroup.count - fullGroup.count) <= fullGroup.count * 0.002, JSON.stringify({ warpAmount, padding, fullGroup: fullGroup.bounds, croppedGroup: croppedGroup.bounds }))
    }
  }
})
