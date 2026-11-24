// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { createApiClient } from './client'
import { analyzeIssueCards, createIssueCardApi, editIssueCard, isEntityId, listIssueCards } from './issueCard'

/**
 * 争点卡 API 测试：路径、方法、请求体、状态码与响应严格校验。
 * 全部通过注入的 fetch 桩，不连接真实后端。
 */

const fetcher = vi.fn<typeof fetch>()

function envelope(data: unknown, status = 200, requestId = 'req-issue-card') {
  return new Response(JSON.stringify({ data, request_id: requestId }), {
    status,
    headers: { 'Content-Type': 'application/json', 'X-Request-ID': requestId },
  })
}

function failure(status: number, code: string) {
  return new Response(JSON.stringify({ error: { code, message: '受控提示' }, request_id: 'req-issue-card' }), {
    status,
    headers: { 'Content-Type': 'application/json', 'X-Request-ID': 'req-issue-card' },
  })
}

function anchor(overrides: Record<string, unknown> = {}) {
  return { message_id: '101', quote: '标准应当可检验', start_cp: 0, end_cp: 7, resolved: true, ...overrides }
}

function citation(overrides: Record<string, unknown> = {}) {
  return { anchor: anchor(), speaker: 'user', turn_no: 1, note: '提出位置', ...overrides }
}

function card(overrides: Record<string, unknown> = {}) {
  return {
    id: '1001', title: '争点：标准可检验', title_source: 'ai', explanation: '回应是否切中问题需本人核对原句。',
    origin: citation(), responses: [citation({ speaker: 'assistant', note: '相关回应' })],
    pending: ['仍待澄清：前提是否被接受？'], notices: [], version: 1, ...overrides,
  }
}

function payload(overrides: Record<string, unknown> = {}) {
  return {
    state: 'ready', display_cap: 5, analysis: { status: 'completed', attempt_version: 1 },
    items: [card()], ...overrides,
  }
}

function client() {
  return createApiClient({ getCsrfToken: () => 'csrf-issue-card' })
}

beforeEach(() => { fetcher.mockReset(); vi.stubGlobal('fetch', fetcher) })
afterEach(() => { vi.unstubAllGlobals() })

describe('争点卡读取', () => {
  it('GET 只读路径，返回完整卡片形状', async () => {
    fetcher.mockResolvedValueOnce(envelope(payload()))
    const result = await createIssueCardApi(client()).list('7')
    expect(result.items).toHaveLength(1)
    expect(result.state).toBe('ready')
    expect(result.display_cap).toBe(5)
    const [url, init] = fetcher.mock.calls[0]!
    expect(url).toBe('/api/v1/debates/7/issue-cards')
    expect(init?.body).toBeUndefined()
  })

  it('隔离状态返回空列表且不带卡片', async () => {
    fetcher.mockResolvedValueOnce(envelope(payload({ state: 'not_ready', analysis: { status: 'not_started', attempt_version: 0 }, items: [] })))
    const result = await createIssueCardApi(client()).list('7')
    expect(result.state).toBe('not_ready')
    expect(result.items).toEqual([])
  })

  it.each([
    ['未知状态', payload({ state: 'archived' })],
    ['隔离状态却带卡片', payload({ state: 'unsupported' })],
    ['超过展示上限', payload({ items: Array.from({ length: 6 }, (_, i) => card({ id: String(1001 + i) })) })],
    ['未知标题来源', payload({ items: [card({ title_source: 'system' })] })],
    ['已解析却无坐标', payload({ items: [card({ origin: citation({ anchor: anchor({ start_cp: null, end_cp: null }) }) })] })],
    ['待核对却带坐标', payload({ items: [card({ origin: citation({ anchor: anchor({ resolved: false }) }) })] })],
    ['倒序坐标', payload({ items: [card({ origin: citation({ anchor: anchor({ start_cp: 5, end_cp: 2 }) }) })] })],
    ['未知角色', payload({ items: [card({ origin: citation({ speaker: 'system' }) })] })],
    ['非法轮号', payload({ items: [card({ origin: citation({ turn_no: 0 }) })] })],
    ['空标题', payload({ items: [card({ title: '  ' })] })],
    ['版本为 0', payload({ items: [card({ version: 0 })] })],
    ['非数字卡 ID', payload({ items: [card({ id: 'abc' })] })],
    ['相关回应超量', payload({ items: [card({ responses: Array.from({ length: 7 }, () => citation()) })] })],
    ['失败却缺错误码', payload({ analysis: { status: 'failed', attempt_version: 2 } })],
    ['非失败却带错误码', payload({ analysis: { status: 'completed', attempt_version: 2, error_code: 'model_error' } })],
    ['未知错误码', payload({ analysis: { status: 'failed', attempt_version: 2, error_code: 'boom' } })],
  ])('拒绝不符合契约的响应：%s', async (_name, body) => {
    fetcher.mockResolvedValueOnce(envelope(body))
    await expect(createIssueCardApi(client()).list('7')).rejects.toMatchObject({ kind: 'format' })
  })

  it('拒绝非法会话编号，不发请求', async () => {
    await expect(createIssueCardApi(client()).list('0')).rejects.toMatchObject({ kind: 'format' })
    expect(fetcher).not.toHaveBeenCalled()
  })

  it('越权按 404 映射为受控提示', async () => {
    fetcher.mockResolvedValueOnce(failure(404, 'not_found'))
    await expect(createIssueCardApi(client()).list('7')).rejects.toMatchObject({ status: 404, code: 'not_found' })
  })
})

describe('争点卡整理', () => {
  it('POST 显式整理，带 CSRF，接受 200/201/202', async () => {
    fetcher.mockResolvedValueOnce(envelope(payload(), 201))
    const result = await createIssueCardApi(client()).analyze('7')
    expect(result.analysis.status).toBe('completed')
    const [url, init] = fetcher.mock.calls[0]!
    expect(url).toBe('/api/v1/debates/7/issue-cards/analyze')
    expect(init?.method).toBe('POST')
    expect(new Headers(init?.headers).get('X-CSRF-Token')).toBe('csrf-issue-card')
    expect(JSON.parse(String(init?.body))).toEqual({})
  })

  it('重试携带版本；202 表示槽位忙仍返回当前状态', async () => {
    fetcher.mockResolvedValueOnce(envelope(payload({ analysis: { status: 'generating', attempt_version: 2 }, items: [] }), 202))
    const result = await createIssueCardApi(client()).analyze('7', { retry: true, expected_attempt_version: 1 })
    expect(result.analysis.status).toBe('generating')
    const [, init] = fetcher.mock.calls[0]!
    expect(JSON.parse(String(init?.body))).toEqual({ retry: true, expected_attempt_version: 1 })
  })

  it('拒绝非法版本，不发请求', async () => {
    await expect(createIssueCardApi(client()).analyze('7', { expected_attempt_version: -1 })).rejects.toMatchObject({ kind: 'format' })
    expect(fetcher).not.toHaveBeenCalled()
  })

  it('分析失败按受控错误码映射', async () => {
    fetcher.mockResolvedValueOnce(failure(502, 'model_error'))
    await expect(createIssueCardApi(client()).analyze('7')).rejects.toMatchObject({ status: 502, code: 'model_error' })
  })

  it('缺少 CSRF 时不发写请求', async () => {
    const bare = createApiClient()
    await expect(createIssueCardApi(bare).analyze('7')).rejects.toMatchObject({ kind: 'csrf' })
    expect(fetcher).not.toHaveBeenCalled()
  })
})

describe('争点卡人工编辑', () => {
  it('改名提交版本、动作与标题', async () => {
    fetcher.mockResolvedValueOnce(envelope(card({ title: '人工标题', title_source: 'user', version: 2 })))
    const updated = await createIssueCardApi(client()).edit('7', '1001', { expected_version: 1, action: 'rename', title: '人工标题' })
    expect(updated.title).toBe('人工标题')
    expect(updated.title_source).toBe('user')
    const [url, init] = fetcher.mock.calls[0]!
    expect(url).toBe('/api/v1/debates/7/issue-cards/1001/edit')
    expect(init?.method).toBe('POST')
    expect(JSON.parse(String(init?.body))).toEqual({ expected_version: 1, action: 'rename', title: '人工标题' })
  })

  it('合并与移除提交各自请求体', async () => {
    fetcher.mockResolvedValueOnce(envelope(card({ version: 2 })))
    await createIssueCardApi(client()).edit('7', '1001', { expected_version: 1, action: 'merge', target_card_id: '1002' })
    expect(JSON.parse(String(fetcher.mock.calls[0]![1]?.body))).toEqual({ expected_version: 1, action: 'merge', target_card_id: '1002' })

    fetcher.mockResolvedValueOnce(envelope(card({ version: 2 })))
    await createIssueCardApi(client()).edit('7', '1001', { expected_version: 1, action: 'remove' })
    expect(JSON.parse(String(fetcher.mock.calls[1]![1]?.body))).toEqual({ expected_version: 1, action: 'remove' })
  })

  it('版本冲突 409 保留业务码，供组件只做弱提示', async () => {
    fetcher.mockResolvedValueOnce(failure(409, 'issue_card_version_conflict'))
    await expect(createIssueCardApi(client()).edit('7', '1001', { expected_version: 1, action: 'remove' }))
      .rejects.toMatchObject({ status: 409, code: 'issue_card_version_conflict' })
  })

  it('越权 404 与非法 ID 都被拒绝', async () => {
    fetcher.mockResolvedValueOnce(failure(404, 'not_found'))
    await expect(createIssueCardApi(client()).edit('7', '1001', { expected_version: 1, action: 'remove' }))
      .rejects.toMatchObject({ status: 404 })
    await expect(createIssueCardApi(client()).edit('7', '0', { expected_version: 1, action: 'remove' }))
      .rejects.toMatchObject({ kind: 'format' })
  })

  it('响应卡片 ID 与服务端不一致时按格式错误拒绝', async () => {
    fetcher.mockResolvedValueOnce(envelope(card({ id: '1002' })))
    await expect(createIssueCardApi(client()).edit('7', '1001', { expected_version: 1, action: 'remove' }))
      .rejects.toMatchObject({ kind: 'format' })
  })
})

describe('争点卡便捷入口', () => {
  it('导出函数复用同一契约', async () => {
    fetcher.mockResolvedValueOnce(envelope(payload()))
    const listed = await listIssueCards('7', client())
    expect(listed.items[0]!.id).toBe('1001')

    fetcher.mockResolvedValueOnce(envelope(payload(), 201))
    const analyzed = await analyzeIssueCards('7', {}, client())
    expect(analyzed.state).toBe('ready')

    fetcher.mockResolvedValueOnce(envelope(card({ version: 2 })))
    const edited = await editIssueCard('7', '1001', { expected_version: 1, action: 'rename', title: '新' }, client())
    expect(edited.version).toBe(2)
  })

  it('实体 ID 校验与共享规则一致', () => {
    expect(isEntityId('1')).toBe(true)
    expect(isEntityId('0')).toBe(false)
    expect(isEntityId('01')).toBe(false)
    expect(isEntityId('18446744073709551616')).toBe(false)
  })
})
