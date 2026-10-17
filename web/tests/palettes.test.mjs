import assert from 'node:assert/strict'
import { test } from 'node:test'
import { build } from 'esbuild'
import { createRequire } from 'node:module'
import React from 'react'
import { renderToStaticMarkup } from 'react-dom/server'

async function bundled(entry, react = React) {
  const { outputFiles } = await build({ entryPoints: [entry], bundle: true, write: false, platform: 'node', format: 'cjs', external: ['react', 'react-dom/server'] })
  const module = { exports: {} }
  const require = createRequire(import.meta.url)
  new Function('require', 'module', 'exports', outputFiles[0].text)((name) => name === 'react' ? react : require(name), module, module.exports)
  return module.exports
}
const { api } = await bundled('src/api.ts')
const draft = { name: '暖色', bg: '#ffffff', text: '#111111', highlight: '#ff4400', text2: '#666666', accent: '#0077aa' }
const saved = { ...draft, id: 'pal_one', createdAt: '2026-10-17', updatedAt: '2026-10-17' }

test('palette API keeps analysis separate from saving and supports editor target', async () => {
  const requests = []
  const original = globalThis.fetch
  globalThis.fetch = async (path, init = {}) => {
    requests.push({ path, ...init })
    return new Response(JSON.stringify({ ok: true, palette: path.endsWith('/analyze') ? draft : saved, palettes: [saved] }), { status: 200 })
  }
  try {
    assert.deepEqual(await api.analyzePalette(new Blob(['image'], { type: 'image/png' })), draft)
    assert.equal(requests.length, 1)
    assert.equal(requests[0].path, '/api/palettes/analyze')
    assert.ok(requests[0].body instanceof FormData)
    assert.equal(await requests[0].body.get('file').text(), 'image')
    assert.deepEqual(await api.savePalette(draft), saved)
    assert.deepEqual(JSON.parse(requests[1].body), draft)
    await api.savePalette({ ...draft, text: '#222222' }, saved.id)
    assert.equal(requests[2].method, 'PATCH')
    assert.equal(JSON.parse(requests[2].body).text, '#222222')
    assert.deepEqual(await api.palettes(), [saved])
    assert.deepEqual(await api.palette(saved.id), saved)
    await api.applyPalette('p_one', saved.id, 'board_second')
    assert.equal(requests[5].path, '/api/projects/p_one/palettes/pal_one/apply')
    assert.deepEqual(JSON.parse(requests[5].body), { board: 'board_second' })
    await api.deletePalette(saved.id)
    assert.equal(requests[6].method, 'DELETE')
  } finally { globalThis.fetch = original }
})

test('palette API propagates analysis/save/apply errors', async () => {
  const original = globalThis.fetch
  globalThis.fetch = async () => new Response(JSON.stringify({ ok: false, error: '无效配色' }), { status: 400 })
  try {
    await assert.rejects(api.analyzePalette(new Blob(['bad'])), /无效配色/)
    await assert.rejects(api.savePalette(draft), /无效配色/)
    await assert.rejects(api.applyPalette('p_one', 'absent'), /无效配色/)
  } finally { globalThis.fetch = original }
})

const { PalettePreview, PaletteDetail } = await bundled('src/components/PaletteParts.tsx')
test('cover preview renders all five semantic colours and escapes palette names', () => {
  const html = renderToStaticMarkup(React.createElement(PalettePreview, { palette: draft, name: '<封面>' }))
  for (const color of [draft.bg, draft.text, draft.highlight, draft.text2, draft.accent]) assert.ok(html.includes(color), color)
  assert.ok(html.includes('&lt;封面&gt;'))
})
test('detail exposes native picker, hex field and copy control for each role', () => {
  const html = renderToStaticMarkup(React.createElement(PaletteDetail, { initial: draft, id: saved.id, onClose() {}, onSaved() {}, async onApply() {} }))
  assert.equal((html.match(/type="color"/g) ?? []).length, 5)
  for (const role of ['背景色', '主文字色', '高亮色', '次文字色', '点缀色']) {
    assert.ok(html.includes(`${role}（色值）`))
    assert.ok(html.includes(`复制${role}`))
  }
  assert.ok(html.includes('应用到当前画板'))
  assert.ok(html.includes('删除配色'))
})


test('a pre-save list response cannot overwrite the fresh saved palette list', async () => {
  const cache = await bundled('src/components/paletteState.ts', {
    useEffect() {},
    useSyncExternalStore(_subscribe, snapshot) { return snapshot() },
  })
  const original = globalThis.fetch
  const resolve = []
  globalThis.fetch = () => new Promise((done) => resolve.push(done))
  try {
    const old = cache.reloadPalettes()
    assert.equal(cache.reloadPalettes(), old, 'colour fields share one request')
    const fresh = cache.reloadPalettes(true)
    resolve[1](new Response(JSON.stringify({ ok: true, palettes: [saved] })))
    await fresh
    resolve[0](new Response(JSON.stringify({ ok: true, palettes: [] })))
    await old
    assert.deepEqual(cache.usePalettes().items, [saved])
  } finally { globalThis.fetch = original }
})
