// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { createRepracticeApi, isRepracticeList, isRepracticeVersion } from './repractice'
import { createApiClient } from './client'

/**
 * V2-08 局部重练契约测试：使用隔离 fetch 桩，不发真实请求。
 *
 * 重点覆盖：
 * - 三态版本的字段自洽（generating/completed/failed），失败版本不得伪装成已完成；
 * - available=false 的能力关闭形状可被正确读取；
 * - 提交/重试的请求体与路径、CSRF 头、超时分层；
 * - 空选段、空白改写、越界坐标在本地就被拒绝，不发请求。
 */

const fetcher = vi.fn<typeof fetch>()

function envelope(data: unknown, status = 200, requestId = 'req-repractice') {
  return new Response(JSON.stringify({ data, request_id: requestId }), {
    status,
    headers: { 'Content-Type': 'application/json', 'X-Request-ID': requestId },
  })
}

function respondWith(data: unknown, status = 200) {
  fetcher.mockImplementation(async () => envelope(data, status))
}

function failure(status: number, code: string) {
  return new Response(JSON.stringify({ error: { code, message: '受控提示' }, request_id: 'req-repractice' }), {
    status,
    headers: { 'Content-Type': 'application/json', 'X-Request-ID': 'req-repractice' },
  })
}

function client() {
  // 提交与重试都是受保护写操作：显式提供 CSRF token，避免命中未登录分支。
  return createApiClient({ getCsrfToken: () => 'csrf-repractice' })
}

beforeEach(() => {
  fetcher.mockReset()
  vi.stubGlobal('fetch', fetcher)
})
afterEach(() => { vi.unstubAllGlobals() })

const source = { message_id: '202', start_cp: 0, end_cp: 3, quote: '甲😀乙' }

function version(overrides: Record<string, unknown> = {}) {
  return {
    id: '7',
    status: 'completed',
    attempt_version: 1,
    source,
    goal: '',
    rewrite: '补充了判断依据',
    feedback: { improved: true, change: '补了依据', reason: '理由更直接', new_problem: '' },
    error_code: null,
    restored_from: null,
    ...overrides,
  }
}

describe('局部重练状态校验', () => {
  it('接受三态版本与关闭态列表', () => {
    expect(isRepracticeVersion(version())).toBe(true)
    expect(isRepracticeVersion(version({ status: 'generating', feedback: null, error_code: null }))).toBe(true)
    expect(isRepracticeVersion(version({ status: 'failed', feedback: null, error_code: 'model_timeout' }))).toBe(true)
    expect(isRepracticeList({ available: false, items: [] })).toBe(true)
  })

  it('拒绝字段自相矛盾的版本', () => {
    // 失败版本不能带反馈；完成版本必须有理由；生成中不能带错误码。
    expect(isRepracticeVersion(version({ status: 'failed', error_code: null }))).toBe(false)
    expect(isRepracticeVersion(version({ status: 'failed', feedback: version().feedback }))).toBe(false)
    expect(isRepracticeVersion(version({ feedback: { improved: true, change: '', reason: '  ', new_problem: '' } }))).toBe(false)
    expect(isRepracticeVersion(version({ status: 'generating', error_code: 'model_error' }))).toBe(false)
    // 改写不能为空白；选段坐标必须有序；版本号必须为正整数。
    expect(isRepracticeVersion(version({ rewrite: '   ' }))).toBe(false)
    expect(isRepracticeVersion(version({ source: { ...source, start_cp: 3, end_cp: 3 } }))).toBe(false)
    expect(isRepracticeVersion(version({ attempt_version: 0 }))).toBe(false)
  })
})

describe('局部重练 API', () => {
  it('读取历史：GET 本场 repractice，返回 available 与按序版本', async () => {
    respondWith({ available: true, items: [version(), version({ id: '8', rewrite: '第二版' })] })
    const result = await createRepracticeApi(client()).list('123')
    expect(result.available).toBe(true)
    expect(result.items.map(item => item.id)).toEqual(['7', '8'])
    const [path, init] = fetcher.mock.calls[0]!
    expect(String(path)).toBe('/api/v1/debates/123/repractice')
    expect(init?.method ?? 'GET').toBe('GET')
  })

  it('提交改写：POST 且带 CSRF 与选中原文坐标', async () => {
    respondWith(version({ id: '9' }))
    const api = createRepracticeApi(client())
    const created = await api.submit('123', {
      client_request_id: 'req-1',
      message_id: '202',
      start_cp: 0,
      end_cp: 3,
      quote: '甲😀乙',
      goal: '补依据',
      rewrite: '补充了判断依据',
    })
    expect(created.id).toBe('9')
    const [path, init] = fetcher.mock.calls[0]!
    expect(String(path)).toBe('/api/v1/debates/123/repractice')
    expect(init?.method).toBe('POST')
    expect(new Headers(init?.headers).get('X-CSRF-Token')).toBe('csrf-repractice')
    expect(JSON.parse(String(init?.body))).toMatchObject({
      message_id: '202', start_cp: 0, end_cp: 3, quote: '甲😀乙', rewrite: '补充了判断依据',
    })
  })

  it('失败版本按已保存结果返回，不改写成错误', async () => {
    respondWith(version({ id: '9', status: 'failed', feedback: null, error_code: 'model_timeout' }))
    const created = await createRepracticeApi(client()).submit('123', {
      client_request_id: 'req-2',
      message_id: '202',
      start_cp: 0,
      end_cp: 3,
      quote: '甲😀乙',
      goal: '',
      rewrite: '改写',
    })
    expect(created.status).toBe('failed')
    expect(created.status === 'failed' && created.error_code).toBe('model_timeout')
  })

  it('缺失的原文坐标不会返回成功，不会创建没有选段的版本', async () => {
    // 空区间在客户端即被格式校验拒绝：不发请求，也不会产生“没有选段”的版本。
    await expect(createRepracticeApi(client()).submit('123', {
      client_request_id: 'req-3',
      message_id: '202',
      start_cp: 1,
      end_cp: 1,
      quote: '',
      goal: '',
      rewrite: '改写',
    })).rejects.toMatchObject({ kind: 'format' })
    expect(fetcher).not.toHaveBeenCalled()

    // 即便坐标看起来合法，服务端对空选段统一返回 400：客户端必须按错误抛出。
    fetcher.mockImplementation(async () => failure(400, 'invalid_request'))
    await expect(createRepracticeApi(client()).submit('123', {
      client_request_id: 'req-4',
      message_id: '202',
      start_cp: 0,
      end_cp: 1,
      quote: '',
      goal: '',
      rewrite: '改写',
    })).rejects.toMatchObject({ status: 400 })
  })

  it('重试必须带当前 attempt_version，路径包含版本编号', async () => {
    respondWith(version({ id: '9', attempt_version: 2 }))
    const api = createRepracticeApi(client())
    await api.retry('123', '9', { expected_attempt_version: 1 })
    const [path, init] = fetcher.mock.calls[0]!
    expect(String(path)).toBe('/api/v1/debates/123/repractice/9/retry')
    expect(init?.method).toBe('POST')
    expect(JSON.parse(String(init?.body))).toEqual({ expected_attempt_version: 1 })
    // 缺版本号在本地即拒绝，不发请求。
    await expect(api.retry('123', '9', { expected_attempt_version: 0 })).rejects.toMatchObject({ kind: 'format' })
    expect(fetcher).toHaveBeenCalledTimes(1)
  })
})
