import { createServer } from 'node:http'
import { once } from 'node:events'
import { afterAll, afterEach, beforeAll, beforeEach, describe, expect, it, vi } from 'vitest'
import { createApiClient, GENERATION_TIMEOUT_MS } from './client'

const nativeFetch = globalThis.fetch
const fetchMock = vi.fn<typeof fetch>()
const isResult = (value: unknown): value is { result: string } =>
  typeof value === 'object' && value !== null && 'result' in value && typeof value.result === 'string'
const ok = { expectedStatus: 200, validate: isResult } as const

function json(body: unknown, status = 200, extraHeaders: Record<string, string> = {}) {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json; charset=utf-8', 'X-Request-ID': 'req-test', ...extraHeaders },
  })
}
function success() { return json({ data: { result: 'ok' }, request_id: 'req-test' }) }
function failure(status: number, code: string, headers: Record<string, string> = {}) {
  return json({ error: { code, message: '不应直接展示的上游内容' }, request_id: 'req-test' }, status, headers)
}

beforeEach(() => { vi.stubGlobal('fetch', fetchMock); fetchMock.mockReset() })
afterEach(() => { vi.unstubAllGlobals(); vi.restoreAllMocks() })

describe('统一业务 client', () => {
  it('解包 data，使用同源 Cookie、no-store 和相对路径', async () => {
    fetchMock.mockResolvedValue(success())
    await expect(createApiClient().request('/api/v1/example', ok)).resolves.toEqual({ result: 'ok' })
    expect(fetchMock).toHaveBeenCalledOnce()
    const [path, init] = fetchMock.mock.calls[0]!
    expect(path).toBe('/api/v1/example')
    expect(init).toMatchObject({ method: 'GET', credentials: 'same-origin', cache: 'no-store' })
    expect(new Headers(init?.headers).get('Accept')).toBe('application/json')
    expect(new Headers(init?.headers).has('Content-Type')).toBe(false)
  })

  it('仅 Session 写操作附带内存 CSRF；Origin 由浏览器提供', async () => {
    fetchMock.mockResolvedValue(success())
    const client = createApiClient({ getCsrfToken: () => 'memory-token' })
    await client.request('/api/v1/example', { ...ok, method: 'POST', body: { text: '观点' } })
    const init = fetchMock.mock.calls[0]![1]!
    const headers = new Headers(init.headers)
    expect(headers.get('X-CSRF-Token')).toBe('memory-token')
    expect(headers.get('Content-Type')).toBe('application/json')
    expect(headers.has('Origin')).toBe(false)
    expect(init.body).toBe(JSON.stringify({ text: '观点' }))
  })

  it('注册/登录即使已有旧 token 也不加 CSRF 头', async () => {
    fetchMock.mockResolvedValue(success())
    await createApiClient({ getCsrfToken: () => 'old-token' }).request('/api/v1/auth/login', {
      ...ok, method: 'POST', authenticated: false, body: { username: 'alice', password: 'not-a-real-password' },
    })
    expect(new Headers(fetchMock.mock.calls[0]![1]?.headers).has('X-CSRF-Token')).toBe(false)
  })

  it('未恢复 CSRF 时在本地拒绝 Session 写操作，不发送请求', async () => {
    await expect(createApiClient().request('/api/v1/auth/logout', {
      method: 'POST', expectedStatus: 204,
    })).rejects.toMatchObject({ kind: 'csrf' })
    expect(fetchMock).not.toHaveBeenCalled()
  })

  it('204 无正文时不读取 JSON，也不要求 Content-Type', async () => {
    const response = new Response(null, { status: 204 })
    const readJson = vi.spyOn(response, 'json')
    fetchMock.mockResolvedValue(response)
    await expect(createApiClient({ getCsrfToken: () => 'csrf' }).request('/api/v1/auth/logout', {
      method: 'POST', expectedStatus: 204,
    })).resolves.toBeUndefined()
    expect(readJson).not.toHaveBeenCalled()
    expect(fetchMock.mock.calls[0]![1]?.body).toBeUndefined()
    expect(new Headers(fetchMock.mock.calls[0]![1]?.headers).has('Content-Type')).toBe(false)
  })

  it.each([
    () => new Response(null, { status: 204 }),
    () => json({ data: { result: 'ok' }, request_id: 'req-test' }, 201),
    () => json({ result: '没有信封' }),
    () => json({ data: { result: 1 }, request_id: 'req-test' }),
    () => json({ data: { result: 'ok' }, request_id: 'different-id' }),
    () => json({ data: { result: 'ok' }, request_id: '' }),
    () => new Response('{broken', { headers: { 'Content-Type': 'application/json' } }),
    () => new Response('<html>proxy detail</html>', { status: 503, headers: { 'Content-Type': 'text/html' } }),
    () => json({ error: 'raw internal detail' }, 401),
  ])('拒绝非契约/伪成功响应 %#，不当成业务错误或自动重试', async (response) => {
    fetchMock.mockResolvedValue(response())
    const expired = vi.fn()
    await expect(createApiClient({ onUnauthenticated: expired }).request('/api/v1/example', ok))
      .rejects.toMatchObject({ kind: 'format' })
    expect(expired).not.toHaveBeenCalled()
    expect(fetchMock).toHaveBeenCalledOnce()
  })

  it('保留状态、业务码、request_id、Retry-After，只展示受控提示', async () => {
    fetchMock.mockResolvedValue(failure(429, 'rate_limited', { 'Retry-After': '12' }))
    await expect(createApiClient().request('/api/v1/example', ok)).rejects.toMatchObject({
      kind: 'http', status: 429, code: 'rate_limited', requestId: 'req-test', retryAfterSeconds: 12,
      message: expect.not.stringContaining('上游内容'),
    })
  })

  it.each(['-1', '1.5', 'tomorrow', '999999999999999999999999'])('忽略不合约的 Retry-After %s', async (value) => {
    fetchMock.mockResolvedValue(failure(429, 'rate_limited', { 'Retry-After': value }))
    await expect(createApiClient().request('/api/v1/example', ok)).rejects.toMatchObject({
      status: 429, retryAfterSeconds: undefined,
    })
  })

  // 429 三类原因各自有独立文案：分钟防刷可稍后重试，并发占用要等当前生成，
  // 日额度用尽只保留受控提示。日额度由服务端按 UTC 日重置，“明天再试”在中国
  // 时区 07:59 可能一分钟后即可用；Retry-After 只是额度窗口重开的秒数，
  // 所以该码不拼接“N 秒后重试”，但仍把秒数保留在 metadata（不自动重试）。
  it.each([
    ['rate_limited', '请求过于频繁，请稍后手动重试。'],
    ['generation_busy', '生成服务繁忙，请稍后手动重试。'],
    ['daily_quota_exhausted', '今日生成次数已用完，可查看历史。'],
  ])('429 无 Retry-After 时展示完整受控文案：%s', async (code, expected) => {
    fetchMock.mockResolvedValue(failure(429, code))
    await expect(createApiClient().request('/api/v1/example', ok)).rejects.toMatchObject({
      kind: 'http', status: 429, code, retryAfterSeconds: undefined, message: expected,
    })
  })

  it.each([
    ['rate_limited', '请求过于频繁，请稍后手动重试。请在 12 秒后重试。', 12],
    ['generation_busy', '生成服务繁忙，请稍后手动重试。请在 12 秒后重试。', 12],
    ['daily_quota_exhausted', '今日生成次数已用完，可查看历史。', 12],
  ])('429 带有效 Retry-After 时文案按业务码区分、秒数入 metadata：%s', async (code, expected, retryAfterSeconds) => {
    fetchMock.mockResolvedValue(failure(429, code, { 'Retry-After': '12' }))
    await expect(createApiClient().request('/api/v1/example', ok)).rejects.toMatchObject({
      kind: 'http', status: 429, code, retryAfterSeconds, message: expected,
    })
  })

  it('日额度用尽的 Retry-After 不合约时只忽略秒数，不改受控文案', async () => {
    fetchMock.mockResolvedValue(failure(429, 'daily_quota_exhausted', { 'Retry-After': 'tomorrow' }))
    await expect(createApiClient().request('/api/v1/example', ok)).rejects.toMatchObject({
      kind: 'http', status: 429, code: 'daily_quota_exhausted', retryAfterSeconds: undefined,
      message: '今日生成次数已用完，可查看历史。',
    })
  })

  it.each([
    [401, 'unauthenticated', true, 1],
    [401, 'invalid_credentials', false, 0],
    [401, 'unauthenticated', false, 0],
    [403, 'csrf_failed', true, 0],
    [503, 'dependency_unavailable', true, 0],
  ] as const)('仅受保护请求的 401 unauthenticated 通知登录失效：%s %s auth=%s', async (status, code, authenticated, calls) => {
    fetchMock.mockResolvedValue(failure(status, code))
    const expired = vi.fn()
    await expect(createApiClient({ onUnauthenticated: expired }).request('/api/v1/example', {
      ...ok, authenticated,
    })).rejects.toMatchObject({ kind: 'http', status, code })
    expect(expired).toHaveBeenCalledTimes(calls)
  })

  it('旧请求迟到的 401 不清除新登录状态', async () => {
    let finish!: (response: Response) => void
    let version = 0
    fetchMock.mockReturnValue(new Promise<Response>((resolve) => { finish = resolve }))
    const expired = vi.fn()
    const pending = createApiClient({ getAuthVersion: () => version, onUnauthenticated: expired })
      .request('/api/v1/example', ok)
    version++
    finish(failure(401, 'unauthenticated'))
    await expect(pending).rejects.toMatchObject({ code: 'unauthenticated' })
    expect(expired).not.toHaveBeenCalled()
  })

  it.each(['unknown_code', '__proto__', 'constructor', 'toString'])('未知错误码 %s 只使用兜底中文提示', async (code) => {
    fetchMock.mockResolvedValue(failure(500, code))
    await expect(createApiClient().request('/api/v1/example', ok)).rejects.toMatchObject({
      kind: 'http', code, message: '请求失败，请稍后手动重试。',
    })
  })
  it('网络错误不泄露原始异常、不自动重试', async () => {
    fetchMock.mockRejectedValue(new Error('private transport detail'))
    await expect(createApiClient().request('/api/v1/example', ok)).rejects.toMatchObject({
      kind: 'network', message: expect.not.stringContaining('private'),
    })
    expect(fetchMock).toHaveBeenCalledOnce()
  })

  it.each(['https://other.example/api/v1/test', '//other.example/api/v1/test', '/api/v1/../../outside', '/health/live'])
  ('业务 client 拒绝越出同源 API 范围的路径 %s', async (path) => {
    await expect(createApiClient().request(path, ok)).rejects.toMatchObject({ kind: 'format' })
    expect(fetchMock).not.toHaveBeenCalled()
  })

  it.each([200, 201, 202] as const)('允许 A 显式声明多个合法成功状态 %s，不把生成中 202 当作失败', async (status) => {
    fetchMock.mockResolvedValue(json({ data: { result: 'ok' }, request_id: 'req-test' }, status))
    await expect(createApiClient().request('/api/v1/example', {
      expectedStatus: [200, 201, 202], validate: isResult,
    })).resolves.toEqual({ result: 'ok' })
    expect(fetchMock).toHaveBeenCalledOnce()
  })
  it('普通业务默认短超时 5 秒；A 可显式选择生成契约的 70 秒', async () => {
    fetchMock.mockImplementation(async () => success())
    const timer = vi.spyOn(globalThis, 'setTimeout')
    const client = createApiClient()
    await client.request('/api/v1/example', ok)
    expect(timer).toHaveBeenCalledWith(expect.any(Function), 5_000)
    await client.request('/api/v1/example', { ...ok, timeoutMs: GENERATION_TIMEOUT_MS })
    expect(GENERATION_TIMEOUT_MS).toBe(70_000)
    expect(timer).toHaveBeenCalledWith(expect.any(Function), 70_000)
  })
})

// 用真实 HTTP 分块而不是仅 Mock Promise，验证收到响应头后仍受总超时约束。
const server = createServer((req, res) => {
  const body = JSON.stringify({ data: { result: 'ok' }, request_id: 'req-test' })
  const sendHeaders = () => res.writeHead(200, { 'Content-Type': 'application/json', 'X-Request-ID': 'req-test' })
  if (req.url?.endsWith('/slow-body')) { sendHeaders(); res.write(body.slice(0, 12)) }
  const timer = setTimeout(() => {
    if (!res.headersSent) sendHeaders()
    res.end(res.headersSent && req.url?.endsWith('/slow-body') ? body.slice(12) : body)
  }, 200)
  res.on('close', () => clearTimeout(timer))
})
let baseUrl: string
beforeAll(async () => {
  server.listen(0, '127.0.0.1')
  await once(server, 'listening')
  const address = server.address()
  if (!address || typeof address === 'string') throw new Error('测试 HTTP 监听失败')
  baseUrl = `http://127.0.0.1:${address.port}`
})
afterAll(async () => {
  server.closeAllConnections()
  await new Promise<void>((resolve, reject) => server.close((error) => error ? reject(error) : resolve()))
})

describe('业务总超时（真实 HTTP）', () => {
  it.each(['slow-headers', 'slow-body'])('中止 %s 且只请求一次', async (path) => {
    fetchMock.mockImplementation((input, init) => nativeFetch(new URL(String(input), baseUrl), init))
    await expect(createApiClient().request(`/api/v1/${path}`, { ...ok, timeoutMs: 30 }))
      .rejects.toMatchObject({ kind: 'timeout' })
    expect(fetchMock).toHaveBeenCalledOnce()
  })
})
