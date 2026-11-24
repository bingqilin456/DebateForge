import { afterEach, describe, expect, it, vi } from 'vitest'
import { createPersonalModelApi } from './personalModel'
import { createApiClient } from './client'

const configured = { enabled: true, configured: true, alias: '训练用模型', verified: false, used: false }
afterEach(() => vi.unstubAllGlobals())

function response(data: unknown) {
  return new Response(JSON.stringify({ data, request_id: 'req-model' }), {
    headers: { 'Content-Type': 'application/json', 'X-Request-ID': 'req-model' },
  })
}

describe('个人模型 API', () => {
  it('保存只提交配置，校验和停用由各自的显式动作触发', async () => {
    const fetch = vi.fn<typeof globalThis.fetch>()
      .mockResolvedValueOnce(response(configured))
      .mockResolvedValueOnce(response({ ok: true }))
      .mockResolvedValueOnce(response({ ok: true }))
    vi.stubGlobal('fetch', fetch)
    const api = createPersonalModelApi(createApiClient({ getCsrfToken: () => 'fake-csrf' }))
    await expect(api.save({ provider: 'custom', model: 'synthetic-model', alias: configured.alias,
      base_url: 'https://models.invalid/v1', api_key: 'synthetic-test-only' })).resolves.toEqual(configured)
    expect(fetch.mock.calls.map(([path]) => path)).toEqual(['/api/v1/settings/model'])
    await api.test()
    await api.remove()
    expect(fetch.mock.calls.map(([path]) => path)).toEqual([
      '/api/v1/settings/model', '/api/v1/settings/model/test', '/api/v1/settings/model/remove',
    ])
  })

  it('读取可区分已保存、已验证与已实际使用，并拒绝回传密钥', async () => {
    const fetch = vi.fn<typeof globalThis.fetch>()
      .mockResolvedValueOnce(response({ ...configured, verified: true, used: false }))
      .mockResolvedValueOnce(response({ ...configured, api_key: 'synthetic-test-only' }))
    vi.stubGlobal('fetch', fetch)
    const api = createPersonalModelApi(createApiClient())
    await expect(api.get()).resolves.toMatchObject({ configured: true, verified: true, used: false })
    await expect(api.get()).rejects.toMatchObject({ kind: 'format' })
  })
})
