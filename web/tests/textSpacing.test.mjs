import assert from 'node:assert/strict'
import { test } from 'node:test'
import { build } from 'esbuild'

async function load(entry) {
  const { outputFiles } = await build({ entryPoints: [entry], bundle: true, write: false, platform: 'node', format: 'esm' })
  return import(`data:text/javascript;base64,${Buffer.from(outputFiles[0].text).toString('base64')}`)
}
const { layoutText, letterSpacingPx } = await load('src/render/text.ts')
const { Renderer } = await load('src/render/renderer.ts')
// Fixed one-em glyph metrics keep these layout assertions independent of system fonts.
const face = { typeface: {}, weight: 400, italic: false }
const faces = { face: () => face, fallbacks: ['test'] }
const fonts = { get: (_, size) => ({ getGlyphIDs: () => new Uint16Array([1]), getGlyphWidths: () => new Float32Array([size]) }) }
const layer = (style, text = '告别抽卡', width = 2000) => ({ id: 'title', type: 'text', text, width, style })
const layout = (style, text, width = 2000) => layoutText(layer(style, text, width), width, faces, fonts)

test('50px adds 50 between characters at any font size; negative and fractional spacing work', () => {
  for (const fontSize of [40, 100, 250]) {
    for (const letterSpacing of [50, 0, -5, 12.5]) {
      const result = layout({ fontSize, letterSpacing })
      assert.equal(result.width, fontSize * 4 + letterSpacing * 3)
      assert.equal(result.lines.length, 1)
    }
  }
})

test('legacy documents retain their width and display converted pixels, including explicit zero overrides', () => {
  const legacy = { fontSize: 250, charSpacing: 50 }
  assert.equal(letterSpacingPx(legacy), 12.5)
  assert.deepEqual(layout(legacy).lines.map(({ width }) => width), [1037.5])
  assert.equal(layout({ ...legacy, letterSpacing: 50 }).width, 1150)
  assert.equal(layout({ ...legacy, letterSpacing: 0 }).width, 1000)
  assert.equal(letterSpacingPx({}), 0)
})

test('pixel spacing affects CJK wrapping and treats combined characters as one character', () => {
  assert.equal(layout({ fontSize: 100, letterSpacing: 0 }, '告别抽卡', 440).lines.length, 1)
  const spaced = layout({ fontSize: 100, letterSpacing: 50 }, '告别抽卡', 440)
  assert.deepEqual(spaced.lines.map((line) => line.glyphs.map((g) => g.text).join('')), ['告别抽', '卡'])
  assert.deepEqual(spaced.lines.map((line) => line.width), [400, 100])
  assert.equal(layout({ fontSize: 100, letterSpacing: 50 }, 'e\u0301e\u0301').width, 250)
})

test('changing pixel spacing invalidates the renderer measurement cache', () => {
  const renderer = new Renderer({}, { font: async () => null, image: async () => null }, ['test'])
  renderer.face = faces.face
  renderer.fonts.get = fonts.get
  const text = layer({ fontSize: 100, letterSpacing: 0 }, '告别抽卡', 440)
  assert.equal(renderer.measure(text).lines, 1)
  text.style.letterSpacing = 50
  assert.equal(renderer.measure(text).lines, 2)
  text.style.letterSpacing = -5
  assert.equal(renderer.measure(text).width, 385)
})
