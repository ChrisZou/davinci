import assert from 'node:assert/strict'
import { test } from 'node:test'
import { createRequire } from 'node:module'
import { build } from 'esbuild'
import React from 'react'
import { renderToStaticMarkup } from 'react-dom/server'

const require = createRequire(import.meta.url)
async function load(entry, react = React, modules = {}) {
  const { outputFiles } = await build({
    entryPoints: [entry], bundle: true, write: false, platform: 'node', format: 'cjs', external: ['react', 'react-dom'],
    plugins: [{ name: 'canvas-loader-stub', setup(b) {
      b.onResolve({ filter: /^\.\/ck$/ }, () => ({ path: 'ck', namespace: 'test' }))
      b.onLoad({ filter: /.*/, namespace: 'test' }, () => ({ contents: 'export const getRenderer = () => {}; export const encodeBoard = () => {}; export const blobToDataURL = () => {}' }))
    } }],
  })
  const module = { exports: {} }
  new Function('require', 'module', 'exports', outputFiles[0].text)((name) => name === 'react' ? react : modules[name] ?? require(name), module, module.exports)
  return module.exports
}

const { Editor } = await load('src/editor/Editor.ts')
const { Session } = await load('src/editor/bridge.ts')
const { Properties } = await load('src/components/Properties.tsx', {
  ...React,
  useSyncExternalStore: (subscribe, getSnapshot) => React.useSyncExternalStore(subscribe, getSnapshot, getSnapshot),
})
const layers = [
  { id: 'title', name: '标题', type: 'text', text: '模板标题', x: 20, y: 20, width: 100, height: 80, rotation: 0, opacity: 1, visible: true, style: { fontFamily: 'PingFang SC', fontSize: 24, fill: '#111111' } },
  { id: 'shape', name: '矩形', type: 'shape', shape: { kind: 'rect' }, x: 180, y: 20, width: 100, height: 80, rotation: 0, opacity: 1, visible: true, style: { fill: '#c23a22' } },
  { id: 'locked', name: '锁定', type: 'image', x: 20, y: 180, width: 100, height: 80, rotation: 0, opacity: 1, visible: true, locked: true },
]
const doc = { canvas: { width: 400, height: 400, background: '#ffffff' }, layers }
const pointer = (x, y, extra = {}) => ({ clientX: x, clientY: y, pointerId: 1, button: 0, preventDefault() {}, ...extra })

function canvas(readOnly = true) {
  const commands = [], selections = []
  const captured = new Set()
  // Exercise the real hit testing and pointer handlers without a GPU or WASM.
  const editor = Object.assign(Object.create(Editor.prototype), {
    readOnly, project: { active: 'b1', boards: [{ id: 'b1', ...structuredClone(doc) }] },
    selection: [], gesture: null, editing: null, ghosts: new Map(), previews: new Map(), guides: [], gaps: [],
    z: 1, tx: 0, ty: 0, width: 400, height: 400,
    renderer: { frame: (l) => ({ cw: l.width, ch: l.height, scaleX: 1, scaleY: 1, strokeWidth: 0 }) },
    overlay: {
      style: {}, getBoundingClientRect: () => ({ left: 0, top: 0 }),
      setPointerCapture: (id) => captured.add(id), hasPointerCapture: (id) => captured.has(id), releasePointerCapture: (id) => captured.delete(id),
    },
    cb: { onSelection: (ids) => selections.push(ids), onCommit: async (cmd) => commands.push(cmd) },
    render() {},
  })
  return { editor, commands, selections, click(x, y, extra) { editor.onDown(pointer(x, y, extra)); editor.onUp(pointer(x, y, extra)) } }
}

test('template canvas selects, toggles multiple layers, inspects locked layers and clears on empty click', () => {
  const s = canvas()
  s.click(60, 60)
  assert.deepEqual(s.editor.selectionIDs(), ['title'])
  for (const modifier of ['shiftKey', 'metaKey', 'ctrlKey']) {
    s.click(220, 60, { [modifier]: true })
    assert.deepEqual(s.editor.selectionIDs(), ['title', 'shape'])
    s.click(220, 60, { [modifier]: true })
    assert.deepEqual(s.editor.selectionIDs(), ['title'])
  }
  s.click(220, 60, { shiftKey: true })
  s.click(60, 60)
  assert.deepEqual(s.editor.selectionIDs(), ['title'])
  s.click(60, 220)
  assert.deepEqual(s.editor.selectionIDs(), ['locked'])
  s.click(350, 350)
  assert.deepEqual(s.editor.selectionIDs(), [])
  assert.deepEqual(s.selections.at(-1), [])
  assert.deepEqual(s.commands, [])
})

test('read-only drags, handles, double-clicks and previews cannot edit; pan and zoom still work', () => {
  const s = canvas()
  const before = structuredClone(s.editor.board)
  s.click(60, 60)
  assert.equal(s.editor.handleAt({ x: 20, y: 20 }), null)
  for (const point of [[60, 60], [20, 20], [70, -6]]) {
    s.editor.onDown(pointer(...point))
    s.editor.onMove(pointer(320, 320))
    s.editor.onUp(pointer(320, 320))
    assert.equal(s.editor.ghosts.size, 0)
  }
  s.editor.onDblClick(pointer(60, 60))
  s.editor.onDblClick(pointer(60, 220))
  s.editor.editText('title')
  s.editor.preview('title', 'opacity', 0.1)
  assert.equal(s.editor.editing, null)
  assert.equal(s.editor.previews.size, 0)
  s.editor.onDown(pointer(60, 60, { button: 1 }))
  s.editor.onMove(pointer(100, 90))
  s.editor.onUp(pointer(100, 90))
  assert.deepEqual([s.editor.tx, s.editor.ty], [40, 30])
  s.editor.onWheel(pointer(100, 90, { ctrlKey: true, deltaY: -20 }))
  assert.ok(s.editor.zoom > 1)
  assert.deepEqual(s.editor.board, before)
  assert.deepEqual(s.commands, [])
})

test('edit mode still starts move, resize and rotate gestures and commits a drag', () => {
  const s = canvas(false)
  s.editor.onDown(pointer(60, 60))
  assert.equal(s.editor.gesture.kind, 'move')
  s.editor.onMove(pointer(100, 100))
  s.editor.onUp(pointer(100, 100))
  assert.equal(s.commands[0].type, 'moveLayer')
  s.editor.settle()
  s.editor.onDown(pointer(20, 20))
  assert.equal(s.editor.gesture.kind, 'resize')
  s.editor.onUp(pointer(20, 20))
  s.editor.onDown(pointer(70, -6))
  assert.equal(s.editor.gesture.kind, 'rotate')
  s.editor.onUp(pointer(70, -6))
  s.click(60, 220)
  assert.deepEqual(s.editor.selectionIDs(), [])
})

const nodes = (node) => !node || typeof node !== 'object' ? [] : [node, ...React.Children.toArray(node.props?.children).flatMap(nodes)]

test('layer list keeps selection and range modifiers while disabling rename, reorder and context edits', async () => {
  let cursor = 0
  const state = []
  const { LayerPanel } = await load('src/components/LayerPanel.tsx', {
    ...React,
    useState(initial) {
      const i = cursor++
      if (!(i in state)) state[i] = initial
      return [state[i], (next) => { state[i] = next }]
    },
    useRef(initial) { return state[cursor++] ??= { current: initial } },
  })
  const commands = [], menus = []
  let selected = []
  const render = () => {
    cursor = 0
    return nodes(LayerPanel({
      doc, rows: canvas().editor.layerRows(), selected, readOnly: true,
      onSelect: (ids) => { selected = ids }, onRun: (cmd) => commands.push(cmd), onContextMenu: (...args) => menus.push(args),
    }))
  }
  const row = (id) => render().find((n) => n.type === 'li' && n.props['data-layer-name'] === layers.find((l) => l.id === id).name)
  row('title').props.onClick({})
  assert.deepEqual(selected, ['title'])
  row('shape').props.onClick({ ctrlKey: true })
  assert.deepEqual(selected, ['title', 'shape'])
  row('locked').props.onClick({ shiftKey: true })
  assert.deepEqual(selected, ['locked', 'shape'])
  for (const id of ['title', 'shape', 'locked']) {
    const r = row(id)
    assert.equal(r.props.draggable, false)
    r.props.onDragStart({ preventDefault() {} })
    r.props.onDrop({ preventDefault() {} })
    r.props.onContextMenu({ preventDefault() {} })
    nodes(r).find((n) => n.props?.onDoubleClick).props.onDoubleClick({ stopPropagation() {} })
  }
  assert.equal(render().some((n) => n.type === 'input' || n.type === 'button'), false)
  assert.deepEqual(commands, [])
  assert.deepEqual(menus, [])
})

test('properties keep the same values but disable controls and all mutation callbacks in read-only mode', async () => {
  const calls = []
  const callbacks = Object.fromEntries(['onRun', 'onRunSelect', 'onUpload', 'onPickFromLibrary', 'onPickBackground', 'onCrop', 'onPreview'].map((key) => [key, (...args) => calls.push([key, ...args])]))
  for (const selection of [[], layers.slice(0, 1), layers.slice(1, 2), layers.slice(2), layers]) {
    const props = { doc, selection, layer: selection.length === 1 ? selection[0] : undefined, ...callbacks }
    const tree = Properties({ ...props, readOnly: true })
    assert.equal(tree.props.children.type, 'fieldset')
    assert.equal(tree.props.children.props.disabled, true)
    const content = tree.props.children.props.children
    for (const key of Object.keys(callbacks)) await content.props[key]?.('test', 'opacity', 0.1)
    assert.deepEqual(calls, [])
    const editable = Properties(props).props.children
    assert.ok(!editable.props.disabled)
    for (const key of Object.keys(callbacks)) assert.equal(editable.props.children.props[key], callbacks[key])
    const html = renderToStaticMarkup(tree)
    assert.match(html, /<fieldset disabled=""/)
    for (const layer of selection) assert.ok(html.includes(layer.name))
    if (selection.length === 1) assert.match(html, new RegExp(`data-testid="layer-x"[^>]*value="${selection[0].x}"`))
    if (props.layer?.type === 'text') {
      assert.match(html, /<div[^>]*data-testid="text-font"[^>]*aria-label="字体"/)
      assert.match(html, />PingFang SC<\/span>/)
      assert.doesNotMatch(html, /<button[^>]*data-testid="text-font"|aria-haspopup="listbox"/)
      assert.match(renderToStaticMarkup(Properties(props)), /<button[^>]*data-testid="text-font"/)
    }
  }
})

// Keep component state across renders so the test can disable an already open
// portal, not just check the first render's fieldset attribute.
async function fontPicker() {
  const state = [], effects = [], cleanups = []
  let cursor = 0
  const { FontPicker } = await load('src/components/FontPicker.tsx', {
    ...React,
    useState(initial) {
      const i = cursor++
      if (!(i in state)) state[i] = initial
      return [state[i], (next) => { state[i] = typeof next === 'function' ? next(state[i]) : next }]
    },
    useRef(initial) { return state[cursor++] ??= { current: initial } },
    useMemo(fn) { return fn() },
    useEffect(effect, deps) {
      const i = cursor++
      if (!state[i] || deps.some((v, n) => v !== state[i][n])) {
        state[i] = deps
        effects.push(() => { cleanups[i]?.(); cleanups[i] = effect() })
      }
    },
  }, { 'react-dom': { createPortal: (content) => React.createElement('test-portal', null, content) } })
  return {
    render(props) {
      cursor = 0
      const tree = FontPicker(props)
      // React attaches the trigger ref before running effects.
      if (tree.ref) tree.ref.current = { getBoundingClientRect: () => ({ bottom: 36, right: 300 }) }
      effects.splice(0).forEach((effect) => effect())
      return tree
    },
    dispose() { cleanups.forEach((cleanup) => cleanup?.()) },
  }
}

test('font inspection displays Chinese names, unknown families and defaults without any picker interaction', async (t) => {
  const requests = [], changes = []
  t.mock.method(globalThis, 'fetch', async (url) => {
    requests.push(url)
    return new Response(JSON.stringify({ fonts: [{ family: 'PingFang SC', aliases: ['蘋方-簡', '苹方-简'] }] }))
  })
  const picker = await fontPicker()
  t.after(() => picker.dispose())
  const props = { value: 'PingFang SC', readOnly: true, onChange: (value) => changes.push(value) }
  assert.match(renderToStaticMarkup(picker.render(props)), />PingFang SC<\/span>/)
  await new Promise(setImmediate)
  for (const mode of [{ readOnly: true }, { disabled: true }]) {
    for (const [value, expected] of [['PingFang SC', '苹方-简'], ['Unknown Custom Font', 'Unknown Custom Font'], ['', '默认字体']]) {
      const tree = picker.render({ ...props, readOnly: false, ...mode, value })
      const html = renderToStaticMarkup(tree)
      assert.ok(html.includes(`>${expected}</span>`))
      assert.doesNotMatch(html, /<button|<input|<svg|listbox|font-family|test-portal/)
      assert.ok(nodes(tree).every((n) => !n.props.onClick && !n.props.onKeyDown && !n.props.onChange && n.props.tabIndex === undefined))
    }
  }
  assert.deepEqual(changes, [])
  assert.deepEqual(requests, ['/api/fonts'])
})

test('turning off editing removes an open font portal; enabling it again preserves font selection', async (t) => {
  const win = Object.assign(new EventTarget(), { innerWidth: 1200, innerHeight: 900 })
  const previous = { window: globalThis.window, document: globalThis.document }
  globalThis.window = win
  globalThis.document = { body: {} }
  t.after(() => {
    for (const [key, value] of Object.entries(previous)) {
      if (value === undefined) delete globalThis[key]
      else globalThis[key] = value
    }
  })
  t.mock.method(globalThis, 'fetch', async () => new Response(JSON.stringify({ fonts: [{ family: 'PingFang SC', aliases: ['苹方-简'] }] })))
  const picker = await fontPicker()
  t.after(() => picker.dispose())
  const changes = []
  const props = { value: 'PingFang SC', onChange: (value) => changes.push(value) }
  const trigger = (tree) => nodes(tree).find((n) => n.type === 'button' && n.props['data-testid'] === 'text-font')
  const open = () => {
    trigger(picker.render(props)).props.onClick()
    picker.render(props)
    return picker.render(props)
  }
  picker.render(props)
  await new Promise(setImmediate)
  for (const mode of [{ readOnly: true }, { disabled: true }]) {
    assert.ok(nodes(open()).some((n) => n.props.role === 'listbox'))
    const tree = picker.render({ ...props, ...mode })
    assert.doesNotMatch(renderToStaticMarkup(tree), /<button|test-portal|listbox/)
    assert.deepEqual(changes, [])
    assert.equal(trigger(picker.render(props)).props['aria-expanded'], false)
  }
  const choice = nodes(open()).find((n) => n.type === 'button' && n.props.title === 'PingFang SC')
  choice.props.onClick()
  assert.deepEqual(changes, ['PingFang SC'])
  assert.equal(trigger(picker.render(props)).props['aria-expanded'], false)
})

test('read-only sessions block mutation commands before transport, including JSON and batches', async (t) => {
  const previous = globalThis.window
  globalThis.window = { setTimeout, clearTimeout }
  t.after(() => { if (previous === undefined) delete globalThis.window; else globalThis.window = previous })
  const session = new Session('template')
  session.editorInstance = { readOnly: true, endPreview() {} }
  const sent = []
  session.send = (message) => {
    sent.push(message.command)
    session.pending.get(message.reqId)({ ok: true })
  }
  for (const cmd of [
    { type: 'updateLayer', id: 'title', props: { fontFamily: 'Songti SC' } },
    { type: 'cropImage', id: 'photo' }, { type: 'replaceImage', id: 'photo', url: '/assets/new.png' },
    { type: 'setZIndex', id: 'title', index: 0 }, { type: 'undo' }, { type: 'redo' },
  ]) {
    for (const raw of [cmd, JSON.stringify(cmd), [cmd], { type: 'batch', commands: [cmd] }]) {
      assert.equal((await session.run(raw)).ok, false)
    }
  }
  assert.deepEqual(sent, [])
  assert.equal((await session.run({ type: 'selectBoard', id: 'b2' })).ok, true)
  session.editorInstance.readOnly = false
  assert.equal((await session.run({ type: 'setText', id: 'title', text: '已编辑' })).ok, true)
  assert.deepEqual(sent.map((cmd) => cmd.type), ['selectBoard', 'setText'])
})
