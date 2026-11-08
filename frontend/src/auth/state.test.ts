import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { createAuthState } from './state'

const fetchMock = vi.fn<typeof fetch>()
const credentials = { username: 'alice', password: 'example-password' }
const alice = { id: '42', username: 'alice' }
const bob = { id: '43', username: 'bob' }
function envelope(data: unknown, status = 200) {
  return new Response(JSON.stringify({ data, request_id: 'req-state' }), {
    status, headers: { 'Content-Type': 'application/json', 'X-Request-ID': 'req-state' },
  })
}
function session(user = alice, token = 'csrf-alice') { return envelope({ user, csrf_token: token }) }
function failure(status: number, code: string) {
  return new Response(JSON.stringify({ error: { code, message: '受控服务端提示' }, request_id: 'req-state' }), {
    status, headers: { 'Content-Type': 'application/json', 'X-Request-ID': 'req-state' },
  })
}
function deferred() {
  let resolve!: (response: Response) => void
  const promise = new Promise<Response>((done) => { resolve = done })
  return { promise, resolve }
}
beforeEach(() => { vi.stubGlobal('fetch', fetchMock); fetchMock.mockReset() })
afterEach(() => vi.unstubAllGlobals())

describe('仅内存认证状态', () => {
  it('初始未知；不触碰 localStorage/sessionStorage；登录后共享 client 自动用 CSRF', async () => {
    const access = vi.fn(() => { throw new Error('禁止访问浏览器存储') })
    vi.stubGlobal('localStorage', { getItem: access, setItem: access, removeItem: access })
    vi.stubGlobal('sessionStorage', { getItem: access, setItem: access, removeItem: access })
    const auth = createAuthState()
    expect(auth.state).toMatchObject({ status: 'unknown', user: null, pending: false })
    fetchMock.mockResolvedValueOnce(session()).mockResolvedValueOnce(new Response(null, { status: 204 }))
    await auth.login(credentials)
    expect(auth.state).toMatchObject({ status: 'authenticated', user: alice })
    await auth.logout()
    expect(new Headers(fetchMock.mock.calls[1]![1]?.headers).get('X-CSRF-Token')).toBe('csrf-alice')
    expect(auth.state).toMatchObject({ status: 'anonymous', user: null, pending: false })
    expect(access).not.toHaveBeenCalled()
  })

  it('新实例不会继承另一个实例的用户或 CSRF，必须通过 Cookie/me 恢复', async () => {
    fetchMock.mockResolvedValueOnce(session()).mockResolvedValueOnce(session(alice, 'restored-csrf'))
    const first = createAuthState()
    await first.login(credentials)
    const refreshed = createAuthState()
    expect(refreshed.state.status).toBe('unknown')
    expect(refreshed.state.user).toBeNull()
    await expect(refreshed.logout()).rejects.toMatchObject({ kind: 'csrf' })
    await refreshed.restore()
    expect(refreshed.state.user).toEqual(alice)
    expect(fetchMock.mock.calls[1]![0]).toBe('/api/v1/auth/me')
  })

  it.each([
    () => failure(503, 'dependency_unavailable'),
    () => new Response('<html>proxy</html>', { status: 401, headers: { 'Content-Type': 'text/html' } }),
  ])('恢复遇到服务/代理故障不清除已有 Session %#', async (response) => {
    fetchMock.mockResolvedValueOnce(session()).mockResolvedValueOnce(response())
    const auth = createAuthState()
    await auth.login(credentials)
    await expect(auth.restore()).rejects.toBeDefined()
    expect(auth.state).toMatchObject({ status: 'authenticated', user: alice })
  })

  it('登录写操作期间不发起 me，以免读取旧 Cookie 干扰状态', async () => {
    const pending = deferred()
    fetchMock.mockReturnValueOnce(pending.promise)
    const auth = createAuthState()
    const loggingIn = auth.login(credentials)
    await auth.restore()
    expect(fetchMock).toHaveBeenCalledOnce()
    pending.resolve(session())
    await loggingIn
    expect(auth.state.status).toBe('authenticated')
  })
  it('注册不自动登录、不改变未知认证状态', async () => {
    fetchMock.mockResolvedValue(envelope(alice, 201))
    const auth = createAuthState()
    await expect(auth.register(credentials)).resolves.toEqual(alice)
    expect(auth.state.status).toBe('unknown')
    expect(fetchMock).toHaveBeenCalledOnce()
  })

  it('并发恢复只发一次 me；恢复后可以受保护写入', async () => {
    const pending = deferred()
    fetchMock.mockReturnValueOnce(pending.promise).mockResolvedValueOnce(new Response(null, { status: 204 }))
    const auth = createAuthState()
    const first = auth.restore()
    const second = auth.restore()
    expect(fetchMock).toHaveBeenCalledOnce()
    pending.resolve(session())
    await Promise.all([first, second])
    expect(auth.state.user).toEqual(alice)
    await auth.logout()
    expect(new Headers(fetchMock.mock.calls[1]![1]?.headers).get('X-CSRF-Token')).toBe('csrf-alice')
  })

  it('me 的 401 unauthenticated 恢复为匿名，不当成网络故障', async () => {
    fetchMock.mockResolvedValue(failure(401, 'unauthenticated'))
    const auth = createAuthState()
    await expect(auth.restore()).resolves.toBeUndefined()
    expect(auth.state).toMatchObject({ status: 'anonymous', user: null })
  })

  it('恢复的 503/传输故障保持未知并抛错，允许显式恢复重试', async () => {
    fetchMock.mockResolvedValueOnce(failure(503, 'dependency_unavailable')).mockResolvedValueOnce(session())
    const auth = createAuthState()
    await expect(auth.restore()).rejects.toMatchObject({ status: 503 })
    expect(auth.state.status).toBe('unknown')
    expect(fetchMock).toHaveBeenCalledOnce()
    await auth.restore()
    expect(auth.state.status).toBe('authenticated')
  })

  it('登录凭据错误不抹掉已知 Session', async () => {
    fetchMock.mockResolvedValueOnce(session()).mockResolvedValueOnce(failure(401, 'invalid_credentials'))
    const auth = createAuthState()
    await auth.login(credentials)
    await expect(auth.login(credentials)).rejects.toMatchObject({ code: 'invalid_credentials' })
    expect(auth.state).toMatchObject({ status: 'authenticated', user: alice, pending: false })
  })

  it('受保护 API 的有效 401 立即清除用户和 CSRF', async () => {
    fetchMock.mockResolvedValueOnce(session()).mockResolvedValueOnce(failure(401, 'unauthenticated'))
    const auth = createAuthState()
    await auth.login(credentials)
    await expect(auth.client.request('/api/v1/example', { expectedStatus: 200, validate: (value): value is string => typeof value === 'string' }))
      .rejects.toMatchObject({ code: 'unauthenticated' })
    expect(auth.state).toMatchObject({ status: 'anonymous', user: null })
    await expect(auth.logout()).rejects.toMatchObject({ kind: 'csrf' })
    expect(fetchMock).toHaveBeenCalledTimes(2)
  })

  it('退出失败 503 不假装已注销，仍保留可恢复的认证状态', async () => {
    fetchMock.mockResolvedValueOnce(session()).mockResolvedValueOnce(failure(503, 'dependency_unavailable'))
    const auth = createAuthState()
    await auth.login(credentials)
    await expect(auth.logout()).rejects.toMatchObject({ status: 503 })
    expect(auth.state).toMatchObject({ status: 'authenticated', user: alice, pending: false })
  })

  it('退出收到 401 时清除状态，但保留错误供入口按码处理', async () => {
    fetchMock.mockResolvedValueOnce(session()).mockResolvedValueOnce(failure(401, 'unauthenticated'))
    const auth = createAuthState()
    await auth.login(credentials)
    await expect(auth.logout()).rejects.toMatchObject({ status: 401, code: 'unauthenticated' })
    expect(auth.state.status).toBe('anonymous')
  })

  it.each([false, true])('旧 me 迟到（401=%s）不会覆盖随后登录的新 Session', async (unauthenticated) => {
    const pending = deferred()
    fetchMock.mockReturnValueOnce(pending.promise).mockResolvedValueOnce(session(bob, 'csrf-bob'))
    const auth = createAuthState()
    const restoring = auth.restore()
    await auth.login({ username: 'bob', password: credentials.password })
    pending.resolve(unauthenticated ? failure(401, 'unauthenticated') : session())
    await restoring
    expect(auth.state).toMatchObject({ status: 'authenticated', user: bob })
  })

  it('认证写操作进行中拒绝第二次写操作，防止 Cookie 响应乱序', async () => {
    const pending = deferred()
    fetchMock.mockReturnValueOnce(pending.promise)
    const auth = createAuthState()
    const loggingIn = auth.login(credentials)
    expect(auth.state.pending).toBe(true)
    await expect(auth.logout()).rejects.toMatchObject({ kind: 'busy' })
    expect(fetchMock).toHaveBeenCalledOnce()
    pending.resolve(session())
    await loggingIn
    expect(auth.state.pending).toBe(false)
  })
})
