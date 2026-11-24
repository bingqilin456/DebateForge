import { afterEach, expect, it, vi } from 'vitest'
import { createApiClient } from './client'
import { createJuryApi } from './jury'

afterEach(() => vi.unstubAllGlobals())

it('reads an incomplete verdict and explicitly retries only the requested seat', async () => {
  const seat = { id: '2', judge_id: 'preset-evidence', judge_name: '证据裁判', model_id: 'demo', model_alias: '本地演示', status: 'failed', source: 'local' }
  const partial = { status: 'partial', seats: [seat], score: { user: [70, 71, 72, 73, 74], assistant: [60, 61, 62, 63, 64], user_total: 72, assistant_total: 62, outcome: 'win', source: 'local' } }
  const fetch = vi.fn<typeof globalThis.fetch>().mockImplementation(async () => new Response(JSON.stringify({ data: partial, request_id: 'jury-test' }), {
    headers: { 'Content-Type': 'application/json', 'X-Request-ID': 'jury-test' },
  }))
  vi.stubGlobal('fetch', fetch)
  const api = createJuryApi(createApiClient({ getCsrfToken: () => 'synthetic-csrf' }), '42')
  await expect(api.verdict()).resolves.toEqual(partial)
  expect(fetch).toHaveBeenCalledTimes(1)
  await api.retry('2')
  expect(fetch.mock.calls[1]?.[0]).toBe('/api/v1/debates/42/verdict/seats/2/retry')
  expect(fetch.mock.calls[1]?.[1]?.method).toBe('POST')
})
