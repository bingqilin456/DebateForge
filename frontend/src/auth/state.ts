import { reactive, readonly } from 'vue'
import { ApiError, createApiClient } from '../api/client'
import { createAuthApi } from '../api/auth'
import type { AuthCredentials, AuthSession, User } from '../types/api'

type AuthStatus = 'unknown' | 'anonymous' | 'authenticated'
interface AuthSnapshot {
  status: AuthStatus
  user: User | null
  pending: boolean
}

// 每个实例独立持有内存凭据；生产入口和表单应使用同一个 authState。
export function createAuthState() {
  const state = reactive<AuthSnapshot>({ status: 'unknown', user: null, pending: false })
  let csrfToken: string | null = null
  let version = 0
  let restoring: Promise<void> | null = null

  function clearSession() {
    version++
    csrfToken = null
    state.user = null
    state.status = 'anonymous'
  }
  function acceptSession(session: AuthSession) {
    version++
    csrfToken = session.csrf_token
    state.user = session.user
    state.status = 'authenticated'
  }

  const client = createApiClient({
    getCsrfToken: () => csrfToken,
    getAuthVersion: () => version,
    onUnauthenticated: clearSession,
  })
  const api = createAuthApi(client)

  // 不排队、不自动重发认证写操作，避免登录/退出的 Set-Cookie 响应相互覆盖。
  async function mutate<T>(operation: () => Promise<T>): Promise<T> {
    if (state.pending) throw new ApiError('busy', '认证操作正在进行，请等待完成。')
    state.pending = true
    version++
    try { return await operation() } finally { state.pending = false }
  }

  function restore(): Promise<void> {
    // 写操作已有明确结果处理；此时再查询旧 Cookie 会引入无意义的状态竞争。
    if (state.pending) return Promise.resolve()
    if (restoring) return restoring
    const startedVersion = version
    const pending = (async () => {
      try {
        const session = await api.me()
        if (version === startedVersion) acceptSession(session)
      } catch (error) {
        // 有效 401 已由 client 清理；503、超时、代理 HTML 等保留状态并交给入口提示。
        if (!(error instanceof ApiError && error.status === 401 && error.code === 'unauthenticated')) throw error
      }
    })()
    // 返回 finally 产生的同一个 Promise：保留原错误给调用者，且失败后允许显式重试。
    restoring = pending.finally(() => { restoring = null })
    return restoring
  }

  return {
    state: readonly(state),
    client,
    restore,
    register: api.register,
    login(input: AuthCredentials): Promise<User> {
      return mutate(async () => {
        const session = await api.login(input)
        acceptSession(session)
        return session.user
      })
    },
    logout(): Promise<void> {
      return mutate(async () => {
        await api.logout()
        clearSession()
      })
    },
  }
}

export type AuthState = ReturnType<typeof createAuthState>
export const authState = createAuthState()
