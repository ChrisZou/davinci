import assert from 'node:assert/strict'
import { test } from 'node:test'
import { build } from 'esbuild'

const { outputFiles } = await build({ entryPoints: ['src/api.ts'], bundle: true, write: false, platform: 'node', format: 'cjs' })
const module = { exports: {} }
new Function('module', 'exports', outputFiles[0].text)(module, module.exports)
const { api } = module.exports

test('project metadata travels through create, patch and image import without conflating source and link', async () => {
  const requests = []
  const original = globalThis.fetch
  const metadata = { source: '小红书', author: '作者', likes: 120, link: 'https://example.com/post', note: '备注', templateId: 'p_template' }
  globalThis.fetch = async (path, init = {}) => {
    requests.push({ path, ...init })
    return new Response(JSON.stringify({ ok: true, project: { id: 'p_design', ...metadata } }))
  }
  try {
    assert.equal((await api.createProject({ ...metadata, kind: 'template' })).source, '小红书')
    assert.deepEqual(JSON.parse(requests[0].body), { ...metadata, kind: 'template' })
    await api.updateProject('p_design', metadata)
    assert.equal(requests[1].method, 'PATCH')
    assert.deepEqual(JSON.parse(requests[1].body), metadata)
    const clear = { source: '', author: '', likes: 0, templateId: '' }
    await api.updateProject('p_design', clear)
    assert.deepEqual(JSON.parse(requests[2].body), clear)
    await api.projectFromImage('https://example.com/image.png', { ...metadata, likes: 0, name: '模板', tags: ['大字'] })
    const form = requests[3].body
    assert.equal(form.get('ref'), 'https://example.com/image.png')
    assert.equal(form.get('kind'), 'template')
    assert.equal(form.get('tags'), '大字')
    assert.equal(form.get('name'), '模板')
    for (const [key, value] of Object.entries({ ...metadata, likes: 0 })) assert.equal(form.get(key), String(value))
    const copy = await api.duplicateProject('p_template')
    assert.equal(copy.templateId, 'p_template')
    assert.equal(requests[4].path, '/api/projects/p_template/duplicate')
  } finally { globalThis.fetch = original }
})
