import assert from 'node:assert/strict'
import { test } from 'node:test'
import { build } from 'esbuild'

const { outputFiles } = await build({
  entryPoints: ['src/editor/keyboard.ts'], bundle: true, write: false, platform: 'node', format: 'esm',
})
const { installShortcuts } = await import(`data:text/javascript;base64,${Buffer.from(outputFiles[0].text).toString('base64')}`)

function shortcuts(t, layers = [{ id: 'title', visible: true }]) {
  const win = new EventTarget()
  const doc = Object.assign(new EventTarget(), { activeElement: null })
  const restore = []
  for (const [key, value] of Object.entries({ window: win, document: doc })) {
    const descriptor = Object.getOwnPropertyDescriptor(globalThis, key)
    Object.defineProperty(globalThis, key, { configurable: true, value })
    restore.push(() => descriptor ? Object.defineProperty(globalThis, key, descriptor) : delete globalThis[key])
  }
  const state = { selected: layers.map((l) => l.id), editing: false }
  const commands = []
  const editor = {
    doc: { layers }, readOnly: false,
    selectionIDs: () => [...state.selected],
  }
  const handler = installShortcuts(editor, {
    isEditing: () => state.editing,
    run: async (command) => { commands.push(command); return { ok: true } },
    select: (ids) => { state.selected = ids },
  })
  t.after(() => {
    handler.dispose()
    restore.forEach((reset) => reset())
  })
  return {
    commands, state, editor, doc,
    key(key = 'v', options = {}) {
      const event = Object.assign(new Event('keydown', { cancelable: true }), { key, ...options })
      win.dispatchEvent(event)
      return event
    },
  }
}

test('V hides the selected layer and shows it again without losing selection', (t) => {
  const layer = { id: 'title', visible: true }
  const s = shortcuts(t, [layer])
  assert.equal(s.key().defaultPrevented, true)
  assert.deepEqual(s.commands, [{ type: 'setVisible', id: 'title', visible: false }])
  // The session applies the server's updated layer to the editor.
  layer.visible = false
  assert.equal(s.key('V').defaultPrevented, true)
  assert.deepEqual(s.commands.at(-1), { type: 'setVisible', id: 'title', visible: true })
  assert.deepEqual(s.state.selected, ['title'])
})

test('V handles a mixed selection together in one undoable batch', (t) => {
  const layers = [{ id: 'title', visible: true }, { id: 'photo', visible: false, locked: true }]
  const s = shortcuts(t, layers)
  s.key()
  assert.deepEqual(s.commands, [{ type: 'batch', commands: [
    { type: 'setVisible', id: 'title', visible: false },
    { type: 'setVisible', id: 'photo', visible: false },
  ] }])
  layers.forEach((layer) => { layer.visible = false })
  s.key()
  assert.deepEqual(s.commands.at(-1), { type: 'batch', commands: [
    { type: 'setVisible', id: 'title', visible: true },
    { type: 'setVisible', id: 'photo', visible: true },
  ] })
})

test('typing, IME composition, read-only views and empty selection do not toggle visibility', (t) => {
  const s = shortcuts(t)
  for (const tagName of ['INPUT', 'TEXTAREA', 'SELECT']) {
    s.doc.activeElement = { tagName }
    assert.equal(s.key().defaultPrevented, false)
  }
  s.doc.activeElement = { tagName: 'DIV', isContentEditable: true }
  assert.equal(s.key().defaultPrevented, false)
  s.doc.activeElement = null
  s.state.editing = true
  assert.equal(s.key().defaultPrevented, false)
  s.state.editing = false
  assert.equal(s.key('v', { isComposing: true }).defaultPrevented, false)
  s.editor.readOnly = true
  assert.equal(s.key().defaultPrevented, false)
  s.editor.readOnly = false
  s.state.selected = []
  assert.equal(s.key().defaultPrevented, false)
  assert.deepEqual(s.commands, [])
})

test('holding V does not create repeated visibility changes', (t) => {
  const s = shortcuts(t)
  s.key()
  for (let i = 0; i < 5; i++) s.key('v', { repeat: true })
  assert.equal(s.commands.length, 1)
})

test('modified V leaves paste and other key combinations alone', (t) => {
  const s = shortcuts(t)
  for (const modifier of ['metaKey', 'ctrlKey', 'altKey', 'shiftKey']) {
    assert.equal(s.key('v', { [modifier]: true }).defaultPrevented, false)
  }
  assert.deepEqual(s.commands, [])
  const paste = Object.assign(new Event('paste', { cancelable: true }), {
    clipboardData: { files: [], getData: () => 'pasted text' },
  })
  s.doc.dispatchEvent(paste)
  assert.equal(paste.defaultPrevented, true)
  assert.equal(s.commands.length, 1)
  assert.equal(s.commands[0].type, 'addText')
  assert.equal(s.commands[0].text, 'pasted text')
})
