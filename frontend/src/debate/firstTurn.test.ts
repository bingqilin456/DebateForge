import { afterEach, describe, expect, it, vi } from 'vitest'
import { createFirstTurnState } from './firstTurn'
import { ApiError } from '../api/client'
import type { Debate, DebateDetail, DebateApi, SummaryContent, Turn } from '../api/debate'

// 主夹具沿用三轮 quick_3，使既有的三轮状态机用例保持原语义；
// 四轮标准赛制的分支由文件末尾的专门用例覆盖。
const debate: Debate = {
  id: '123',
  topic: { id: '1', title: '开放考核', description: '学习与公平' },
  user_stance: 'pro',
  ai_stance: 'con',
  mode: 'quick_3',
  ai_style: 'logical',
  total_rounds: 3,
  status: 'active',
  completed_rounds: 0,
  created_at: '2026-11-09T00:00:00Z',
  updated_at: '2026-11-09T00:00:00Z',
}
const sampleSummary: SummaryContent = {
  strengths: [{ round_no: 1, point: '首轮立论明确', reason: '紧扣学习效果' }],
  issues: [{ round_no: 2, point: '二轮拆解不足', reason: '未回应评分标准' }],
  suggestions: [{ round_no: 3, action: '三轮总结先破后立', example: '先回应公平性，再强调能力培养' }],
}
const empty: DebateDetail = { debate, turns: [], summary_status: { status: 'not_started', attempt_version: null } }
function makeTurn(roundNo: number, status: Turn['status'] = 'completed', version = 1, content = `第${roundNo}轮发言`): Turn {
  const messages: Turn['messages'] = [{ id: `${roundNo * 10 + 1}`, role: 'user', content, created_at: debate.created_at }]
  if (status === 'completed') {
    messages.push({ id: `${roundNo * 10 + 2}`, role: 'assistant', content: `第${roundNo}轮反驳`, created_at: debate.created_at })
  }
  return {
    id: `${roundNo + 1}`,
    round_no: roundNo,
    client_request_id: `550e8400-e29b-41d4-a716-44665544000${roundNo}`,
    status,
    attempt_version: version,
    error_code: status === 'failed' ? 'model_timeout' : null,
    messages,
  }
}
const generating: DebateDetail = { ...empty, turns: [makeTurn(1, 'generating', 1, '原文')] }

function fixture() {
  const api: DebateApi = {
    topics: vi.fn().mockResolvedValue([debate.topic]),
    list: vi.fn().mockResolvedValue({ items: [], total: 0, page: 1, page_size: 10 }),
    create: vi.fn().mockResolvedValue(debate),
    detail: vi.fn().mockResolvedValue(empty),
    send: vi.fn().mockImplementation(async (_id: string, content: string, _key: string, roundNo = 1) => ({
      turn: makeTurn(roundNo, 'completed', 1, content),
      completed_rounds: roundNo,
      query_url: '/api/v1/debates/123',
    })),
    retryTurn: vi.fn().mockResolvedValue({
      turn: makeTurn(1, 'completed', 2, '原文'),
      completed_rounds: 1,
      query_url: '/api/v1/debates/123',
    }),
    summary: vi.fn().mockResolvedValue({ status: 'not_started', attempt_version: null }),
    requestSummary: vi.fn().mockResolvedValue({
      status: 'completed',
      attempt_version: 1,
      schema_version: 1,
      content: sampleSummary,
    }),
  }
  const state = createFirstTurnState(api)
  return { api, state }
}

afterEach(() => { vi.useRealTimers() })

describe('三轮状态机、重试与总结恢复', () => {
  it('创建不调用生成；连续完成三轮后自动请求一次总结，并禁止第四轮提交', async () => {
    const { api, state } = fixture()
    await state.start('1', 'pro')
    expect(api.send).not.toHaveBeenCalled()

    const first = state.submit('第一轮观点')
    await state.submit('并发重复点击')
    await first
    expect(api.send).toHaveBeenCalledOnce()
    expect(state.detail.value?.debate.completed_rounds).toBe(1)
    expect(state.nextRound.value).toBe(2)

    await state.submit('第二轮观点')
    expect(api.send).toHaveBeenCalledTimes(2)
    expect(vi.mocked(api.send).mock.calls[1]?.[3]).toBe(2)
    expect(state.detail.value?.debate.completed_rounds).toBe(2)
    expect(state.nextRound.value).toBe(3)
    expect(api.requestSummary).not.toHaveBeenCalled()

    await state.submit('第三轮观点')
    expect(api.send).toHaveBeenCalledTimes(3)
    expect(vi.mocked(api.send).mock.calls[2]?.[3]).toBe(3)
    expect(state.detail.value?.debate.completed_rounds).toBe(3)
    expect(state.detail.value?.debate.status).toBe('completed')
    expect(state.nextRound.value).toBeNull()
    expect(api.requestSummary).toHaveBeenCalledOnce()
    expect(api.requestSummary).toHaveBeenCalledWith('123', { retry: false, totalRounds: 3 })
    expect(state.detail.value?.summary_status.status).toBe('completed')

    await state.submit('第四轮越界')
    expect(api.send).toHaveBeenCalledTimes(3)
  })

  it('按 Unicode 码点校验，全空白与 2001 码点不请求，2000 emoji 可发送', async () => {
    const { api, state } = fixture()
    await state.start('1', 'con')
    await state.submit('  \n')
    await state.submit('😀'.repeat(2001))
    expect(api.send).not.toHaveBeenCalled()
    await state.submit('😀'.repeat(2000))
    expect(api.send).toHaveBeenCalledOnce()
  })

  it('丢失生成响应先查详情；不自动重新生成，查询失败仍锁定原文', async () => {
    const { api, state } = fixture()
    vi.mocked(api.send).mockRejectedValue(new ApiError('network', '网络失败'))
    vi.mocked(api.detail).mockRejectedValue(new ApiError('network', '查询失败'))
    await state.start('1', 'pro')
    await state.submit('原文')
    expect(api.detail).toHaveBeenCalledWith('123')
    expect(state.uncertain.value).toBe(true)
    await state.submit('换键危险')
    expect(api.send).toHaveBeenCalledOnce()
    expect(state.draft.value).toBe('原文')
  })

  it('生成中只每 2 秒查询，120 秒停止，保留稍后查询入口', async () => {
    vi.useFakeTimers()
    const { api, state } = fixture()
    vi.mocked(api.detail).mockResolvedValue(generating)
    await state.open('123')
    await vi.advanceTimersByTimeAsync(120_000)
    expect(api.detail).toHaveBeenCalledTimes(61)
    await vi.advanceTimersByTimeAsync(10_000)
    expect(api.detail).toHaveBeenCalledTimes(61)
    expect(state.notice.value).toContain('稍后')
    expect(api.send).not.toHaveBeenCalled()
    state.dispose()
  })

  it('卸载清除轮询且迟到响应不能带回旧用户数据', async () => {
    const { api, state } = fixture()
    let finish!: (value: DebateDetail) => void
    vi.mocked(api.detail).mockReturnValue(new Promise(resolve => { finish = resolve }))
    const opening = state.open('123')
    state.dispose()
    finish(generating)
    await opening
    expect(state.detail.value).toBeNull()
    expect(state.busy.value).toBe(false)
  })

  it('已失败轮次保留原文且禁止通过 submit 跳轮；显式重试携带所见 expected_attempt_version，遇 409 只查询不重发', async () => {
    const { api, state } = fixture()
    const failedV1: DebateDetail = {
      debate: { ...debate, completed_rounds: 1 },
      turns: [makeTurn(1, 'completed', 1, '首轮'), makeTurn(2, 'failed', 1, '二轮原文')],
      summary_status: { status: 'not_started', attempt_version: null },
    }
    const failedV2: DebateDetail = {
      ...failedV1,
      turns: [makeTurn(1, 'completed', 1, '首轮'), makeTurn(2, 'failed', 2, '二轮原文')],
    }
    vi.mocked(api.detail).mockResolvedValueOnce(failedV1).mockResolvedValueOnce(failedV2)
    vi.mocked(api.retryTurn).mockRejectedValueOnce(new ApiError(
      'http',
      '重试版本已变化，请查询最新状态后再操作。',
      { status: 409, code: 'attempt_version_conflict' },
    ))

    await state.open('123')
    await state.submit('试图跳过失败轮')
    expect(api.send).not.toHaveBeenCalled()
    expect(state.notice.value).toContain('失败')

    await state.retryTurn()
    expect(api.retryTurn).toHaveBeenCalledOnce()
    expect(api.retryTurn).toHaveBeenCalledWith('123', '3', 1)
    expect(api.detail).toHaveBeenCalledTimes(2)
    expect(state.detail.value?.turns[1]?.attempt_version).toBe(2)
    expect(state.notice.value).toContain('重试版本已变化')

    vi.mocked(api.retryTurn).mockResolvedValueOnce({
      turn: makeTurn(2, 'completed', 3, '二轮原文'),
      completed_rounds: 2,
      query_url: '/api/v1/debates/123',
    })
    await state.retryTurn()
    expect(api.retryTurn).toHaveBeenCalledTimes(2)
    expect(api.retryTurn).toHaveBeenLastCalledWith('123', '3', 2)
    expect(state.detail.value?.debate.completed_rounds).toBe(2)
  })

  it('刷新 completed 会话：not_started 自动补发一次总结；failed 只展示手动重试且 409 后只查询不自动重发', async () => {
    const { api, state } = fixture()
    const completedNotStarted: DebateDetail = {
      debate: { ...debate, status: 'completed', completed_rounds: 3 },
      turns: [makeTurn(1), makeTurn(2), makeTurn(3)],
      summary_status: { status: 'not_started', attempt_version: null },
    }
    const completedFailedV1: DebateDetail = {
      ...completedNotStarted,
      summary_status: { status: 'failed', attempt_version: 1, error_code: 'model_error' },
    }
    const completedFailedV2: DebateDetail = {
      ...completedNotStarted,
      summary_status: { status: 'failed', attempt_version: 2, error_code: 'model_timeout' },
    }

    // 1) 打开 completed + not_started：自动补发一次，若失败转 failed 绝不循环调用
    vi.mocked(api.detail).mockResolvedValueOnce(completedNotStarted).mockResolvedValueOnce(completedFailedV1)
    vi.mocked(api.requestSummary).mockRejectedValueOnce(new ApiError('http', 'AI 回复生成失败，请查询会话状态。', { status: 502, code: 'model_error' }))
    await state.open('123')
    expect(api.requestSummary).toHaveBeenCalledOnce()
    expect(api.requestSummary).toHaveBeenCalledWith('123', { retry: false, totalRounds: 3 })
    expect(state.detail.value?.summary_status).toEqual(completedFailedV1.summary_status)
    expect(state.canRetrySummary.value).toBe(true)

    // 2) 再次刷新已 failed 会话：不自动请求总结
    vi.mocked(api.detail).mockReset()
    vi.mocked(api.detail).mockResolvedValue(completedFailedV1)
    await state.refresh()
    expect(api.requestSummary).toHaveBeenCalledOnce()
    expect(state.detail.value?.summary_status.attempt_version).toBe(1)

    // 3) 手动重试总结：携带当前所见版本 1；遇 409 只重查状态更新为版本 2，不自动换版本重发
    vi.mocked(api.requestSummary).mockRejectedValueOnce(new ApiError(
      'http',
      '重试版本已变化，请查询最新状态后再操作。',
      { status: 409, code: 'attempt_version_conflict' },
    ))
    vi.mocked(api.detail).mockResolvedValue(completedFailedV2)
    const callsBeforeRetry = vi.mocked(api.requestSummary).mock.calls.length
    await state.retrySummary()
    const retryCall = vi.mocked(api.requestSummary).mock.calls[callsBeforeRetry]?.[1] as {
      retry: boolean; expectedAttemptVersion?: number; totalRounds?: number
    }
    // 断言“这一次重试携带了用户所见版本 1”，而不是队列末尾的调用。
    expect(retryCall).toMatchObject({ retry: true, expectedAttemptVersion: 1, totalRounds: 3 })
    // 一次 409 之后不得自动重发第二次，只允许查询状态。
    expect(api.requestSummary).toHaveBeenCalledTimes(callsBeforeRetry + 1)
    expect(state.detail.value?.summary_status.attempt_version).toBe(2)

    // 4) 用户再次点击手动重试：使用新版本 2 成功完成
    vi.mocked(api.requestSummary).mockResolvedValueOnce({
      status: 'completed',
      attempt_version: 3,
      schema_version: 1,
      content: sampleSummary,
    })
    await state.retrySummary()
    expect(api.requestSummary).toHaveBeenCalledTimes(3)
    expect(api.requestSummary).toHaveBeenLastCalledWith('123', { retry: true, expectedAttemptVersion: 2, totalRounds: 3 })
    expect(state.detail.value?.summary_status.status).toBe('completed')
  })

  it('确认未落库后仅由用户重发原文与原键，保留 429 等失败反馈', async () => {
    const { api, state } = fixture()
    vi.mocked(api.send).mockRejectedValueOnce(new ApiError('http', '请在 60 秒后重试。', { status: 429, code: 'rate_limited' }))
    await state.start('1', 'pro')
    await state.submit(' 原文 ')
    expect(state.notice.value).toContain('60 秒')
    expect(state.canSend.value).toBe(true)
    await state.submit('篡改内容')
    expect(vi.mocked(api.send).mock.calls[1]).toEqual(vi.mocked(api.send).mock.calls[0])
  })

  it('恢复生成中会话后轮询读到已完成结果便停止，无自动生成', async () => {
    vi.useFakeTimers()
    const { api, state } = fixture()
    vi.mocked(api.detail).mockResolvedValueOnce(generating).mockResolvedValue(empty)
    await state.open('123')
    const completed = {
      ...generating,
      debate: { ...debate, completed_rounds: 1 },
      turns: [{
        ...generating.turns[0]!,
        status: 'completed' as const,
        messages: [...generating.turns[0]!.messages, { id: '4', role: 'assistant' as const, content: '已保存', created_at: debate.created_at }],
      }],
    }
    vi.mocked(api.detail).mockResolvedValue(completed)
    await vi.advanceTimersByTimeAsync(2000)
    expect(state.detail.value?.turns[0]?.messages[1]?.content).toBe('已保存')
    await vi.advanceTimersByTimeAsync(120_000)
    expect(api.detail).toHaveBeenCalledTimes(2)
    expect(api.send).not.toHaveBeenCalled()
    state.dispose()
  })

  it.each(['\u0085', '\u0085\u{FEFF}\u{2000}\n', '\u{FEFF}', '\u{3000}'])('拒绝后端视为空白的输入 %#，不绑定原文且仍能修改后提交', async content => {
    const { api, state } = fixture()
    await state.start('1', 'pro')
    await state.submit(content)
    expect(api.send).not.toHaveBeenCalled()
    expect(state.draft.value).toBe('')
    await state.submit('\u0085有效论点\u{FEFF}')
    expect(api.send).toHaveBeenCalledWith('123', '\u0085有效论点\u{FEFF}', expect.any(String), 1, 3)
  })

  it('首轮提交返回生成中后启动查询，120 秒内不重发生成', async () => {
    vi.useFakeTimers()
    const { api, state } = fixture()
    vi.mocked(api.send).mockResolvedValue({ turn: generating.turns[0]!, completed_rounds: 0, query_url: '/api/v1/debates/123' })
    vi.mocked(api.detail).mockResolvedValue(generating)
    await state.start('1', 'pro')
    await state.submit('原文')
    expect(api.detail).not.toHaveBeenCalled()
    await vi.advanceTimersByTimeAsync(1999)
    expect(api.detail).not.toHaveBeenCalled()
    await vi.advanceTimersByTimeAsync(1)
    expect(api.detail).toHaveBeenCalledWith('123')
    await vi.advanceTimersByTimeAsync(118_000)
    expect(api.detail).toHaveBeenCalledTimes(60)
    await vi.advanceTimersByTimeAsync(10_000)
    expect(api.detail).toHaveBeenCalledTimes(60)
    expect(api.send).toHaveBeenCalledOnce()
    expect(state.canSend.value).toBe(false)
    expect(state.notice.value).toContain('稍后')
    state.dispose()
  })
})

// 四轮标准赛制是新的生产默认；以下用例锁定“轮数由 total_rounds 驱动”这一约束，
// 防止将来有人把上限重新写死成 3。
describe('四轮标准赛制', () => {
  const standard4: Debate = { ...debate, mode: 'standard_4', total_rounds: 4 }

  function fourRoundFixture() {
    const detailAt = (completed: number): DebateDetail => ({
      debate: { ...standard4, completed_rounds: completed, status: completed >= 4 ? 'completed' : 'active' },
      turns: Array.from({ length: completed }, (_, i) => makeTurn(i + 1, 'completed')),
      summary_status: completed >= 4
        ? { status: 'completed', attempt_version: 1, schema_version: 1, content: sampleSummary }
        : { status: 'not_started', attempt_version: null },
    })
    const api: DebateApi = {
      topics: vi.fn().mockResolvedValue([standard4.topic]),
      list: vi.fn().mockResolvedValue({ items: [], total: 0, page: 1, page_size: 10 }),
      create: vi.fn().mockResolvedValue(standard4),
      detail: vi.fn().mockImplementation(async () => detailAt(0)),
      send: vi.fn().mockImplementation(async (_id: string, content: string, _key: string, roundNo = 1) => ({
        turn: makeTurn(roundNo, 'completed', 1, content),
        completed_rounds: roundNo,
        query_url: '/api/v1/debates/123',
      })),
      retryTurn: vi.fn().mockResolvedValue({
        turn: makeTurn(1, 'completed', 2, '原文'),
        completed_rounds: 1,
        query_url: '/api/v1/debates/123',
      }),
      requestSummary: vi.fn().mockResolvedValue({ status: 'completed', attempt_version: 1, schema_version: 1, content: sampleSummary }),
      summary: vi.fn().mockResolvedValue({ status: 'not_started', attempt_version: null }),
    }
    return { api, state: createFirstTurnState(api) }
  }

  it('允许推进到第 4 轮，且第 4 轮完成后才请求总结', async () => {
    const { api, state } = fourRoundFixture()
    await state.start('1', 'pro')
    expect(state.detail.value?.debate.total_rounds).toBe(4)

    for (let round = 1; round <= 4; round += 1) {
      expect(state.nextRound.value).toBe(round)
      await state.submit(`第 ${round} 轮发言`)
      // 轮次上限作为第 5 个参数传给 send，证明上限来自会话而非常量。
      expect(vi.mocked(api.send).mock.calls[round - 1]?.[4]).toBe(4)
      if (round < 4) expect(api.requestSummary).not.toHaveBeenCalled()
    }
    expect(state.nextRound.value).toBeNull()
    expect(api.requestSummary).toHaveBeenCalledOnce()
  })

  it('四轮未完成时不请求总结', async () => {
    const { api, state } = fourRoundFixture()
    await state.start('1', 'pro')
    for (let round = 1; round <= 3; round += 1) await state.submit(`第 ${round} 轮发言`)
    expect(state.detail.value?.debate.status).toBe('active')
    expect(state.nextRound.value).toBe(4)
    expect(api.requestSummary).not.toHaveBeenCalled()
  })

  it('三轮会话不会因为全局上限放宽而接受第 4 轮', async () => {
    const { api, state } = fixture()
    await state.start('1', 'pro')
    for (let round = 1; round <= 3; round += 1) await state.submit(`第 ${round} 轮发言`)
    expect(state.nextRound.value).toBeNull()
    expect(state.canSend.value).toBe(false)
    expect(vi.mocked(api.send).mock.calls.every(call => call[4] === 3)).toBe(true)
  })
})