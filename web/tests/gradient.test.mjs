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
const renderer = new Renderer(ck, { font: async () => null, image: async () => null }, [])
const gradient = (angle = 90, alpha = 1) => ({ type: 'linear', angle, stops: [{ color: '#ff0000', opacity: 1 }, { color: '#0000ff', opacity: alpha }] })
const rectangle = (style = {}, props = {}) => ({ id: 'rect', type: 'shape', name: 'rect', x: 10, y: 10, width: 200, height: 100, opacity: 1, rotation: 0, visible: true, shape: { kind: 'rect' }, style: { fill: '#00ff00', strokeWidth: 0, ...style }, ...props })
const board = (layer) => ({ canvas: { width: 260, height: 260, background: '#ffffff' }, layers: [layer] })

function pixels(layer, options = {}) {
  const bytes = renderer.encode(board(layer), { transparent: true, ...options })
  const image = ck.MakeImageFromEncoded(bytes)
  const width = image.width()
  const data = image.readPixels(0, 0, { width, height: image.height(), colorType: ck.ColorType.RGBA_8888, alphaType: ck.AlphaType.Unpremul, colorSpace: ck.ColorSpace.SRGB })
  image.delete()
  return (x, y) => Array.from(data.slice((y * width + x) * 4, (y * width + x) * 4 + 4))
}

test('horizontal and vertical gradients reach both edges of non-square rectangles', () => {
  const horizontal = pixels(rectangle({ fillGradient: gradient(90) }))
  assert.ok(horizontal(12, 60)[0] > 245)
  assert.ok(horizontal(207, 60)[2] > 245)
  const midpoint = horizontal(110, 60)
  assert.ok(midpoint[0] > 120 && midpoint[0] < 135 && midpoint[2] > 120 && midpoint[2] < 135)
  const vertical = pixels(rectangle({ fillGradient: gradient(180) }))
  assert.ok(vertical(110, 12)[0] > 245)
  assert.ok(vertical(110, 107)[2] > 245)
})

test('transparent endpoints preserve colour without a dark halo and combine with layer opacity', () => {
  const g = gradient(90, 0)
  const sample = pixels(rectangle({ fillGradient: g }, { opacity: 0.5 }))
  const middle = sample(110, 60)
  assert.ok(middle[0] > 245 && middle[2] < 5, middle.join(','))
  assert.ok(middle[3] >= 62 && middle[3] <= 65, middle.join(','))
  assert.ok(sample(207, 60)[3] < 5)
})

test('rounded corners remain transparent and the stroke stays solid', () => {
  const sample = pixels(rectangle({ fillGradient: gradient(), cornerRadius: 24, stroke: '#00ff00', strokeWidth: 6 }))
  assert.equal(sample(10, 10)[3], 0)
  assert.deepEqual(sample(110, 10), [0, 255, 0, 255])
  assert.ok(sample(110, 60)[0] > 100 && sample(110, 60)[2] > 100)
})

test('gradient rotates and scales with the shape and survives PNG export', () => {
  const sample = pixels(rectangle({ fillGradient: gradient() }, { x: 120, y: 10, rotation: 90 }), { scale: 2 })
  assert.ok(sample(140, 26)[0] > 245)
  assert.ok(sample(140, 414)[2] > 245)
})

test('solid documents and malformed imported gradients retain their solid fill', () => {
  for (const fillGradient of [undefined, {}, { type: 'linear', angle: 90, stops: [] }]) {
    assert.deepEqual(pixels(rectangle({ fillGradient }))(110, 60), [0, 255, 0, 255])
  }
})
