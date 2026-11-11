import { reactive, readonly } from 'vue'
import { isRecord } from '../../api/client'
import type { User } from '../../types/api'

type Operation = 'register' | 'login' | 'me' | 'logout'
export type PreviewScenario = 'success' | 'rejected' | 'rate_limited' | 'unavailable' | 'network'
const routes = new Map<string, Operation>([
  ['/api/v1/auth/register', 'register'], ['/api/v1/auth/login', 'login'],
  ['/api/v1/auth/me', 'me'], ['/api/v1/auth/logout', 'logout'],
])

// 等待可取消，避免页面超时后又偷偷建立模拟会话；这里不产生真实网络 I/O。
function wait(ms: number, signal?: AbortSignal | null): Promise<void> {
  if (signal?.aborted) return Promise.reject(new DOMException('预览请求已取消', 'AbortError'))
  return new Promise((resolve, reject) => {
    const timer = setTimeout(() => { signal?.removeEventListener('abort', abort); resolve() }, ms)
    function abort() {
      clearTimeout(timer)
      signal?.removeEventListener('abort', abort)
      reject(new DOMException('预览请求已取消', 'AbortError'))
    }
    signal?.addEventListener('abort', abort, { once: true })
  })
}

// 仅由独立开发预览入口安装。密码不参与认证、不保存账号，刷新即清空模拟会话。
export function createPreviewAuthMock(origin: string) {
  const controls = reactive({ scenario: 'success' as PreviewScenario, delayMs: 1_500 })
  const state = reactive({
    pending: 0,
    counts: { register: 0, login: 0, me: 0, logout: 0 },
    lastRequest: '尚无模拟请求',
    lastStatus: null as number | 'network' | 'aborted' | null,
  })
  let sessionUser: User | null = null
  let sequence = 0

  const mockFetch: typeof fetch = async (input, init) => {
    const url = new URL(input instanceof Request ? input.url : String(input), origin)
    const operation = routes.get(url.pathname)
    // 不回退真实 fetch：错误路径/其它 origin 也不能误触后端或模型。
    if (url.origin !== origin || !operation) throw new TypeError('本地预览禁止真实业务请求')
    const method = (init?.method ?? (input instanceof Request ? input.method : 'GET')).toUpperCase()
    const signal = init?.signal ?? (input instanceof Request ? input.signal : undefined)
    const scenario = controls.scenario
    const requestId = `preview-${++sequence}`
    state.counts[operation]++
    state.pending++
    state.lastRequest = `${method} ${url.pathname}`
    state.lastStatus = null
    function response(status: number, data?: unknown, code?: string) {
      state.lastStatus = status
      const headers: Record<string, string> = { 'X-Request-ID': requestId, 'Cache-Control': 'no-store' }
      if (status === 204) return new Response(null, { status, headers })
      headers['Content-Type'] = 'application/json'
      if (status === 429) headers['Retry-After'] = '15'
      const body = code ? { error: { code, message: '本地模拟响应，不是真实认证。' }, request_id: requestId }
        : { data, request_id: requestId }
      return new Response(JSON.stringify(body), { status, headers })
    }
    try {
      await wait(controls.delayMs, signal)
      signal?.throwIfAborted()
      if (method !== (operation === 'me' ? 'GET' : 'POST')) return response(405, undefined, 'method_not_allowed')
      if (operation === 'me') {
        return sessionUser ? response(200, { user: sessionUser, csrf_token: 'preview-csrf-only' })
          : response(401, undefined, 'unauthenticated')
      }
      if (operation === 'logout') {
        if (!sessionUser) return response(401, undefined, 'unauthenticated')
        if (new Headers(init?.headers).get('X-CSRF-Token') !== 'preview-csrf-only') return response(403, undefined, 'csrf_failed')
        sessionUser = null
        return response(204)
      }
      if (scenario === 'network') { state.lastStatus = 'network'; throw new TypeError('模拟网络故障') }
      if (scenario === 'rate_limited') return response(429, undefined, 'rate_limited')
      if (scenario === 'unavailable') return response(503, undefined, 'dependency_unavailable')
      if (scenario === 'rejected') {
        return operation === 'login' ? response(401, undefined, 'invalid_credentials')
          : response(409, undefined, 'username_conflict')
      }
      let body: unknown
      try { body = JSON.parse(typeof init?.body === 'string' ? init.body : '') }
      catch { return response(400, undefined, 'invalid_request') }
      if (!isRecord(body) || typeof body.username !== 'string') return response(400, undefined, 'invalid_request')
      const user: User = { id: '42', username: body.username }
      if (operation === 'register') return response(201, user)
      sessionUser = user
      return response(200, { user, csrf_token: 'preview-csrf-only' })
    } catch (error) {
      if (signal?.aborted) state.lastStatus = 'aborted'
      throw error
    } finally { state.pending-- }
  }
  return { controls, state: readonly(state), fetch: mockFetch }
}

export type PreviewAuthMock = ReturnType<typeof createPreviewAuthMock>
