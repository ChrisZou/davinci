import assert from 'node:assert/strict'
import { test } from 'node:test'
import { readFileSync } from 'node:fs'
import { build } from 'esbuild'
import { unzipSync, strFromU8 } from 'fflate'

// Use the project's existing TS bundler; tests also run on Node 20.
const { outputFiles } = await build({
  entryPoints: ['src/editor/export.ts'], bundle: true, write: false, platform: 'node', format: 'esm',
})
const { exportName, exportBoards, exportBoardNames } = await import(`data:text/javascript;base64,${Buffer.from(outputFiles[0].text).toString('base64')}`)

test('filenames are portable, bounded and retain Chinese names', () => {
  assert.equal(exportName('封面 / 第一版'), '封面 _ 第一版')
  assert.equal(exportName('../a\\b:*?"<>|\0'), '_a_b________')
  assert.equal(exportName(' . . '), 'davinci')
  assert.equal(exportName('CON.txt'), '_CON.txt')
  assert.equal(exportName('lpt1'), '_lpt1')
  assert.equal(exportName('e\u0301'), 'é')
  assert.ok(Buffer.byteLength(exportName('封面😀'.repeat(100))) <= 180)
})

const board = (name, index) => ({ id: `b${index}`, name, canvas: { width: 40 + index, height: 30 }, layers: [] })

for (const format of ['png', 'jpeg', 'webp']) {
  test(`${format}: ZIP contains each board once with unique safe names and shared options`, async () => {
    const boards = ['封/面', '封:面', '封_面 (2)', 'CON', 'cover', 'COVER', '..', 'é', 'e\u0301'].map(board)
    const options = { format, scale: 3, quality: 0.8, transparent: format !== 'jpeg' }
    let count = 0
    const archive = await exportBoards(boards, options, async (b, received) => {
      assert.deepEqual(received, { ...options, ids: undefined })
      assert.equal(b.id, `b${count++}`)
      // A remote update during resource loading must not alter later exports.
      boards[1].canvas.width = 999
      return new Blob([JSON.stringify(b)])
    })
    const entries = unzipSync(new Uint8Array(await archive.arrayBuffer()))
    const ext = format === 'jpeg' ? 'jpg' : format
    const names = ['封_面', '封_面 (2)', '封_面 (2) (2)', '_CON', 'cover', 'COVER (2)', '画板 7', 'é', 'é (2)'].map((n) => `${n}.${ext}`)
    assert.equal(archive.type, 'application/zip')
    assert.deepEqual(Object.keys(entries), names)
    assert.equal(count, boards.length)
    assert.equal(JSON.parse(strFromU8(entries[names[1]])).canvas.width, 41)
    for (const [index, name] of names.entries()) assert.equal(JSON.parse(strFromU8(entries[name])).id, `b${index}`)
  })
}

test('an encoding failure rejects the archive rather than downloading partial results', async () => {
  let count = 0
  await assert.rejects(exportBoards(['one', 'two', 'three'].map(board), {}, async () => {
    if (++count === 2) throw new Error('image failed')
    return new Blob(['image'])
  }), /image failed/)
  assert.equal(count, 2)
})

// This exact fixture is also consumed by internal/doc/export_test.go.
test('browser and CLI filename contract', () => {
  const fixture = JSON.parse(readFileSync('../testdata/export-names.json', 'utf8'))
  for (const format of ['png', 'jpg', 'jpeg', 'webp']) {
    const ext = format === 'jpeg' ? 'jpg' : format
    assert.deepEqual(exportBoardNames(fixture.names.map(name => ({ name })), format), fixture.stems.map(s => `${s}.${ext}`))
  }
})
