import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { createDebateApi } from './debate'
import { createApiClient, GENERATION_TIMEOUT_MS } from './client'

const fetcher = vi.fn<typeof fetch>()
const topic = { id: '1', title: '开放考核', description: '评价公平' }
const debate = { id: '123', topic, user_stance: 'pro', ai_stance: 'con', mode: 'standard_4', ai_style: 'logical', total_rounds: 4, status: 'active', completed_rounds: 0, created_at: '2026-11-15T00:00:00Z', updated_at: '2026-11-15T00:00:00Z' }
const content = { thesis: '支持用户立场。', arguments: ['论据一。'], risks: ['回应对手质疑。'] }
const completed = { status: 'completed', attempt_version: 1, schema_version: 1, prompt_version: 'strategy-v1', ai_style: 'logical', content }
function reply(data: unknown, status = 200) {
  return new Response(JSON.stringify({ data, request_id: 'req-t14' }), {
    status, headers: { 'Content-Type': 'application/json', 'X-Request-ID': 'req-t14' },
  })
}
function api() { return createDebateApi(createApiClient({ getCsrfToken: () => 'test-csrf' })) }
beforeEach(() => { fetcher.mockReset(); vi.stubGlobal('fetch', fetcher) })
afterEach(() => { vi.unstubAllGlobals(); vi.restoreAllMocks() })

describe('T14 流派创建边界', () => {
  it.each(['logical', 'aggressive', 'empathic', 'data'] as const)('原样提交合法流派 %s，不发送 mode', async ai_style => {
    fetcher.mockResolvedValue(reply({ ...debate, ai_style }, 201))
    await expect(api().create({ topic_id: '1', user_stance: 'pro', ai_style })).resolves.toMatchObject({ ai_style })
    expect(JSON.parse(String(fetcher.mock.calls[0]![1]?.body))).toEqual({ topic_id: '1', user_stance: 'pro', ai_style })
  })
  it('保留旧调用方式，省略时由后端默认 logical', async () => {
    fetcher.mockResolvedValue(reply(debate, 201))
    await expect(api().create('1', 'pro')).resolves.toMatchObject({ ai_style: 'logical' })
    expect(JSON.parse(String(fetcher.mock.calls[0]![1]?.body))).toEqual({ topic_id: '1', user_stance: 'pro' })
  })
  it.each([null, '', 'Logical', 'unknown', 7, [], {}])('非法流派 %# 在本地拒绝，不发创建请求', async ai_style => {
    const request = { topic_id: '1', user_stance: 'pro', ai_style }
    await expect(api().create(request as Parameters<ReturnType<typeof api>['create']>[0])).rejects.toMatchObject({ kind: 'format' })
    expect(fetcher).not.toHaveBeenCalled()
  })
})

describe('T14 策略 GET / POST 契约', () => {
  it.each([
    { status: 'not_started', attempt_version: null },
    { status: 'generating', attempt_version: 1 },
    { status: 'failed', attempt_version: 2, error_code: 'model_timeout' },
    completed,
  ])('GET 只查询已有状态 $status', async state => {
    fetcher.mockResolvedValue(reply(state))
    await expect(api().getStrategy('123')).resolves.toEqual(state)
    const [url, init] = fetcher.mock.calls[0]!
    expect(url).toBe('/api/v1/debates/123/strategy')
    expect(init).toMatchObject({ method: 'GET', body: undefined })
    expect(new Headers(init?.headers).has('X-CSRF-Token')).toBe(false)
    expect(fetcher).toHaveBeenCalledOnce()
  })
  it('按契约读取保存的任意非空 Prompt 版本，不假定当前版本或自增长度上限', async () => {
    const state = { ...completed, prompt_version: 'saved-version-'.repeat(8) }
    fetcher.mockResolvedValue(reply(state))
    await expect(api().getStrategy('123')).resolves.toEqual(state)
  })
  it.each([201, 200, 202] as const)('首次 POST {} 接受 %s，复用 Session/CSRF 和 70 秒预算', async status => {
    const state = status === 202 ? { status: 'generating', attempt_version: 1 } : completed
    fetcher.mockResolvedValue(reply(state, status))
    const timer = vi.spyOn(globalThis, 'setTimeout')
    await expect(api().generateStrategy('123')).resolves.toEqual(state)
    const [url, init] = fetcher.mock.calls[0]!
    expect(url).toBe('/api/v1/debates/123/strategy')
    expect(init).toMatchObject({ method: 'POST', body: '{}', credentials: 'same-origin' })
    expect(new Headers(init?.headers).get('X-CSRF-Token')).toBe('test-csrf')
    expect(timer).toHaveBeenCalledWith(expect.any(Function), GENERATION_TIMEOUT_MS)
  })
  it('显式重试原样提交最新查询版本，不自行增加版本', async () => {
    fetcher.mockResolvedValue(reply({ ...completed, attempt_version: 3 }))
    await api().generateStrategy('123', { retry: true, expected_attempt_version: 2 })
    expect(JSON.parse(String(fetcher.mock.calls[0]![1]?.body))).toEqual({ retry: true, expected_attempt_version: 2 })
  })
  it.each([
    { retry: true }, { retry: true, expected_attempt_version: 0 },
    { retry: true, expected_attempt_version: 1.5 }, { retry: false, expected_attempt_version: 1 },
    { retry: null }, { retry: 'true' }, { retry: false, content: '不允许字段' },
  ])('拒绝不合约的重试参数 %#', async body => {
    await expect(api().generateStrategy('123', body as Parameters<ReturnType<typeof api>['generateStrategy']>[1])).rejects.toMatchObject({ kind: 'format' })
    expect(fetcher).not.toHaveBeenCalled()
  })
  it.each([
    { status: 'not_started', attempt_version: 1 },
    { status: 'generating', attempt_version: 0 },
    { status: 'generating', attempt_version: 1, content },
    { status: 'failed', attempt_version: 1, error_code: 'private_error' },
    { ...completed, schema_version: 2 }, { ...completed, ai_style: 'Unknown' },
    { ...completed, prompt_version: '' },
    { ...completed, content: { ...content, thesis: '\ud800' } },
    { ...completed, content: { ...content, extra: '不接受原始HTML字段' } },
    { ...completed, content: { ...content, arguments: [] } },
    { ...completed, content: { ...content, thesis: ' '.repeat(20) } },
    { ...completed, content: { ...content, thesis: '字'.repeat(301) } },
    { ...completed, content: { thesis: '😀'.repeat(300), arguments: Array(3).fill('😀'.repeat(300)), risks: Array(3).fill('😀'.repeat(300)) } },
  ])('严格拒绝损坏的策略状态/内容 %#', async state => {
    fetcher.mockResolvedValue(reply(state))
    await expect(api().getStrategy('123')).rejects.toMatchObject({ kind: 'format' })
  })
  it('POST 不接受 not_started 伪成功', async () => {
    fetcher.mockResolvedValue(reply({ status: 'not_started', attempt_version: null }, 200))
    await expect(api().generateStrategy('123')).rejects.toMatchObject({ kind: 'format' })
  })
  it('409 保留错误码，API 层不自动重发', async () => {
    fetcher.mockResolvedValue(new Response(JSON.stringify({ error: { code: 'attempt_version_conflict', message: '受控失败', details: { attempt_version: 3 } }, request_id: 'req-t14' }), { status: 409, headers: { 'Content-Type': 'application/json', 'X-Request-ID': 'req-t14' } }))
    await expect(api().generateStrategy('123', { retry: true, expected_attempt_version: 2 })).rejects.toMatchObject({ status: 409, code: 'attempt_version_conflict' })
    expect(fetcher).toHaveBeenCalledOnce()
  })
})
