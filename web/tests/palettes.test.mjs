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

test('colour fields group five roles, show three most recently updated palettes and toggle more', async () => {
  const items = [1, 4, 2, 3, 5].map((n) => ({ ...saved, id: `pal_${n}`, name: `配色${n}`, updatedAt: `2026-10-${10 + n}T00:00:00Z` }))
  // Repeated values must remain as distinct role buttons within their palette.
  items[4].accent = items[4].bg
  const state = []
  let cursor = 0
  const { ColorField } = await bundled('src/components/fields.tsx', {
    ...React,
    useEffect() {},
    useRef(value) { return { current: value } },
    useState(initial) {
      const i = cursor++
      if (!(i in state)) state[i] = initial
      return [state[i], (next) => { state[i] = next }]
    },
    useSyncExternalStore() { return { items, error: '' } },
  })
  const nodes = (node) => !node || typeof node !== 'object' ? [] : [node, ...React.Children.toArray(node.props?.children).flatMap(nodes)]
  let committed
  const render = () => {
    cursor = 0
    return nodes(ColorField({ label: '颜色', value: '#000000', onCommit(value) { committed = value } }))
  }
  let tree = render()
  const groups = () => tree.filter((n) => n.props.role === 'group')
  const toggle = () => tree.find((n) => n.type === 'button' && 'aria-expanded' in n.props)
  assert.deepEqual(groups().map((n) => n.props['aria-label']), ['配色5', '配色4', '配色3'])
  const buttons = nodes(groups()[0]).filter((n) => n.type === 'button')
  assert.equal(buttons.length, 5)
  assert.deepEqual(buttons.map((n) => n.props.style.background), ['bg', 'text', 'highlight', 'text2', 'accent'].map((key) => items[4][key]))
  assert.match(buttons[3].props.title, /配色5 · 次文字/)
  const original = globalThis.window
  globalThis.window = { clearTimeout }
  try { buttons[3].props.onClick(); assert.equal(committed, items[4].text2) }
  finally { globalThis.window = original }
  assert.equal(toggle().props['aria-expanded'], false)
  toggle().props.onClick()
  tree = render()
  assert.equal(groups().length, 5)
  assert.equal(toggle().props.children, '收起')
  toggle().props.onClick()
  tree = render()
  assert.equal(groups().length, 3)
  assert.deepEqual(items.map((p) => p.id), ['pal_1', 'pal_4', 'pal_2', 'pal_3', 'pal_5'], 'do not reorder the shared cache')
})

test('new palette detail shows the source thumbnail beside the editable cover preview', () => {
  const props = { initial: draft, onClose() {}, onSaved() {} }
  const html = renderToStaticMarkup(React.createElement(PaletteDetail, { ...props, sourceImage: 'blob:reference-image' }))
  assert.ok(html.includes('src="blob:reference-image"'))
  assert.ok(html.includes('alt="配色原图"'))
  assert.ok(html.includes('object-contain'))
  assert.ok(html.includes('aria-label="封面预览"'))
  assert.equal((html.match(/type="color"/g) ?? []).length, 5)
  const savedHTML = renderToStaticMarkup(React.createElement(PaletteDetail, { ...props, id: saved.id }))
  assert.ok(!savedHTML.includes('alt="配色原图"'))
})

const { PaletteExamples, highlightStyle } = await bundled('src/components/PaletteExamples.tsx')
test('examples render three proportional layouts and highlight only key words', () => {
  const html = renderToStaticMarkup(React.createElement(PaletteExamples, { palette: draft }))
  assert.equal((html.match(/<figure/g) ?? []).length, 3)
  for (const label of ['16:9 幻灯片', '3:4 竖版封面', '1:1 社交卡片']) assert.ok(html.includes(label))
  assert.match(html, /<h3>让<mark[^>]*>灵感<\/mark>成为日常<\/h3>/)
  for (const role of ['bg', 'text', 'text2', 'highlight', 'accent']) assert.ok(html.includes(draft[role]))
  assert.equal(highlightStyle(draft).backgroundColor, draft.highlight)
  const lowContrast = { ...draft, highlight: draft.text }
  assert.equal(highlightStyle(lowContrast).backgroundColor, 'transparent')
  assert.equal(highlightStyle(lowContrast).textDecorationColor, lowContrast.highlight)
  assert.equal(highlightStyle(lowContrast).color, lowContrast.text)
  assert.equal(lowContrast.highlight, draft.text, 'never rewrite supplied colours')
})

test('full detail displays original and examples; compact detail keeps contextual link', () => {
  const props = { initial: { ...saved, imageUrl: '/assets/original.png' }, id: saved.id, onClose() {}, onSaved() {} }
  const html = renderToStaticMarkup(React.createElement(PaletteDetail, { ...props, full: true }))
  assert.ok(html.includes('palette-detail-full'))
  assert.ok(html.includes('palette-original'))
  assert.ok(html.includes('src="/assets/original.png"'))
  assert.ok(html.includes('object-contain'))
  assert.ok(html.includes('更换原图'))
  assert.ok(html.includes('按原图重新提色'))
  assert.ok(html.includes('示例设计'))
  assert.ok(!html.includes('关闭配色详情'))
  const empty = renderToStaticMarkup(React.createElement(PaletteDetail, { ...props, initial: draft, full: true }))
  assert.ok(empty.includes('暂无原图'))
  assert.ok(empty.includes('上传原图'))
  const compact = renderToStaticMarkup(React.createElement(PaletteDetail, { ...props, detailHref: '/palettes/pal_one?project=p_one&board=board_two' }))
  assert.ok(compact.includes('查看详情'))
  assert.ok(compact.includes('/palettes/pal_one?project=p_one&amp;board=board_two'))
  assert.ok(!compact.includes('示例设计'))
})

test('saving with a source image sends original bytes alongside colour overrides', async () => {
  const original = globalThis.fetch
  const requests = []
  globalThis.fetch = async (path, init) => {
    requests.push({ path, ...init })
    return new Response(JSON.stringify({ palette: { ...saved, imageUrl: '/assets/hash.png' } }))
  }
  try {
    const image = new Blob(['original bytes'], { type: 'image/png' })
    const updated = { ...draft, bg: '#123456' }
    const added = await api.savePalette(updated, undefined, image)
    await api.savePalette(updated, saved.id, image)
    assert.equal(added.imageUrl, '/assets/hash.png')
    for (const request of requests) {
      assert.ok(request.body instanceof FormData)
      assert.deepEqual(JSON.parse(request.body.get('palette')), updated)
      assert.equal(await request.body.get('file').text(), 'original bytes')
      assert.equal(request.headers, undefined, 'browser provides multipart boundary')
    }
    assert.equal(requests[0].method, 'POST')
    assert.equal(requests[1].method, 'PATCH')
  } finally { globalThis.fetch = original }
})

test('editing a role updates examples before saving; replacing an image preserves colours', async () => {
  const state = []
  let cursor = 0
  const { PaletteDetail: Detail } = await bundled('src/components/PaletteParts.tsx', {
    ...React, useEffect() {}, useRef(value) { return { current: value } },
    useState(initial) {
      const index = cursor++
      if (!(index in state)) state[index] = typeof initial === 'function' ? initial() : initial
      return [state[index], (value) => { state[index] = typeof value === 'function' ? value(state[index]) : value }]
    },
  })
  const nodes = (node) => !node || typeof node !== 'object' ? [] : [node, ...React.Children.toArray(node.props?.children).flatMap(nodes)]
  const render = () => { cursor = 0; return nodes(Detail({ initial: draft, id: saved.id, full: true, onSaved() {}, onClose() {} })) }
  let tree = render()
  tree.find((n) => n.props['aria-label'] === '高亮色（色值）').props.onChange({ target: { value: '#224466' } })
  tree = render()
  const example = tree.find((n) => n.type.name === 'PaletteExamples')
  assert.equal(example.props.palette.highlight, '#224466')
  assert.ok(renderToStaticMarkup(React.createElement(PaletteExamples, example.props)).includes('#224466'))
  const before = { ...example.props.palette }
  tree.find((n) => n.props['aria-label'] === '上传或更换原图').props.onChange({ target: { files: [new Blob(['image'])], value: '' } })
  tree = render()
  assert.deepEqual(tree.find((n) => n.type.name === 'PaletteExamples').props.palette, before)
  assert.equal(tree.find((n) => n.type === 'button' && n.props.children === '保存').props.disabled, false)
})
