import assert from 'node:assert/strict'
import { test } from 'node:test'
import { build } from 'esbuild'

const { outputFiles } = await build({
  entryPoints: ['src/types.ts'], bundle: true, write: false, platform: 'node', format: 'esm',
})
const types = await import(`data:text/javascript;base64,${Buffer.from(outputFiles[0].text).toString('base64')}`)

test('canvasBackgroundVisible defaults to true and respects false', () => {
  assert.equal(types.canvasBackgroundVisible(undefined), true)
  assert.equal(types.canvasBackgroundVisible({}), true)
  assert.equal(types.canvasBackgroundVisible({ backgroundVisible: true }), true)
  assert.equal(types.canvasBackgroundVisible({ backgroundVisible: false }), false)
})

test('BACKGROUND_ID helpers recognise aliases', () => {
  assert.equal(types.BACKGROUND_ID, '__background')
  assert.equal(types.isBackgroundID('__background'), true)
  assert.equal(types.isBackgroundID('background'), true)
  assert.equal(types.isBackgroundID('背景'), true)
  assert.equal(types.isBackgroundID('title'), false)
})

const kb = await build({
  entryPoints: ['src/editor/keyboard.ts'], bundle: true, write: false, platform: 'node', format: 'esm',
})
const { installShortcuts } = await import(`data:text/javascript;base64,${Buffer.from(kb.outputFiles[0].text).toString('base64')}`)

test('V toggles canvas background when the virtual row is selected', (t) => {
  const win = new EventTarget()
  const docEl = Object.assign(new EventTarget(), { activeElement: null })
  const restore = []
  for (const [key, value] of Object.entries({ window: win, document: docEl })) {
    const descriptor = Object.getOwnPropertyDescriptor(globalThis, key)
    Object.defineProperty(globalThis, key, { configurable: true, value })
    restore.push(() => descriptor ? Object.defineProperty(globalThis, key, descriptor) : delete globalThis[key])
  }
  const canvas = { width: 100, height: 100, background: '#ffffff', backgroundVisible: true }
  const state = { selected: ['__background'] }
  const commands = []
  const editor = {
    doc: { canvas, layers: [] }, readOnly: false,
    selectionIDs: () => [...state.selected],
    selectableIDs: () => [],
  }
  const handler = installShortcuts(editor, {
    isEditing: () => false,
    run: async (command) => { commands.push(command); return { ok: true } },
    select: (ids) => { state.selected = ids },
  })
  t.after(() => {
    handler.dispose()
    restore.forEach((reset) => reset())
  })
  const event = Object.assign(new Event('keydown', { cancelable: true }), { key: 'v' })
  win.dispatchEvent(event)
  assert.equal(event.defaultPrevented, true)
  assert.deepEqual(commands, [{ type: 'setVisible', id: '__background', visible: false }])
  canvas.backgroundVisible = false
  win.dispatchEvent(Object.assign(new Event('keydown', { cancelable: true }), { key: 'v' }))
  assert.deepEqual(commands.at(-1), { type: 'setVisible', id: '__background', visible: true })
})
