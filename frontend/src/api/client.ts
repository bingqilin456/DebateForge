type ErrorKind = 'http' | 'network' | 'timeout' | 'format'

// 只向页面传递受控提示，不展示上游原始错误正文。
export class ApiError extends Error {
  constructor(public readonly kind: ErrorKind, message: string) {
    super(message)
    this.name = 'ApiError'
  }
}

// 超时包含读取响应体，避免收到 Header 后无限等待半条 JSON。
export async function requestJson(url: string, timeoutMs = 3000): Promise<unknown> {
  const controller = new AbortController()
  const timer = setTimeout(() => controller.abort(), timeoutMs)
  try {
    const response = await fetch(url, {
      headers: { Accept: 'application/json' },
      credentials: 'same-origin',
      cache: 'no-store',
      signal: controller.signal,
    })
    if (!response.ok) {
      throw new ApiError('http', `服务返回 HTTP ${response.status}，请确认后端已启动后重试。`)
    }
    const contentType = response.headers.get('content-type')?.split(';')[0]?.trim().toLowerCase()
    if (contentType !== 'application/json') {
      throw new ApiError('format', '服务返回了非 JSON 内容，请检查本地代理配置。')
    }
    try {
      const value: unknown = await response.json()
      return value
    } catch (error) {
      if (controller.signal.aborted) throw error
      throw new ApiError('format', '服务返回了无效 JSON，请检查后端响应。')
    }
  } catch (error) {
    if (controller.signal.aborted) throw new ApiError('timeout', '检查超时，请确认后端已启动后重试。')
    if (error instanceof ApiError) throw error
    throw new ApiError('network', '无法连接服务，请确认前后端已启动后重试。')
  } finally {
    clearTimeout(timer)
  }
}
