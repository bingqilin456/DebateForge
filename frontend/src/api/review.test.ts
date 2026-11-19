// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { createReviewApi, isReviewStatus } from './review'
import { createApiClient } from './client'

/**
 * V2-13 / V2-10 复盘契约测试：使用隔离 fetch 桩，不发真实请求。
 * 重点覆盖：四维固定顺序、观察不足合法且不按 0、旧总结形状不被误判为复盘。
 */

const fetcher = vi.fn<typeof fetch>()

function envelope(data: unknown, status = 200, requestId = 'req-review') {
  return new Response(JSON.stringify({ data, request_id: requestId }), {
    status,
    headers: { 'Content-Type': 'application/json', 'X-Request-ID': requestId },
  })
}

// 每次请求都新建 Response：Response 正文只能读一次，复用同一个对象会让第二次请求失败。
function respondWith(data: unknown, status = 200) {
  fetcher.mockImplementation(async () => envelope(data, status))
}

function failure(status: number, code: string) {
  return new Response(JSON.stringify({ error: { code, message: '受控提示' }, request_id: 'req-review' }), {
    status,
    headers: { 'Content-Type': 'application/json', 'X-Request-ID': 'req-review' },
  })
}

function client() {
  // 复盘生成是受保护写操作：显式提供 CSRF token，避免命中“登录状态未恢复”分支。
  return createApiClient({ getCsrfToken: () => 'csrf-review' })
}

beforeEach(() => {
  fetcher.mockReset()
  vi.stubGlobal('fetch', fetcher)
})
afterEach(() => { vi.unstubAllGlobals() })

function quote(text = '我方认为开放式考核更能检验真实能力'): Record<string, unknown> {
  return { message_id: '101', quote: text, start_cp: 0, end_cp: [...text].length, resolved: true }
}

function dimension(key: string, label: string, level: number | null, extra: Record<string, unknown> = {}) {
  if (level === null) {
    return {
      key, label, level: null, level_label: null, basis: '本场可观察到的行为',
      improvement: '', insufficient_reason: '本场缺少足够观察机会', quote: null, ...extra,
    }
  }
  return {
    key, label, level, level_label: { 1: '起步', 2: '发展中', 3: '较稳', 4: '充分' }[level],
    basis: '行为依据', improvement: '改进提示', insufficient_reason: null, quote: quote(), ...extra,
  }
}

function completed(overrides: Record<string, unknown> = {}) {
  return {
    status: 'completed',
    attempt_version: 1,
    schema_version: 1,
    content: {
      dimensions: [
        dimension('argument_structure', '论证结构', 3),
        dimension('responsiveness', '回应针对性', null),
        dimension('evidence_use', '证据使用', 2),
        dimension('concept_scope', '概念与范围', null),
      ],
      key_issues: [{ text: '主要争点', quote: quote() }],
      strengths: [{ text: '论证优点', quote: quote() }],
      improvements: [{ text: '优先改进', quote: quote() }],
    },
    ...overrides,
  }
}

function dimensionsOf(value: unknown): unknown[] {
  return (value as { content: { dimensions: unknown[] } }).content.dimensions
}

describe('isReviewStatus', () => {
  it('接受四维齐全、部分观察不足与全部观察不足', () => {
    expect(isReviewStatus(completed())).toBe(true)
    expect(isReviewStatus(completed({
      content: {
        ...(completed() as { content: Record<string, unknown> }).content,
        dimensions: [
          dimension('argument_structure', '论证结构', null),
          dimension('responsiveness', '回应针对性', null),
          dimension('evidence_use', '证据使用', null),
          dimension('concept_scope', '概念与范围', null),
        ],
      },
    }))).toBe(true)
  })

  it('拒绝观察不足被当成 0 档或 5 档', () => {
    for (const level of [0, 5, -1]) {
      const value = completed({
        content: {
          ...(completed() as { content: Record<string, unknown> }).content,
          dimensions: [
            dimension('argument_structure', '论证结构', level, { insufficient_reason: '原因' }),
            dimension('responsiveness', '回应针对性', null),
            dimension('evidence_use', '证据使用', 2),
            dimension('concept_scope', '概念与范围', null),
          ],
        },
      })
      expect(isReviewStatus(value)).toBe(false)
    }
  })

  it('观察不足不得携带档位标签或坐标', () => {
    const illegal = completed({
      content: {
        ...(completed() as { content: Record<string, unknown> }).content,
        dimensions: [
          dimension('argument_structure', '论证结构', null, { quote: quote() }),
          dimension('responsiveness', '回应针对性', null),
          dimension('evidence_use', '证据使用', 2),
          dimension('concept_scope', '概念与范围', null),
        ],
      },
    })
    expect(isReviewStatus(illegal)).toBe(false)
  })

  it('有档级必须有已解析引用与改进提示', () => {
    const dims = dimensionsOf(completed())
    for (const broken of [
      { ...(dims[0] as Record<string, unknown>), quote: null },
      { ...(dims[0] as Record<string, unknown>), improvement: '' },
      { ...(dims[0] as Record<string, unknown>), level_label: '充分' },
    ]) {
      const value = completed({ content: { ...(completed() as { content: Record<string, unknown> }).content, dimensions: [broken, ...dims.slice(1)] } })
      expect(isReviewStatus(value)).toBe(false)
    }
  })

  it('拒绝维度乱序、缺项与维度标签不符', () => {
    const base = completed() as { content: { dimensions: unknown[] } }
    const reordered = completed({ content: { ...base.content, dimensions: [base.content.dimensions[1], base.content.dimensions[0], base.content.dimensions[2], base.content.dimensions[3]] } })
    expect(isReviewStatus(reordered)).toBe(false)
    const missing = completed({ content: { ...base.content, dimensions: base.content.dimensions.slice(0, 3) } })
    expect(isReviewStatus(missing)).toBe(false)
    const wrongLabel = completed({
      content: { ...base.content, dimensions: [dimension('argument_structure', '回应针对性', 3), ...base.content.dimensions.slice(1)] },
    })
    expect(isReviewStatus(wrongLabel)).toBe(false)
  })

  it('拒绝未完成/失败状态携带内容，以及缺失错误码', () => {
    expect(isReviewStatus({ status: 'generating', attempt_version: 1, content: (completed() as { content: unknown }).content })).toBe(false)
    expect(isReviewStatus({ status: 'failed', attempt_version: 1 })).toBe(false)
    expect(isReviewStatus({ status: 'failed', attempt_version: 1, error_code: 'model_timeout' })).toBe(true)
    expect(isReviewStatus({ status: 'not_started', attempt_version: null })).toBe(true)
  })

  it('关闭切片的 available:false 合法，旧 not_started 形状保持不变', () => {
    // 省略 available 等价于可用：旧 Mock 与旧响应不必改写。
    expect(isReviewStatus({ status: 'not_started', attempt_version: null })).toBe(true)
    expect(isReviewStatus({ status: 'not_started', attempt_version: null, available: false })).toBe(true)
    expect(isReviewStatus({ status: 'not_started', attempt_version: null, available: true })).toBe(true)
    // 非布尔值（含 null）不是合法契约。
    expect(isReviewStatus({ status: 'not_started', attempt_version: null, available: 'false' })).toBe(false)
    expect(isReviewStatus({ status: 'not_started', attempt_version: null, available: null })).toBe(false)
  })

  it('不把旧总结形状当成复盘', () => {
    // schema_version=1 的 V1 三栏总结、以及 0–100 的旧四维都不满足复盘契约。
    expect(isReviewStatus({
      status: 'completed', attempt_version: 1, schema_version: 1,
      content: {
        strengths: [{ round_no: 1, point: 'a', reason: 'b' }],
        issues: [{ round_no: 1, point: 'c', reason: 'd' }],
        suggestions: [{ round_no: 1, action: 'e', example: 'f' }],
      },
    })).toBe(false)
    expect(isReviewStatus({
      status: 'completed', attempt_version: 1, schema_version: 2,
      content: { scores: { argumentation: 80, rebuttal: 70, logic: 60, presentation: 50 } },
    })).toBe(false)
  })
})

describe('createReviewApi', () => {
  it('读取复盘状态使用 GET 且不触发写操作', async () => {
    respondWith(completed())
    const api = createReviewApi(client())
    await expect(api.get('7')).resolves.toEqual(completed())
    expect(fetcher).toHaveBeenCalledOnce()
    const [url, init] = fetcher.mock.calls[0]!
    expect(String(url)).toBe('/api/v1/debates/7/review')
    expect(init?.method ?? 'GET').toBe('GET')
  })

  it('显式生成与显式重试使用 POST 与严格请求体', async () => {
    respondWith(completed())
    const api = createReviewApi(client())
    await api.request('7')
    expect(fetcher.mock.calls[0]?.[1]?.method).toBe('POST')
    expect(fetcher.mock.calls[0]?.[1]?.body).toBe('{}')
    await api.request('7', { retry: true, expected_attempt_version: 2 })
    expect(fetcher.mock.calls[1]?.[1]?.body).toBe(JSON.stringify({ retry: true, expected_attempt_version: 2 }))
  })

  it('拒绝非法会话编号与非法重试版本', async () => {
    const api = createReviewApi(client())
    await expect(api.get('0')).rejects.toThrow()
    await expect(api.request('7', { retry: true, expected_attempt_version: 0 })).rejects.toThrow()
    await expect(api.request('7', { retry: true, expected_attempt_version: 1.5 })).rejects.toThrow()
    expect(fetcher).not.toHaveBeenCalled()
  })

  it('生成返回值本身也要通过契约校验', async () => {
    respondWith({ status: 'not_started', attempt_version: null })
    const api = createReviewApi(client())
    await expect(api.request('7')).rejects.toThrow()
  })

  it('失败响应映射为受控错误并保留业务码', async () => {
    fetcher.mockImplementation(async () => failure(409, 'attempt_version_conflict'))
    const api = createReviewApi(client())
    await expect(api.request('7', { retry: true, expected_attempt_version: 1 })).rejects.toMatchObject({ code: 'attempt_version_conflict' })
  })
})
