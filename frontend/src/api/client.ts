export type ErrorKind = 'http' | 'network' | 'timeout' | 'format' | 'csrf' | 'busy'
interface ErrorDetails {
  status?: number
  code?: string
  requestId?: string
  retryAfterSeconds?: number
}

// 保留可分支的协议元数据，但不保存或展示上游原始正文、异常及凭据。
export class ApiError extends Error {
  readonly status?: number
  readonly code?: string
  readonly requestId?: string
  readonly retryAfterSeconds?: number

  constructor(public readonly kind: ErrorKind, message: string, details: ErrorDetails = {}) {
    super(message)
    this.name = 'ApiError'
    this.status = details.status
    this.code = details.code
    this.requestId = details.requestId
    this.retryAfterSeconds = details.retryAfterSeconds
  }
}

export const DEFAULT_TIMEOUT_MS = 5_000
export const GENERATION_TIMEOUT_MS = 70_000

// 计时覆盖 fetch 和响应体读取；即使读取器迟迟不结束，也必须按总预算返回。
async function withinTimeout<T>(operation: (signal: AbortSignal) => Promise<T>, timeoutMs: number, health = false): Promise<T> {
  if (!Number.isFinite(timeoutMs) || timeoutMs <= 0) throw new ApiError('format', '请求超时配置无效。')
  const controller = new AbortController()
  let timer: ReturnType<typeof setTimeout> | undefined
  const timeout = new Promise<never>((_, reject) => {
    timer = setTimeout(() => {
      controller.abort()
      reject(new ApiError('timeout', health ? '检查超时，请确认后端已启动后重试。' : '请求超时，请稍后手动重试。'))
    }, timeoutMs)
  })
  try {
    return await Promise.race([operation(controller.signal), timeout])
  } catch (error) {
    if (error instanceof ApiError) throw error
    if (controller.signal.aborted) {
      throw new ApiError('timeout', health ? '检查超时，请确认后端已启动后重试。' : '请求超时，请稍后手动重试。')
    }
    throw new ApiError('network', health ? '无法连接服务，请确认前后端已启动后重试。' : '无法连接服务，请稍后手动重试。')
  } finally {
    clearTimeout(timer)
  }
}

async function readJson(response: Response): Promise<unknown> {
  const contentType = response.headers.get('content-type')?.split(';')[0]?.trim().toLowerCase()
  if (contentType !== 'application/json') {
    throw new ApiError('format', '服务返回了非 JSON 内容，请检查本地代理配置。')
  }
  try {
    const value: unknown = await response.json()
    return value
  } catch (error) {
    if (!(error instanceof SyntaxError)) throw error
    throw new ApiError('format', '服务返回了无效 JSON，请检查后端响应。')
  }
}

// 保留 PB 的无业务信封健康请求、默认 3 秒超时及受控 HTTP 提示。
export async function requestJson(url: string, timeoutMs = 3000): Promise<unknown> {
  return withinTimeout(async (signal) => {
    const response = await fetch(url, {
      headers: { Accept: 'application/json' }, credentials: 'same-origin', cache: 'no-store', signal,
    })
    if (!response.ok) throw new ApiError('http', `服务返回 HTTP ${response.status}，请确认后端已启动后重试。`)
    return readJson(response)
  }, timeoutMs, true)
}

interface ClientHooks {
  getCsrfToken?: () => string | null
  getAuthVersion?: () => number
  onUnauthenticated?: () => void
}
interface RequestOptions {
  method?: 'GET' | 'POST'
  body?: unknown
  // 只有注册、登录可设为 false；默认按受保护接口处理。
  authenticated?: boolean
  timeoutMs?: number
}
export interface JsonRequestOptions<T> extends RequestOptions {
  expectedStatus: 200 | 201 | 202 | readonly (200 | 201 | 202)[]
  validate: (data: unknown) => data is T
}
export interface EmptyRequestOptions extends RequestOptions {
  expectedStatus: 204
}

export function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null && !Array.isArray(value)
}

function requestId(value: unknown, response: Response): string {
  if (!isRecord(value) || typeof value.request_id !== 'string' || !value.request_id.trim()
    || response.headers.get('X-Request-ID') !== value.request_id) {
    throw new ApiError('format', '服务响应标识不符合约定，请检查后端接口。')
  }
  return value.request_id
}

const errorMessages: Record<string, string> = {
  invalid_request: '输入不符合要求，请检查后重试。',
  invalid_credentials: '用户名或密码错误。',
  unauthenticated: '登录状态已失效，请重新登录。',
  csrf_failed: '安全校验失败，请刷新登录状态后重试。',
  username_conflict: '用户名已被使用，请更换用户名。',
  not_found: '请求的资源不存在或不可访问。',
  method_not_allowed: '请求方式不受支持。',
  request_too_large: '请求内容过大，请缩短后重试。',
  rate_limited: '请求过于频繁，请稍后手动重试。',
  generation_busy: '生成服务繁忙，请稍后手动重试。',
  dependency_unavailable: '服务暂时不可用，请稍后手动重试。',
  internal_error: '服务发生错误，请稍后手动重试。',
  model_error: 'AI 回复生成失败，请查询会话状态。',
  model_unavailable: 'AI 服务暂不可用，请查询会话状态。',
  model_timeout: 'AI 生成超时，请查询会话状态。',
  round_conflict: '会话状态已变化，请查询最新状态。',
  idempotency_conflict: '请求内容发生冲突，请查询最新状态。',
  turn_failed: '本轮生成失败，请查询会话状态。',
  attempt_version_conflict: '重试版本已变化，请查询最新状态后再操作。',
}

function businessError(value: unknown, response: Response, id: string): ApiError {
  if (!isRecord(value) || !isRecord(value.error) || typeof value.error.code !== 'string'
    || !value.error.code.trim() || typeof value.error.message !== 'string' || !value.error.message.trim()) {
    throw new ApiError('format', '服务错误响应不符合约定，请检查后端接口。')
  }
  const header = response.headers.get('Retry-After')
  const seconds = header !== null && /^\d+$/.test(header) ? Number(header) : NaN
  const retryAfterSeconds = Number.isSafeInteger(seconds) && seconds >= 0 ? seconds : undefined
  const message = Object.hasOwn(errorMessages, value.error.code) ? errorMessages[value.error.code]! : '请求失败，请稍后手动重试。'
  return new ApiError('http', retryAfterSeconds === undefined ? message : `${message}请在 ${retryAfterSeconds} 秒后重试。`, {
    status: response.status, code: value.error.code, requestId: id, retryAfterSeconds,
  })
}

// 只接收同源 /api/v1/ 路径，避免向任意地址发送 Session 写操作的 CSRF token。
export function createApiClient(hooks: ClientHooks = {}) {
  async function request<T>(path: string, options: JsonRequestOptions<T>): Promise<T>
  async function request(path: string, options: EmptyRequestOptions): Promise<void>
  async function request<T>(path: string, options: JsonRequestOptions<T> | EmptyRequestOptions): Promise<T | void> {
    if (!path.startsWith('/api/v1/') || path.includes('\\') || path.includes('#')
      || !new URL(path, 'http://api.invalid').pathname.startsWith('/api/v1/')) {
      throw new ApiError('format', '业务请求必须使用同源 /api/v1/ 路径。')
    }
    const method = options.method ?? 'GET'
    const authenticated = options.authenticated !== false
    const authVersion = hooks.getAuthVersion?.()
    const headers = new Headers({ Accept: 'application/json' })
    if (method === 'POST' && authenticated) {
      const token = hooks.getCsrfToken?.()
      if (!token) throw new ApiError('csrf', '登录状态尚未恢复，请先恢复登录或重新登录。')
      headers.set('X-CSRF-Token', token)
    }
    let body: string | undefined
    if (options.body !== undefined) {
      if (method !== 'POST') throw new ApiError('format', 'GET 请求不能携带正文。')
      try { body = JSON.stringify(options.body) } catch { throw new ApiError('format', '请求内容无法编码为 JSON。') }
      if (body === undefined || new TextEncoder().encode(body).length > 32 * 1024) {
        throw new ApiError('format', '请求内容无效或超过 32 KiB。')
      }
      headers.set('Content-Type', 'application/json')
    }
    return withinTimeout(async (signal) => {
      const response = await fetch(path, {
        method, body, headers, credentials: 'same-origin', cache: 'no-store', redirect: 'error', signal,
      })
      if (response.status === 204) {
        if (options.expectedStatus !== 204) throw new ApiError('format', '服务返回了非预期的空响应。')
        return
      }
      const value = await readJson(response)
      const id = requestId(value, response)
      if (!response.ok) {
        const error = businessError(value, response, id)
        // 请求开始后若已切换认证代次，旧请求的 401 不能清掉新 Session。
        if (authenticated && error.status === 401 && error.code === 'unauthenticated'
          && authVersion === hooks.getAuthVersion?.()) hooks.onUnauthenticated?.()
        throw error
      }
      const expectedStatuses: readonly number[] = typeof options.expectedStatus === 'number'
        ? [options.expectedStatus] : options.expectedStatus
      if (options.expectedStatus === 204 || !expectedStatuses.includes(response.status)
        || !isRecord(value) || !('data' in value) || !options.validate(value.data)) {
        throw new ApiError('format', '服务成功响应不符合约定，请检查后端接口。')
      }
      return value.data
    }, options.timeoutMs ?? DEFAULT_TIMEOUT_MS)
  }
  return { request }
}

export type ApiClient = ReturnType<typeof createApiClient>
