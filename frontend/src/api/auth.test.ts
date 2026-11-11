import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { createApiClient } from './client'
import { createAuthApi } from './auth'

const fetchMock = vi.fn<typeof fetch>()
const user = { id: '42', username: 'alice' }
const credentials = { username: ' Alice ', password: '  example-password  ' }
function response(data: unknown, status = 200) {
  return new Response(JSON.stringify({ data, request_id: 'req-auth' }), {
    status, headers: { 'Content-Type': 'application/json', 'X-Request-ID': 'req-auth' },
  })
}
beforeEach(() => { vi.stubGlobal('fetch', fetchMock); fetchMock.mockReset() })
afterEach(() => vi.unstubAllGlobals())

describe('认证 API 契约', () => {
  it('注册使用 201，归一化用户名但不修改密码，不发送旧 CSRF', async () => {
    fetchMock.mockResolvedValue(response(user, 201))
    const auth = createAuthApi(createApiClient({ getCsrfToken: () => 'old-csrf' }))
    await expect(auth.register(credentials)).resolves.toEqual(user)
    const [path, init] = fetchMock.mock.calls[0]!
    expect(path).toBe('/api/v1/auth/register')
    expect(init?.method).toBe('POST')
    expect(JSON.parse(String(init?.body))).toEqual({ ...credentials, username: 'alice' })
    expect(new Headers(init?.headers).has('X-CSRF-Token')).toBe(false)
  })

  it('登录解包 user 和 csrf_token，不将 Session 当作 JSON token', async () => {
    fetchMock.mockResolvedValue(response({ user, csrf_token: 'session-csrf' }))
    await expect(createAuthApi(createApiClient()).login(credentials)).resolves.toEqual({ user, csrf_token: 'session-csrf' })
    expect(fetchMock.mock.calls[0]![0]).toBe('/api/v1/auth/login')
    expect(new Headers(fetchMock.mock.calls[0]![1]?.headers).has('X-CSRF-Token')).toBe(false)
  })

  it('me 使用 GET 恢复当前 Session；没有请求体', async () => {
    fetchMock.mockResolvedValue(response({ user, csrf_token: 'restored-csrf' }))
    await createAuthApi(createApiClient()).me()
    expect(fetchMock.mock.calls[0]![0]).toBe('/api/v1/auth/me')
    expect(fetchMock.mock.calls[0]![1]).toMatchObject({ method: 'GET', body: undefined })
  })

  it('logout 使用无请求体 POST 和内存 CSRF，接受 204', async () => {
    fetchMock.mockResolvedValue(new Response(null, { status: 204 }))
    await expect(createAuthApi(createApiClient({ getCsrfToken: () => 'logout-csrf' })).logout()).resolves.toBeUndefined()
    const [path, init] = fetchMock.mock.calls[0]!
    expect(path).toBe('/api/v1/auth/logout')
    expect(init).toMatchObject({ method: 'POST', body: undefined })
    expect(new Headers(init?.headers).get('X-CSRF-Token')).toBe('logout-csrf')
  })

  it.each([
    { user, csrf_token: '' }, { user }, { user, csrf_token: 'bad\r\ntoken' },
    { user: { id: 42, username: 'alice' }, csrf_token: 'csrf' },
    { user: { id: '0', username: 'alice' }, csrf_token: 'csrf' },
    { user: { id: '042', username: 'alice' }, csrf_token: 'csrf' },
    { user: { id: '18446744073709551616', username: 'alice' }, csrf_token: 'csrf' },
    { user: { id: '42', username: '<script>bad</script>' }, csrf_token: 'csrf' },
  ])('拒绝不符合身份/CSRF 契约的数据 %#', async (data) => {
    fetchMock.mockResolvedValue(response(data))
    await expect(createAuthApi(createApiClient()).me()).rejects.toMatchObject({ kind: 'format' })
  })

  it.each(['register', 'login'] as const)('非法凭据不会从 %s 发往服务器', async (method) => {
    const auth = createAuthApi(createApiClient())
    await expect(auth[method]({ username: 'ab', password: credentials.password })).rejects.toMatchObject({ kind: 'format' })
    expect(fetchMock).not.toHaveBeenCalled()
  })
  it('BIGINT UNSIGNED 最大值始终作为字符串保留', async () => {
    const maxUser = { id: '18446744073709551615', username: 'alice' }
    fetchMock.mockResolvedValue(response(maxUser, 201))
    await expect(createAuthApi(createApiClient()).register(credentials)).resolves.toEqual(maxUser)
  })
})
