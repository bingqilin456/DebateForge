import { afterEach, describe, expect, it, vi } from 'vitest'
import { createFirstTurnState } from './firstTurn'
import { ApiError } from '../api/client'
import type { DebateApi, DebateDetail, Turn } from '../api/debate'
import type { StreamUpdate, TurnStreamResult } from '../types/api'

const time = '2026-11-15T00:00:00Z'
const original = ' 原文😀 '
const initial: DebateDetail = { debate: { id: '123', topic: { id: '1', title: '流式辩题', description: '说明' }, user_stance: 'pro', ai_stance: 'con', ai_style: 'logical', mode: 'standard_4', total_rounds: 4, status: 'active', completed_rounds: 0, created_at: time, updated_at: time }, turns: [], summary_status: { status: 'not_started', attempt_version: null } }
function turn(key: string, round = 1, status: Turn['status'] = 'completed', version = 1): Turn {
  const messages: Turn['messages'] = [{ id: String(round * 10), role: 'user', content: original, created_at: time }]
  if (status === 'completed') messages.push({ id: String(round * 10 + 1), role: 'assistant', content: '正式回复', created_at: time })
  return { id: String(round), round_no: round, client_request_id: key, status, attempt_version: version, error_code: status === 'failed' ? 'model_timeout' : null, messages }
}
function done(key: string, round = 1): TurnStreamResult { return { kind: 'done', data: { turn: turn(key, round), completed_rounds: round, status: round === 4 ? 'completed' : 'active' } } }
function deferred<T>() { let resolve!: (value: T) => void; let reject!: (reason: unknown) => void; const promise = new Promise<T>((yes, no) => { resolve = yes; reject = no }); return { promise, resolve, reject } }
const states: ReturnType<typeof createFirstTurnState>[] = []
function fixture() {
  const pending = deferred<TurnStreamResult>()
  let update: (event: StreamUpdate) => void = () => {}
  const stream = vi.fn<NonNullable<DebateApi['stream']>>().mockImplementation((_id, _content, _key, _round, _total, onUpdate) => { update = onUpdate; return pending.promise })
  const api: DebateApi = { topics: vi.fn(), list: vi.fn(), create: vi.fn().mockResolvedValue(initial.debate), detail: vi.fn().mockResolvedValue(initial), send: vi.fn(), stream, retryTurn: vi.fn(), summary: vi.fn(), requestSummary: vi.fn().mockResolvedValue({ status: 'completed', attempt_version: 1, schema_version: 1, content: { strengths: [{ round_no: 1, point: '优点', reason: '原因' }], issues: [{ round_no: 1, point: '问题', reason: '原因' }], suggestions: [{ round_no: 1, action: '行动', example: '例子' }] } }) }
  const state = createFirstTurnState(api); states.push(state)
  return { api, stream, pending, state, update: (event: StreamUpdate) => update(event), key: () => stream.mock.calls[0]![2] }
}
afterEach(() => { for (const state of states.splice(0)) state.dispose(); vi.useRealTimers() })

describe('B2 流式状态与只读恢复', () => {
  it('片段仅临时预览，双击不重复；完整 done 才进入正式消息和完成轮数', async () => {
    const f = fixture(); await f.state.start('1', 'pro')
    const sending = f.state.submit(original); await f.state.submit('第二次点击')
    expect(f.stream).toHaveBeenCalledOnce(); expect(f.api.send).not.toHaveBeenCalled()
    f.update({ type: 'start', turn_id: '1', request_id: 'request' }); f.update({ type: 'delta', text: '<img src=x>临时' })
    expect(f.state.streamText.value).toBe('<img src=x>临时')
    expect(f.state.detail.value?.turns).toHaveLength(0); expect(f.state.detail.value?.debate.completed_rounds).toBe(0)
    expect(f.state.canSend.value).toBe(false)
    f.pending.resolve(done(f.key())); await sending
    expect(f.state.streamText.value).toBe(''); expect(f.state.streaming.value).toBe(false)
    expect(f.state.detail.value?.turns[0]?.messages[1]?.content).toBe('正式回复')
    expect(f.state.detail.value?.debate.completed_rounds).toBe(1)
  })
  it('流内失败先丢临时片段并 GET，显式 JSON retry 只用查询版本', async () => {
    const f = fixture(); await f.state.start('1', 'pro')
    vi.mocked(f.api.detail).mockImplementation(async () => ({ ...initial, turns: [turn(f.key(), 1, 'failed', 7)] }))
    const sending = f.state.submit(original)
    f.update({ type: 'delta', text: '不能保存为正式消息' })
    f.pending.reject(new ApiError('http', '模型超时', { code: 'model_timeout' })); await sending
    expect(f.state.streamText.value).toBe(''); expect(f.api.detail).toHaveBeenCalledOnce()
    expect(f.state.canRetryTurn.value).toBe(true); expect(f.api.retryTurn).not.toHaveBeenCalled()
    vi.mocked(f.api.retryTurn).mockResolvedValue({ turn: turn(f.key(), 1, 'completed', 8), completed_rounds: 1, query_url: '/api/v1/debates/123' })
    await f.state.retryTurn()
    expect(f.api.retryTurn).toHaveBeenCalledWith('123', '1', 7)
    expect(f.stream).toHaveBeenCalledOnce()
  })
  it('丢失 start/done 时用原 client_request_id 查正式结果，不重发、不换键', async () => {
    const f = fixture(); await f.state.start('1', 'pro')
    vi.mocked(f.api.detail).mockImplementation(async () => ({ ...initial, debate: { ...initial.debate, completed_rounds: 1 }, turns: [turn(f.key())] }))
    const sending = f.state.submit(original)
    f.pending.reject(new ApiError('network', 'EOF')); await sending
    expect(f.api.detail).toHaveBeenCalledWith('123')
    expect(f.state.detail.value?.turns[0]?.client_request_id).toBe(f.key())
    expect(f.state.draft.value).toBe(''); expect(f.stream).toHaveBeenCalledOnce()
  })
  it('暂未找到原键时有界 GET，保持原文锁定，不自动重发', async () => {
    vi.useFakeTimers()
    const f = fixture(); await f.state.start('1', 'pro')
    const sending = f.state.submit(original)
    f.pending.reject(new ApiError('network', 'EOF')); await sending
    expect(f.state.draft.value).toBe(original); expect(f.state.canSend.value).toBe(false)
    await vi.advanceTimersByTimeAsync(122_000)
    const count = vi.mocked(f.api.detail).mock.calls.length
    expect(count).toBeGreaterThan(1)
    await vi.advanceTimersByTimeAsync(20_000)
    expect(f.api.detail).toHaveBeenCalledTimes(count)
    expect(f.stream).toHaveBeenCalledOnce()
    expect(f.state.notice.value).toContain('查询')
  })
  it('202 走查询而非第二次 SSE POST', async () => {
    const f = fixture(); await f.state.start('1', 'pro')
    vi.mocked(f.api.detail).mockImplementation(async () => ({ ...initial, turns: [turn(f.key(), 1, 'generating')] }))
    const sending = f.state.submit(original)
    f.pending.resolve({ kind: 'pending', data: { turn: turn(f.key(), 1, 'generating'), completed_rounds: 0, query_url: '/api/v1/debates/123' } }); await sending
    expect(f.api.detail).toHaveBeenCalledOnce(); expect(f.stream).toHaveBeenCalledOnce()
    expect(f.state.canSend.value).toBe(false)
  })
  it('409 只查询，即使已出现另一请求的同轮也不换键发送下一轮', async () => {
    const f = fixture(); await f.state.start('1', 'pro')
    vi.mocked(f.api.detail).mockResolvedValue({ ...initial, debate: { ...initial.debate, completed_rounds: 1 }, turns: [turn('550e8400-e29b-41d4-a716-446655440001')] })
    const sending = f.state.submit(original)
    f.pending.reject(new ApiError('http', '轮次冲突', { status: 409, code: 'round_conflict' })); await sending
    await f.state.submit('不能换成下一轮')
    expect(f.stream).toHaveBeenCalledOnce(); expect(f.api.retryTurn).not.toHaveBeenCalled()
    expect(f.state.draft.value).toBe(original)
  })
  it('取消当前连接后查询已保存结果，不把取消等同失败或再次生成', async () => {
    const f = fixture(); await f.state.start('1', 'pro')
    f.stream.mockImplementation((_id, _content, _key, _round, _total, _onUpdate, signal) => { signal?.addEventListener('abort', () => f.pending.reject(new ApiError('network', '取消'))); return f.pending.promise })
    vi.mocked(f.api.detail).mockImplementation(async () => ({ ...initial, debate: { ...initial.debate, completed_rounds: 1 }, turns: [turn(f.key())] }))
    const sending = f.state.submit(original); f.state.cancelStream(); await sending
    expect(f.state.detail.value?.debate.completed_rounds).toBe(1); expect(f.stream).toHaveBeenCalledOnce()
  })
  it('卸载会中止连接，旧会话迟到片段和 done 不回填', async () => {
    const f = fixture(); await f.state.start('1', 'pro')
    const sending = f.state.submit(original)
    const signal = f.stream.mock.calls[0]![6]
    f.state.dispose()
    expect(signal?.aborted).toBe(true)
    f.update({ type: 'delta', text: '旧会话内容' }); f.pending.resolve(done(f.key())); await sending
    expect(f.state.streamText.value).toBe(''); expect(f.state.detail.value).toBeNull()
    expect(f.api.detail).not.toHaveBeenCalled()
  })
  it.each([401, 404])('HTTP %s 受控结束恢复，不自动查询或再次提交', async status => {
    const f = fixture(); await f.state.start('1', 'pro')
    const sending = f.state.submit(original)
    f.pending.reject(new ApiError('http', '不可继续读取', { status })); await sending
    expect(f.api.detail).not.toHaveBeenCalled()
    expect(f.stream).toHaveBeenCalledOnce(); expect(f.state.canSend.value).toBe(false)
  })
  it('已完成旧轮重放快照已推进到下一轮时，GET 补全其它正式轮次', async () => {
    const f = fixture(); await f.state.start('1', 'pro')
    vi.mocked(f.api.detail).mockImplementation(async () => ({ ...initial, debate: { ...initial.debate, completed_rounds: 2 }, turns: [turn(f.key()), turn('550e8400-e29b-41d4-a716-446655440001', 2)] }))
    const sending = f.state.submit(original)
    f.pending.resolve({ kind: 'done', data: { turn: turn(f.key()), completed_rounds: 2, status: 'active' } }); await sending
    expect(f.api.detail).toHaveBeenCalledOnce()
    expect(f.state.detail.value?.turns).toHaveLength(2)
    expect(f.state.detail.value?.debate.completed_rounds).toBe(2)
    expect(f.stream).toHaveBeenCalledOnce()
  })
  it('四轮各一次流式提交，只有末轮正式 done 后才自动请求一次总结', async () => {
    const f = fixture(); await f.state.start('1', 'pro')
    f.stream.mockImplementation(async (_id, _content, key, round) => done(key, round))
    for (let round = 1; round <= 4; round++) {
      await f.state.submit(original)
      expect(f.state.detail.value?.debate.completed_rounds).toBe(round)
      if (round < 4) expect(f.api.requestSummary).not.toHaveBeenCalled()
    }
    expect(f.stream).toHaveBeenCalledTimes(4)
    expect(f.api.requestSummary).toHaveBeenCalledOnce()
  })
})
