import assert from 'node:assert/strict'
import { test } from 'node:test'
import { createRequire } from 'node:module'
import { build } from 'esbuild'

const require = createRequire(import.meta.url)
async function load(entry) {
  const { outputFiles } = await build({
    entryPoints: [entry], bundle: true, write: false, platform: 'node', format: 'cjs', external: ['react', 'react-dom'],
    plugins: [{ name: 'canvas-loader-stub', setup(b) {
      b.onResolve({ filter: /^\.\/ck$/ }, () => ({ path: 'ck', namespace: 'test' }))
      b.onLoad({ filter: /.*/, namespace: 'test' }, () => ({ contents: 'export const getRenderer = () => {}; export const encodeBoard = () => {}; export const blobToDataURL = () => {}' }))
    } }],
  })
  const module = { exports: {} }
  new Function('require', 'module', 'exports', outputFiles[0].text)(require, module, module.exports)
  return module.exports
}

const { Editor } = await load('src/editor/Editor.ts')
const layers = [
  { id: 'title', name: '标题', type: 'text', text: 'Hi', x: 20, y: 30, width: 100, height: 80, rotation: 0, opacity: 1, visible: true, style: { fontSize: 24, lineHeight: 1.2, letterSpacing: 0 } },
  { id: 'rect', name: '矩形', type: 'shape', shape: { kind: 'rect' }, x: 180, y: 20, width: 100, height: 80, rotation: 0, opacity: 1, visible: true, style: { fill: '#c23a22', strokeWidth: 4, cornerRadius: 8 } },
]
const doc = { canvas: { width: 400, height: 400, background: '#ffffff' }, layers }

function canvas() {
  const renders = []
  const editor = Object.assign(Object.create(Editor.prototype), {
    readOnly: false,
    project: { active: 'b1', boards: [{ id: 'b1', ...structuredClone(doc) }] },
    selection: [], gesture: null, editing: null, ghosts: new Map(), previews: new Map(), canvasPreview: null, guides: [], gaps: [],
    z: 1, tx: 0, ty: 0, width: 400, height: 400,
    renderer: { frame: (l) => ({ cw: l.width, ch: l.height, scaleX: 1, scaleY: 1, strokeWidth: 0 }) },
    overlay: { style: {}, getBoundingClientRect: () => ({ left: 0, top: 0 }) },
    cb: {},
    render() { renders.push(structuredClone(editor.viewBoard())) },
  })
  return { editor, renders, view: () => editor.viewBoard() }
}

test('numeric property previews update the viewed board without mutating the document', () => {
  const s = canvas()
  const before = structuredClone(s.editor.board)
  s.editor.preview('title', 'fontSize', 96)
  s.editor.preview('title', 'lineHeight', 1.8)
  s.editor.preview('title', 'x', 55)
  s.editor.preview('title', 'rotation', 15)
  s.editor.preview('rect', 'strokeWidth', 12)
  s.editor.preview('rect', 'cornerRadius', 20)
  s.editor.preview('rect', 'shadow', 'rgba(0,0,0,0.5):8:2:4')
  const title = s.view().layers.find((l) => l.id === 'title')
  const rect = s.view().layers.find((l) => l.id === 'rect')
  assert.equal(title.style.fontSize, 96)
  assert.equal(title.style.lineHeight, 1.8)
  assert.equal(title.x, 55)
  assert.equal(title.rotation, 15)
  assert.equal(rect.style.strokeWidth, 12)
  assert.equal(rect.style.cornerRadius, 20)
  assert.equal(rect.style.shadow, 'rgba(0,0,0,0.5):8:2:4')
  assert.deepEqual(s.editor.board, before)
  s.editor.preview('title', 'fontSize', null)
  assert.equal(s.view().layers.find((l) => l.id === 'title').style.fontSize, 24)
})

test('canvas size preview overlays width/height then clears', () => {
  const s = canvas()
  const before = structuredClone(s.editor.board.canvas)
  s.editor.preview('', 'canvasWidth', 800)
  s.editor.preview('', 'canvasHeight', 600)
  assert.deepEqual(s.view().canvas, { ...before, width: 800, height: 600 })
  assert.deepEqual(s.editor.board.canvas, before)
  s.editor.endPreview()
  assert.deepEqual(s.view().canvas, before)
  assert.equal(s.editor.canvasPreview, null)
})

test('fillGradient preview accepts a gradient object', () => {
  const s = canvas()
  const gradient = { type: 'linear', angle: 90, stops: [{ color: '#ff0000', opacity: 1 }, { color: '#0000ff', opacity: 0.5 }] }
  s.editor.preview('rect', 'fillGradient', gradient)
  assert.deepEqual(s.view().layers.find((l) => l.id === 'rect').style.fillGradient, gradient)
  s.editor.preview('rect', 'fillGradient', null)
  assert.equal(s.view().layers.find((l) => l.id === 'rect').style.fillGradient, undefined)
})
