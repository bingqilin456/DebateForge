// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { createAnnotationApi, listAnnotations, retryAnnotation } from './annotations'
import { createApiClient } from './client'

/**
 * 批注 API 测试：路径、方法、请求体、状态码与响应校验。
 * 全部通过注入的 fetch 桩，不连接真实后端。
 */

const fetcher = vi.fn<typeof fetch>()

function envelope(data: unknown, requestId = 'req-annotation') {
  return new Response(JSON.stringify({ data, request_id: requestId }), {
    status: 200,
    headers: { 'Content-Type': 'application/json', 'X-Request-ID': requestId },
  })
}

function envelopeStatus(data: unknown, status: number, requestId = 'req-annotation') {
  return new Response(JSON.stringify({ data, request_id: requestId }), {
    status,
    headers: { 'Content-Type': 'application/json', 'X-Request-ID': requestId },
  })
}

function failure(status: number, code: string) {
  return new Response(JSON.stringify({ error: { code, message: '受控提示' }, request_id: 'req-annotation' }), {
    status,
    headers: { 'Content-Type': 'application/json', 'X-Request-ID': 'req-annotation' },
  })
}

function anchor(overrides: Record<string, unknown> = {}) {
  return { message_id: '21', quote: '原文', start_cp: 0, end_cp: 2, resolved: true, ...overrides }
}

function item(overrides: Record<string, unknown> = {}) {
  return {
    category: 'strength', label: '论证有效', reason: '理由', improvement: '改进方向',
    anchor: anchor(), turn_no: 1, role: 'user', ...overrides,
  }
}

function batch(overrides: Record<string, unknown> = {}) {
  return { turn_id: '31', round_no: 1, status: 'completed', attempt_version: 1, items: [item()], ...overrides }
}

function client() {
  // 补做是受保护写操作：显式提供 CSRF token，避免命中“登录状态未恢复”分支。
  return createApiClient({ getCsrfToken: () => 'csrf-annotation' })
}

beforeEach(() => {
  fetcher.mockReset()
  vi.stubGlobal('fetch', fetcher)
})
afterEach(() => { vi.unstubAllGlobals() })

describe('批注读取', () => {
  it('GET 只读路径，返回按触发轮排序的批次', async () => {
    fetcher.mockResolvedValueOnce(envelope({ items: [batch()] }))
    const result = await createAnnotationApi(client()).list('7')
    expect(result.items).toHaveLength(1)
    expect(result.items[0]!.status).toBe('completed')
    const [url, init] = fetcher.mock.calls[0]!
    expect(url).toBe('/api/v1/debates/7/annotations')
    // 只读入口不带请求方法覆盖，也不带正文。
    expect(init?.body).toBeUndefined()
    expect(init?.headers).not.toHaveProperty('X-CSRF-Token')
  })

  it('旧会话返回空数组而不是错误', async () => {
    fetcher.mockResolvedValueOnce(envelope({ items: [] }))
    const result = await createAnnotationApi(client()).list('7')
    expect(result.items).toEqual([])
  })

  it.each([
    ['未开始带条目', { items: [batch({ status: 'not_started', attempt_version: 0 })] }],
    ['未开始版本非 0', { items: [batch({ status: 'not_started', attempt_version: 3, items: [] })] }],
    ['生成中带条目', { items: [batch({ status: 'generating' })] }],
    ['失败却带错误码缺失', { items: [batch({ status: 'failed', items: [] })] }],
    ['未知类别', { items: [batch({ items: [item({ category: 'fact_error' })] })] }],
    ['空标签', { items: [batch({ items: [item({ label: '  ' })] })] }],
    ['超长理由', { items: [batch({ items: [item({ reason: '理'.repeat(301) })] })] }],
    ['未知角色', { items: [batch({ items: [item({ role: 'system' })] })] }],
    ['非法轮号', { items: [batch({ items: [item({ turn_no: 0 })] })] }],
    ['非数字 turn_id', { items: [batch({ turn_id: 'abc' })] }],
    ['非整数版本', { items: [batch({ attempt_version: 1.5 })] }],
  ])('拒绝不符合契约的响应：%s', async (_name, payload) => {
    fetcher.mockResolvedValueOnce(envelope(payload))
    await expect(createAnnotationApi(client()).list('7')).rejects.toMatchObject({ kind: 'format' })
  })

  it('拒绝已解析却没有坐标的锚点', async () => {
    fetcher.mockResolvedValueOnce(envelope({ items: [batch({ items: [item({ anchor: anchor({ start_cp: null, end_cp: null }) })] })] }))
    await expect(createAnnotationApi(client()).list('7')).rejects.toMatchObject({ kind: 'format' })
  })

  it('拒绝未解析却带坐标的锚点（避免半截坐标被高亮）', async () => {
    fetcher.mockResolvedValueOnce(envelope({ items: [batch({ items: [item({ anchor: anchor({ resolved: false }) })] })] }))
    await expect(createAnnotationApi(client()).list('7')).rejects.toMatchObject({ kind: 'format' })
  })

  it('接受未解析且无坐标的待核对锚点', async () => {
    fetcher.mockResolvedValueOnce(envelope({
      items: [batch({ items: [item({ anchor: anchor({ resolved: false, start_cp: null, end_cp: null }) })] })],
    }))
    const result = await createAnnotationApi(client()).list('7')
    expect(result.items[0]!.items[0]!.anchor.resolved).toBe(false)
  })

  it('拒绝非法会话编号，且不发起请求', async () => {
    await expect(createAnnotationApi(client()).list('0')).rejects.toMatchObject({ kind: 'format' })
    await expect(createAnnotationApi(client()).list('../etc')).rejects.toMatchObject({ kind: 'format' })
    expect(fetcher).not.toHaveBeenCalled()
  })

  it('404 按既有错误语义返回受控提示', async () => {
    fetcher.mockResolvedValueOnce(failure(404, 'not_found'))
    await expect(createAnnotationApi(client()).list('7')).rejects.toMatchObject({ status: 404, code: 'not_found' })
  })
})

describe('批注手动补做', () => {
  it('POST 只发送 expected_attempt_version，使用生成超时', async () => {
    fetcher.mockResolvedValueOnce(envelopeStatus(batch(), 201))
    const result = await createAnnotationApi(client()).retry('7', '31', 0)
    expect(result.status).toBe('completed')
    const [url, init] = fetcher.mock.calls[0]!
    expect(url).toBe('/api/v1/debates/7/turns/31/annotations/retry')
    expect(init?.method).toBe('POST')
    expect(JSON.parse(String(init?.body))).toEqual({ expected_attempt_version: 0 })
  })

  it('202 返回生成中的批次且不带条目', async () => {
    fetcher.mockResolvedValueOnce(envelopeStatus(batch({ status: 'generating', items: [] }), 202))
    const result = await createAnnotationApi(client()).retry('7', '31', 0)
    expect(result.status).toBe('generating')
    expect(result.items).toEqual([])
  })

  it('409 版本冲突按既有错误分支返回', async () => {
    fetcher.mockResolvedValueOnce(failure(409, 'attempt_version_conflict'))
    await expect(createAnnotationApi(client()).retry('7', '31', 1)).rejects.toMatchObject({ status: 409, code: 'attempt_version_conflict' })
  })

  it('拒绝响应与请求轮次不一致的批次', async () => {
    fetcher.mockResolvedValueOnce(envelopeStatus(batch({ turn_id: '99' }), 201))
    await expect(createAnnotationApi(client()).retry('7', '31', 0)).rejects.toMatchObject({ kind: 'format' })
  })

  it('拒绝非法版本与编号，且不发起请求', async () => {
    await expect(createAnnotationApi(client()).retry('7', '31', -1)).rejects.toMatchObject({ kind: 'format' })
    await expect(createAnnotationApi(client()).retry('7', '31', 1.5)).rejects.toMatchObject({ kind: 'format' })
    await expect(createAnnotationApi(client()).retry('7', '0', 0)).rejects.toMatchObject({ kind: 'format' })
    expect(fetcher).not.toHaveBeenCalled()
  })
})

describe('函数式入口', () => {
  it('复用传入的客户端', async () => {
    fetcher.mockResolvedValueOnce(envelope({ items: [] }))
    const instance = client()
    expect((await listAnnotations('7', instance)).items).toEqual([])
    fetcher.mockResolvedValueOnce(envelopeStatus(batch(), 200))
    expect((await retryAnnotation('7', '31', 1, instance)).status).toBe('completed')
  })
})
