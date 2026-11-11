import { ApiError, isRecord, type ApiClient } from './client'
import { normalizeCredentials, validateCredentials } from '../auth/credentials'
import type { AuthCredentials, AuthSession, User } from '../types/api'

function isUser(value: unknown): value is User {
  return isRecord(value) && typeof value.id === 'string' && /^[1-9]\d{0,19}$/.test(value.id)
    && BigInt(value.id) <= 18446744073709551615n
    && typeof value.username === 'string' && /^[a-z0-9_]{3,32}$/.test(value.username)
}
function isAuthSession(value: unknown): value is AuthSession {
  return isRecord(value) && isUser(value.user) && typeof value.csrf_token === 'string'
    && value.csrf_token.trim().length > 0 && !/[\r\n]/.test(value.csrf_token)
}
function credentials(input: AuthCredentials): AuthCredentials {
  const error = validateCredentials(input)
  if (error) throw new ApiError('format', error)
  return normalizeCredentials(input)
}
function publicUser(user: User): User { return { id: user.id, username: user.username } }
function publicSession(session: AuthSession): AuthSession {
  return { user: publicUser(session.user), csrf_token: session.csrf_token }
}

// 这里只定义认证协议，不保存认证状态，也不尝试读取/设置 Session Cookie。
export function createAuthApi(client: ApiClient) {
  return {
    async register(input: AuthCredentials): Promise<User> {
      return publicUser(await client.request('/api/v1/auth/register', {
        method: 'POST', authenticated: false, body: credentials(input), expectedStatus: 201, validate: isUser,
      }))
    },
    async login(input: AuthCredentials): Promise<AuthSession> {
      return publicSession(await client.request('/api/v1/auth/login', {
        method: 'POST', authenticated: false, body: credentials(input), expectedStatus: 200, validate: isAuthSession,
      }))
    },
    async me(): Promise<AuthSession> {
      return publicSession(await client.request('/api/v1/auth/me', { expectedStatus: 200, validate: isAuthSession }))
    },
    logout(): Promise<void> {
      return client.request('/api/v1/auth/logout', { method: 'POST', expectedStatus: 204 })
    },
  }
}
