import assert from 'node:assert/strict'
import { test } from 'node:test'
import { build } from 'esbuild'

const { outputFiles } = await build({
  entryPoints: ['src/editor/imageClipboard.ts'], bundle: true, write: false, platform: 'node', format: 'esm',
})
const { copyImageToClipboard } = await import(`data:text/javascript;base64,${Buffer.from(outputFiles[0].text).toString('base64')}`)

function clipboard(t, write) {
  t.mock.method(globalThis, 'fetch')
  for (const [key, value] of Object.entries({
    navigator: { clipboard: { write } },
    ClipboardItem: class { constructor(data) { this.data = data } },
  })) {
    const descriptor = Object.getOwnPropertyDescriptor(globalThis, key)
    Object.defineProperty(globalThis, key, { configurable: true, value })
    t.after(() => descriptor ? Object.defineProperty(globalThis, key, descriptor) : delete globalThis[key])
  }
}

test('starts clipboard write before the asset loads and preserves PNG bytes', async (t) => {
  let finishFetch
  let writing = false
  let received
  clipboard(t, async ([item]) => {
    writing = true
    received = await item.data['image/png']
  })
  const png = new Blob(['original PNG bytes'], { type: 'image/png' })
  fetch.mock.mockImplementation(() => new Promise((resolve) => { finishFetch = resolve }))
  const copying = copyImageToClipboard('/assets/cutout.png')
  assert.equal(writing, true, 'user activation must not be lost waiting for fetch')
  finishFetch(new Response(png))
  await copying
  assert.equal(received.type, 'image/png')
  assert.equal(await received.text(), await png.text())
  assert.equal(fetch.mock.calls[0].arguments[0], '/assets/cutout.png')
})

test('reports a missing source instead of copying an error response', async (t) => {
  clipboard(t, async ([item]) => { await item.data['image/png'] })
  fetch.mock.mockImplementation(async () => new Response('not found', { status: 404 }))
  await assert.rejects(copyImageToClipboard('/missing.png'), /图片加载失败（404）/)
})

test('reports clipboard denial even when image loading also fails', async (t) => {
  clipboard(t, async () => { throw new Error('clipboard denied') })
  fetch.mock.mockImplementation(async () => { throw new Error('network failed') })
  await assert.rejects(copyImageToClipboard('/assets/image.png'), /clipboard denied/)
})
