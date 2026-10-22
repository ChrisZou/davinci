import assert from 'node:assert/strict'
import { test } from 'node:test'
import { build } from 'esbuild'

const { outputFiles } = await build({ entryPoints: ['src/components/commentState.ts'], bundle: true, write: false, platform: 'node', format: 'esm' })
const { mergeComments, sessionComments } = await import(`data:text/javascript;base64,${Buffer.from(outputFiles[0].text).toString('base64')}`)
const running = { id: 'c1', status: 'running', createdAt: '2026-10-16T10:00:00Z' }

test('completion pushed before HTTP creation response stays complete', () => {
  const done = { ...running, status: 'done' }
  assert.deepEqual(mergeComments(mergeComments([], [done]), [running]), [done])
})
test('stale history preserves pushed results and new comments', () => {
  for (const status of ['done', 'failed', 'cancelled', 'needs_info']) {
    const complete = { ...running, status }
    const next = { ...running, id: 'c2', createdAt: '2026-10-16T10:01:00Z' }
    assert.deepEqual(mergeComments([complete, next], [running]), [complete, next])
  }
})
test('reconnect history catches up missed completions', () => {
  const done = { ...running, status: 'done' }
  assert.deepEqual(mergeComments([running], [done]), [done])
})
test('a new editor session starts with an empty conversation', () => {
  const earlier = { ...running, id: 'c0', status: 'done', session: 'old' }
  const mine = { ...running, id: 'c2', status: 'done', session: 'new' }
  assert.deepEqual(sessionComments([], [earlier, mine], 'new'), [mine])
})
test('a comment still running from before stays visible after it finishes', () => {
  const blocking = { ...running, session: 'old' }
  const shown = sessionComments([], [blocking], 'new')
  assert.deepEqual(shown, [blocking])
  const done = { ...blocking, status: 'done' }
  assert.deepEqual(sessionComments(shown, [done], 'new'), [done])
})

const bridgeBuild = await build({
  entryPoints: ['src/editor/bridge.ts'], bundle: true, write: false, platform: 'node', format: 'esm',
  plugins: [{ name: 'canvas-stub', setup(b) {
    b.onResolve({ filter: /^\.\/Editor$/ }, () => ({ path: 'canvas-stub', namespace: 'test' }))
    b.onLoad({ filter: /.*/, namespace: 'test' }, () => ({ contents: 'export class Editor {}' }))
  } }],
})
const { Session } = await import(`data:text/javascript;base64,${Buffer.from(bridgeBuild.outputFiles[0].text).toString('base64')}`)
test('one websocket message updates the editor then delivers completion synchronously', () => {
  const events = []
  const session = new Session('p1', {
    onComment: (c) => events.push(c.status),
    onConnected: () => events.push('refresh history'),
  })
  session.editorInstance = { setProject: () => events.push('canvas'), doc: null }
  const document = { version: 2, active: 'b1', boards: [{ id: 'b1', canvas: { width: 100, height: 100 }, layers: [] }] }
  session.onMessage({ type: 'hello', document, revision: 1 })
  assert.deepEqual(events, ['canvas', 'refresh history'])
  events.length = 0
  session.onMessage({ type: 'doc', document, revision: 2, comment: { ...running, status: 'done' } })
  assert.deepEqual(events, ['canvas', 'done'])
  events.length = 0
  session.onMessage({ type: 'comment', comment: { ...running, status: 'failed' } })
  assert.deepEqual(events, ['failed'])
})
