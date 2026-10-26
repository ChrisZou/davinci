import assert from 'node:assert/strict'
import { test } from 'node:test'
import { createRequire } from 'node:module'
import { build } from 'esbuild'
import React from 'react'

const { outputFiles } = await build({ entryPoints: ['src/components/fields.tsx'], bundle: true, write: false, platform: 'node', format: 'cjs', external: ['react'] })
const require = createRequire(import.meta.url)
const nodes = (node) => !node || typeof node !== 'object' ? [] : [node, ...React.Children.toArray(node.props?.children).flatMap(nodes)]

function field(props) {
  const state = []
  const cleanups = []
  let cursor = 0
  const react = {
    ...React,
    useState(initial) {
      const i = cursor++
      if (!(i in state)) state[i] = initial
      return [state[i], (next) => { state[i] = next }]
    },
    useRef(initial) {
      const i = cursor++
      return state[i] ??= { current: initial }
    },
    useEffect(effect, deps) {
      const i = cursor++
      if (!state[i] || deps.some((v, n) => v !== state[i][n])) {
        cleanups[i]?.()
        cleanups[i] = effect()
        state[i] = deps
      }
    },
  }
  const module = { exports: {} }
  new Function('require', 'module', 'exports', outputFiles[0].text)((name) => name === 'react' ? react : require(name), module, module.exports)
  return {
    input() {
      cursor = 0
      return nodes(module.exports.NumField({ label: '字距', value: 12.5, ...props })).find((node) => node.type === 'input').props
    },
    unmount() { cleanups.forEach((cleanup) => cleanup?.()) },
  }
}

test('typing previews immediately without saving each keystroke; blur saves the final value once', () => {
  const previews = [], commits = []
  const f = field({ onPreview: (v) => previews.push(v), onCommit: (v) => commits.push(v) })
  f.input().onFocus({ target: { select() {} } })
  for (const value of ['5', '50', '51', '-2.5']) f.input().onChange({ target: { value } })
  assert.deepEqual(previews, [5, 50, 51, -2.5])
  assert.deepEqual(commits, [])
  f.input().onBlur()
  assert.deepEqual(commits, [-2.5])
  f.unmount()
  assert.equal(previews.at(-1), null)
})

test('empty or incomplete input cancels the preview and does not commit zero', () => {
  const previews = [], commits = []
  const f = field({ onPreview: (v) => previews.push(v), onCommit: (v) => commits.push(v) })
  f.input().onFocus({ target: { select() {} } })
  for (const value of ['50', '', '-', '1e']) f.input().onChange({ target: { value } })
  assert.deepEqual(previews, [50, null, null, null])
  f.input().onBlur()
  assert.deepEqual(commits, [])
  assert.equal(f.input().value, '12.5')
})

test('Enter delegates to blur so a field commits only once', () => {
  const commits = []
  const f = field({ onCommit: (v) => commits.push(v) })
  f.input().onFocus({ target: { select() {} } })
  f.input().onChange({ target: { value: '50' } })
  f.input().onKeyDown({ key: 'Enter', preventDefault() {}, target: { blur() { f.input().onBlur() } } })
  assert.deepEqual(commits, [50])
})
