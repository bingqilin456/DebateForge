import { afterEach, describe, expect, it, vi } from 'vitest'
import { createFirstTurnState } from './firstTurn'
import { ApiError } from '../api/client'
import type { Debate, DebateDetail, DebateApi } from '../api/debate'

const debate: Debate = { id: '123', topic: { id: '1', title: '开放考核', description: '学习与公平' }, user_stance: 'pro', ai_stance: 'con', status: 'active', completed_rounds: 0, created_at: '2026-11-09T00:00:00Z', updated_at: '2026-11-09T00:00:00Z' }
const empty: DebateDetail = { debate, turns: [], summary_status: { status: 'not_started', attempt_version: null } }
const generating: DebateDetail = { ...empty, turns: [{ id: '2', round_no: 1, client_request_id: '550e8400-e29b-41d4-a716-446655440000', status: 'generating', attempt_version: 1, error_code: null, messages: [{ id: '3', role: 'user', content: '原文', created_at: debate.created_at }] }] }
function fixture() {
  const api: DebateApi = { topics: vi.fn().mockResolvedValue([{ id: '1', title: '开放考核', description: '学习与公平' }]), create: vi.fn().mockResolvedValue(debate), detail: vi.fn().mockResolvedValue(empty), send: vi.fn().mockResolvedValue({ turn: { ...generating.turns[0]!, status: 'completed', messages: [...generating.turns[0]!.messages, { id: '4', role: 'assistant', content: '反驳', created_at: debate.created_at }] }, completed_rounds: 1, query_url: '/api/v1/debates/123' }) }
  const state = createFirstTurnState(api)
  return { api, state }
}
afterEach(() => { vi.useRealTimers() })
describe('首轮状态与恢复', () => {
  it('创建不调用生成；重复提交只发送一次并展示持久化结果', async () => {
    const { api, state } = fixture()
    await state.start('1', 'pro')
    expect(api.send).not.toHaveBeenCalled()
    const first = state.submit('原文')
    await state.submit('第二次')
    await first
    expect(api.send).toHaveBeenCalledOnce()
    expect(state.detail.value?.debate.completed_rounds).toBe(1)
    expect(state.detail.value?.turns[0]?.messages[1]?.content).toBe('反驳')
    await state.submit('第二轮')
    expect(api.send).toHaveBeenCalledOnce()
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
  it('已失败的首轮保留原文并禁止冒充 T09 重试', async () => {
    const { api, state } = fixture()
    vi.mocked(api.detail).mockResolvedValue({ ...generating, turns: [{ ...generating.turns[0]!, status: 'failed', error_code: 'model_timeout' }] })
    await state.open('123')
    await state.submit('偷偷重试')
    expect(state.detail.value?.turns[0]?.messages[0]?.content).toBe('原文')
    expect(api.send).not.toHaveBeenCalled()
    expect(state.notice.value).toContain('失败')
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
    const completed = { ...generating, debate: { ...debate, completed_rounds: 1 }, turns: [{ ...generating.turns[0]!, status: 'completed' as const, messages: [...generating.turns[0]!.messages, { id: '4', role: 'assistant' as const, content: '已保存', created_at: debate.created_at }] }] }
    vi.mocked(api.detail).mockResolvedValue(completed)
    await vi.advanceTimersByTimeAsync(2000)
    expect(state.detail.value?.turns[0]?.messages[1]?.content).toBe('已保存')
    await vi.advanceTimersByTimeAsync(120_000)
    expect(api.detail).toHaveBeenCalledTimes(2)
    expect(api.send).not.toHaveBeenCalled()
    state.dispose()
  })
  it.each(['\u0085', '\u0085\ufeff\u2000\n', '\ufeff', '\u3000'])('拒绝后端视为空白的输入 %#，不绑定原文且仍能修改后提交', async content => {
    const { api, state } = fixture()
    await state.start('1', 'pro')
    await state.submit(content)
    expect(api.send).not.toHaveBeenCalled()
    expect(state.draft.value).toBe('')
    await state.submit('\u0085有效论点\ufeff')
    expect(api.send).toHaveBeenCalledWith('123', '\u0085有效论点\ufeff', expect.any(String))
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
