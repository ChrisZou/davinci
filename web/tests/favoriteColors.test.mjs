import assert from 'node:assert/strict'
import { test } from 'node:test'
import { build } from 'esbuild'
import { createRequire } from 'node:module'
import React from 'react'

async function load(entry, react = React) {
  const { outputFiles } = await build({ entryPoints: [entry], bundle: true, write: false, platform: 'node', format: 'cjs', external: ['react'] })
  const module = { exports: {} }, require = createRequire(import.meta.url)
  new Function('require', 'module', 'exports', outputFiles[0].text)((name) => name === 'react' ? react : require(name), module, module.exports)
  return module.exports
}
const hooks = { useEffect() {}, useSyncExternalStore(_subscribe, snapshot) { return snapshot() } }
const response = (colors) => new Response(JSON.stringify({ ok: true, colors }))

test('equivalent hex colours share a key; alpha is preserved and absent/invalid colours cannot be starred', async () => {
  const { normalizeColor } = await load('src/components/colorValue.ts')
  for (const raw of ['#fff', '#FFF', ' #ffffff ', '#ffff', '#FFFFFFFF']) assert.equal(normalizeColor(raw), '#ffffff')
  for (const raw of ['#1238', '#11223388']) assert.equal(normalizeColor(raw), '#11223388')
  for (const raw of ['rgb(255, 255, 255)', 'rgba(255,255,255,1)']) assert.equal(normalizeColor(raw), '#ffffff')
  for (const raw of ['rgba(240,122,52,.5)', '#F07A3480']) assert.equal(normalizeColor(raw), '#f07a3480')
  for (const raw of ['', 'none', 'transparent', '#0000', '#12345600', '#12', '#12345', '#gggggg', 'currentColor', 'var(--color)']) assert.equal(normalizeColor(raw), null, raw)
})

test('shared favourites ignore a pre-save response and serialize edits from separate controls', async (t) => {
  const cache = await load('src/components/favoriteColorState.ts', hooks)
  const requests = []
  t.mock.method(globalThis, 'fetch', (url, init = {}) => new Promise((resolve) => requests.push({ url, init, resolve })))
  const old = cache.reloadFavoriteColors()
  assert.equal(cache.reloadFavoriteColors(), old)
  const first = cache.setFavoriteColor('#ffffff', true)
  const second = cache.setFavoriteColor('#f07a34', true)
  assert.equal(requests.length, 2)
  requests[1].resolve(response(['#ffffff']))
  await first
  await new Promise(setImmediate)
  assert.equal(requests.length, 3)
  requests[2].resolve(response(['#ffffff', '#f07a34']))
  await second
  requests[0].resolve(response([]))
  await old
  assert.deepEqual(cache.useFavoriteColors().colors, ['#ffffff', '#f07a34'])
  assert.equal(cache.useFavoriteColors().saving, false)
  assert.deepEqual(requests.slice(1).map((r) => JSON.parse(r.init.body)), [
    { color: '#ffffff', favorite: true }, { color: '#f07a34', favorite: true },
  ])
})

test('a failed save leaves favourites intact and permits a retry', async (t) => {
  const cache = await load('src/components/favoriteColorState.ts', hooks)
  t.mock.method(globalThis, 'fetch', async () => response(['#ffffff']))
  await cache.reloadFavoriteColors()
  t.mock.method(globalThis, 'fetch', async () => new Response(JSON.stringify({ error: 'offline' }), { status: 503 }))
  await assert.rejects(cache.setFavoriteColor('#ffffff', false), /offline/)
  assert.deepEqual(cache.useFavoriteColors().colors, ['#ffffff'])
  assert.equal(cache.useFavoriteColors().saving, false)
  t.mock.method(globalThis, 'fetch', async () => response([]))
  await cache.setFavoriteColor('#ffffff', false)
  assert.deepEqual(cache.useFavoriteColors().colors, [])
})

test('colour fields star the edited draft, show all single colours, apply and unstar them, and omit palettes', async (t) => {
  let colors = ['#ffffff', '#000000', '#112233', '#224466', '#336699', '#445566', '#556677', '#667788']
  const requests = [], committed = [], state = []
  let cursor = 0
  t.mock.method(globalThis, 'fetch', async (url, init) => {
    const body = JSON.parse(init.body)
    requests.push({ url, ...body })
    colors = body.favorite ? [...new Set([...colors, body.color])] : colors.filter((c) => c !== body.color)
    return response(colors)
  })
  const previousWindow = globalThis.window
  globalThis.window = { clearTimeout() {}, setTimeout() { return 1 } }
  t.after(() => { if (previousWindow === undefined) delete globalThis.window; else globalThis.window = previousWindow })
  const { ColorField } = await load('src/components/fields.tsx', {
    ...React, useEffect() {}, useRef(value) { return { current: value } },
    useState(initial) {
      const i = cursor++
      if (!(i in state)) state[i] = initial
      return [state[i], (next) => { state[i] = next }]
    },
    useSyncExternalStore() { return { colors, error: '', saving: false } },
  })
  const nodes = (node) => !node || typeof node !== 'object' ? [] : [node, ...React.Children.toArray(node.props?.children).flatMap(nodes)]
  const render = () => { cursor = 0; return nodes(ColorField({ label: '文字', value: '#000000', onCommit: (color) => committed.push(color) })) }
  const star = () => render().find((n) => n.props['data-testid'] === 'favorite-color')
  const swatches = () => render().filter((n) => n.props['aria-label']?.startsWith('使用收藏颜色'))
  assert.equal(swatches().length, 8, 'do not drop favourites after seven colours')
  assert.equal(star().props['aria-pressed'], true)
  assert.ok(render().every((n) => n.props.children !== '我的配色'))
  render().find((n) => n.props['aria-label'] === '文字（色值）').props.onChange({ target: { value: '#F07A34' } })
  star().props.onClick()
  await new Promise(setImmediate)
  assert.deepEqual(requests.at(-1), { url: '/api/colors/favorites', color: '#f07a34', favorite: true })
  assert.equal(star().props['aria-pressed'], true)
  star().props.onClick()
  await new Promise(setImmediate)
  assert.equal(colors.includes('#f07a34'), false)
  swatches()[0].props.onClick()
  assert.deepEqual(committed, ['#ffffff'])
  assert.equal(star().props['aria-pressed'], true)
  render().find((n) => n.props['aria-label'] === '文字（色值）').props.onChange({ target: { value: 'none' } })
  assert.equal(star().props.disabled, true)
})
