import { ApiError } from './client'

const SSE_FRAME_MAX_BYTES = 64 * 1024
const SSE_TOTAL_MAX_BYTES = 2 * 1024 * 1024

// 只负责增量分帧，不执行重连或 POST。id/retry 字段不作为业务恢复依据。
export async function readSSE(body: ReadableStream<Uint8Array>, event: (name: string, data: unknown) => boolean | void, signal?: AbortSignal): Promise<void> {
  const reader = body.getReader()
  const decoder = new TextDecoder('utf-8', { fatal: true })
  let line = '', name = '', data: string[] = [], skipLF = false, stopped = false
  let frameBytes = 0, totalBytes = 0
  const invalid = () => new ApiError('format', '流式数据格式异常，请查询已保存状态。')
  const cancelled = () => new ApiError('network', '流式连接已中止，请查询已保存状态。')
  const abort = () => { void reader.cancel().catch(() => {}) }
  signal?.addEventListener('abort', abort, { once: true })
  function finishLine() {
    if (!line) {
      if (data.length) {
        let value: unknown
        try { value = JSON.parse(data.join('\n')) } catch { throw invalid() }
        stopped = event(name || 'message', value) === true
      }
      name = ''; data = []; frameBytes = 0
    } else if (!line.startsWith(':')) {
      const colon = line.indexOf(':')
      const field = colon < 0 ? line : line.slice(0, colon)
      let value = colon < 0 ? '' : line.slice(colon + 1)
      if (value.startsWith(' ')) value = value.slice(1)
      if (field === 'event') name = value
      if (field === 'data') data.push(value)
    }
    line = ''
  }
  function accept(text: string) {
    for (const char of text) {
      if (skipLF) { skipLF = false; if (char === '\n') continue }
      const point = char.codePointAt(0)!
      frameBytes += point < 0x80 ? 1 : point < 0x800 ? 2 : point < 0x10000 ? 3 : 4
      if (frameBytes > SSE_FRAME_MAX_BYTES) throw invalid()
      if (char === '\r' || char === '\n') {
        finishLine(); skipLF = char === '\r'
        if (stopped) return
      } else line += char
    }
  }
  try {
    if (signal?.aborted) throw cancelled()
    while (!stopped) {
      const chunk = await reader.read()
      if (signal?.aborted) throw cancelled()
      if (chunk.done) { accept(decoder.decode()); break }
      totalBytes += chunk.value.byteLength
      if (totalBytes > SSE_TOTAL_MAX_BYTES) throw invalid()
      accept(decoder.decode(chunk.value, { stream: true }))
    }
    // EOF 不补空行、不把半事件当作提交成功；由调用方检查是否收到完整 done。
  } catch (error) {
    if (error instanceof ApiError) throw error
    throw invalid()
  } finally {
    signal?.removeEventListener('abort', abort)
    void reader.cancel().catch(() => {})
    reader.releaseLock()
  }
}
