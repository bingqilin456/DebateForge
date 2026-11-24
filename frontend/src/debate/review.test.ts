import { beforeEach, describe, expect, it, vi } from 'vitest'
import { ApiError } from '../api/client'
import { createReviewState } from './review'
import type { ReviewApi } from '../api/review'
import type { ReviewStatus } from '../types/api'

/**
 * V2-13 复盘状态机：失败刷新恢复、显式重试成功、并发/重复点击、生成中查询。
 * 全部使用桩 API，不发真实请求、不触碰任何数据库。
 */

function quote(quote = '我方认为开放式考核更能检验真实能力') {
  return { message_id: '101', quote, start_cp: 0, end_cp: [...quote].length, resolved: true }
}

function dimension(key: string, label: string, level: 1 | 2 | 3 | 4 | null) {
  return level === null
    ? { key, label, level: null, level_label: null, basis: '依据', improvement: '', insufficient_reason: '本场缺少观察机会', quote: null }
    : { key, label, level, level_label: { 1: '起步', 2: '发展中', 3: '较稳', 4: '充分' }[level], basis: '依据', improvement: '改进', insufficient_reason: null, quote: quote() }
}

const completed: ReviewStatus = {
  status: 'completed', attempt_version: 1, schema_version: 1,
  content: {
    dimensions: [
      dimension('argument_structure', '论证结构', 3),
      dimension('responsiveness', '回应针对性', null),
      dimension('evidence_use', '证据使用', 2),
      dimension('concept_scope', '概念与范围', null),
    ] as never,
    key_issues: [{ text: '主要争点', quote: quote() }],
    strengths: [{ text: '优点', quote: quote() }],
    improvements: [{ text: '改进', quote: quote() }],
  },
}
const failed: ReviewStatus = { status: 'failed', attempt_version: 1, error_code: 'model_timeout' }
const generating: ReviewStatus = { status: 'generating', attempt_version: 1 }

function api(overrides: Partial<ReviewApi> = {}): ReviewApi {
  return { get: vi.fn(), request: vi.fn(), ...overrides } as ReviewApi
}

describe('createReviewState', () => {
  let stub: ReviewApi

  beforeEach(() => { stub = api() })

  it('刷新能恢复失败状态，且不改写比赛完成状态', async () => {
    vi.mocked(stub.get).mockResolvedValue(failed)
    const state = createReviewState(stub)
    await state.open('7')
    expect(state.status.value.status).toBe('failed')
    expect(state.failedMessage.value).toContain('比赛完成状态不受影响')
    expect(state.canRetry.value).toBe(true)
    expect(state.canGenerate.value).toBe(false)
    state.dispose()
  })

  it('失败后显式重试成功并恢复内容', async () => {
    vi.mocked(stub.get).mockResolvedValueOnce(failed).mockResolvedValue(completed)
    vi.mocked(stub.request).mockResolvedValue(completed)
    const state = createReviewState(stub)
    await state.open('7')
    await state.retry()
    expect(stub.request).toHaveBeenCalledWith('7', { retry: true, expected_attempt_version: 1 })
    expect(state.status.value.status).toBe('completed')
    expect(state.status.value.status === 'completed' && state.status.value.content.dimensions.length).toBe(4)
    state.dispose()
  })

  it('首次生成只在用户显式调用时发生', async () => {
    vi.mocked(stub.get).mockResolvedValue({ status: 'not_started', attempt_version: null })
    vi.mocked(stub.request).mockResolvedValue(completed)
    const state = createReviewState(stub)
    await state.open('7')
    expect(stub.request).not.toHaveBeenCalled()
    await state.generate()
    expect(stub.request).toHaveBeenCalledWith('7', { retry: false })
    expect(state.status.value.status).toBe('completed')
    state.dispose()
  })

  it('重复点击只触发一次生成，后续调用被 busy 拦下', async () => {
    vi.mocked(stub.get).mockResolvedValue({ status: 'not_started', attempt_version: null })
    let finish: (value: ReviewStatus) => void = () => {}
    vi.mocked(stub.request).mockReturnValue(new Promise<ReviewStatus>(resolve => { finish = resolve }))
    const state = createReviewState(stub)
    await state.open('7')
    const first = state.generate()
    const second = state.generate()
    await second
    expect(stub.request).toHaveBeenCalledOnce()
    finish(completed)
    await first
    expect(state.status.value.status).toBe('completed')
    state.dispose()
  })

  it('失败时重试不会被当成自动重发；只重新读取权威状态', async () => {
    vi.mocked(stub.get).mockResolvedValue(failed)
    vi.mocked(stub.request).mockRejectedValue(new ApiError('http', '复盘生成失败', { status: 502, code: 'model_error' }))
    const state = createReviewState(stub)
    await state.open('7')
    await state.retry()
    expect(stub.request).toHaveBeenCalledOnce()
    // 失败后只做一次 GET 刷新，不自动再次 POST。
    expect(vi.mocked(stub.get).mock.calls.length).toBe(2)
    expect(state.status.value.status).toBe('failed')
    state.dispose()
  })

  it('生成中会做有限查询并最终停在最终状态', async () => {
    vi.useFakeTimers()
    try {
      vi.mocked(stub.get).mockResolvedValueOnce(generating).mockResolvedValue(completed)
      const state = createReviewState(stub)
      await state.open('7')
      expect(state.generating.value).toBe(true)
      await vi.advanceTimersByTimeAsync(2_100)
      expect(stub.get).toHaveBeenCalledTimes(2)
      expect(state.status.value.status).toBe('completed')
      state.dispose()
    } finally {
      vi.useRealTimers()
    }
  })

  it('刷新失败时保留可读错误，不伪造成功状态', async () => {
    vi.mocked(stub.get).mockRejectedValue(new ApiError('network', '网络不可用'))
    const state = createReviewState(stub)
    await state.open('7')
    expect(state.loaded.value).toBe(false)
    expect(state.notice.value).toContain('网络不可用')
    expect(state.status.value.status).toBe('not_started')
    state.dispose()
  })

  it('切换会话会清空上一场状态与定时器', async () => {
    vi.mocked(stub.get).mockResolvedValueOnce(completed).mockResolvedValueOnce(failed)
    const state = createReviewState(stub)
    await state.open('7')
    expect(state.status.value.status).toBe('completed')
    await state.open('8')
    expect(state.status.value.status).toBe('failed')
    state.dispose()
  })

  it('available:false 时标记不可用并拒绝生成', async () => {
    vi.mocked(stub.get).mockResolvedValue({ status: 'not_started', attempt_version: null, available: false })
    const state = createReviewState(stub)
    await state.open('7')
    expect(state.unavailable.value).toBe(true)
    expect(state.canGenerate.value).toBe(false)
    await state.generate()
    expect(stub.request).not.toHaveBeenCalled()
    state.dispose()
  })

  it('省略 available 的旧响应仍按可用处理', async () => {
    vi.mocked(stub.get).mockResolvedValue({ status: 'not_started', attempt_version: null })
    vi.mocked(stub.request).mockResolvedValue(completed)
    const state = createReviewState(stub)
    await state.open('7')
    expect(state.unavailable.value).toBe(false)
    expect(state.canGenerate.value).toBe(true)
    await state.generate()
    expect(stub.request).toHaveBeenCalledWith('7', { retry: false })
    state.dispose()
  })
})
