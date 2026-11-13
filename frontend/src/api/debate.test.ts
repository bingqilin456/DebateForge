import { describe, expect, it, vi } from 'vitest'
import {
  createDebate,
  createDebateApi,
  getDebateDetail,
  listDebates,
  listTopics,
  type SummaryContent,
} from './debate'
import { createApiClient } from './client'

const time = '2026-11-09T00:00:00Z'
const key = '550e8400-e29b-41d4-a716-446655440000'
const topic = { id: '1', title: '题目', description: '说明' }
const sampleSummary: SummaryContent = {
  strengths: [{ round_no: 1, point: '立论清晰', reason: '首轮紧扣考核公平' }],
  issues: [{ round_no: 2, point: '反驳链条偏短', reason: '未展开评价标准对比' }],
  suggestions: [{ round_no: 3, action: '先拆前提再给结论', example: '即便存在差异，也可通过过程留痕解决' }],
}
function result(roundNo = 1) {
  return {
    turn: {
      id: '2', round_no: roundNo, client_request_id: key, status: 'generating',
      attempt_version: 1, error_code: null,
      messages: [{ id: '3', role: 'user', content: '原文', created_at: time }],
    },
    completed_rounds: roundNo - 1,
    query_url: '/api/v1/debates/123',
  }
}
function reply(data: unknown, status = 200) {
  return new Response(JSON.stringify({ data, request_id: 'req' }), {
    status,
    headers: { 'Content-Type': 'application/json', 'X-Request-ID': 'req' },
  })
}

describe('辩论与总结 API 边界', () => {
  it('拒绝无效实体与不可信详情，禁止静默展示损坏响应', async () => {
    const fetcher = vi.spyOn(globalThis, 'fetch').mockResolvedValue(new Response(JSON.stringify({ data: { items: [{ id: '0', title: '题', description: '' }] }, request_id: 'req' }), { headers: { 'Content-Type': 'application/json', 'X-Request-ID': 'req' } }))
    try { await expect(createDebateApi(createApiClient()).topics()).rejects.toMatchObject({ kind: 'format' }) } finally { fetcher.mockRestore() }
  })

  it('生成使用 70 秒预算、按会话赛制校验轮次上限与 202，且保留输入原文和相同请求标识', async () => {
    const request = vi.fn().mockResolvedValue({})
    const api = createDebateApi({ request })
    await api.send('123', ' 原文 ', key)
    expect(request).toHaveBeenCalledWith('/api/v1/debates/123/turns', expect.objectContaining({
      timeoutMs: 70_000, expectedStatus: [200, 201, 202],
      body: { round_no: 1, content: ' 原文 ', client_request_id: key },
    }))
    await api.send('123', '第二轮', key, 2)
    expect(request).toHaveBeenCalledWith('/api/v1/debates/123/turns', expect.objectContaining({
      body: { round_no: 2, content: '第二轮', client_request_id: key },
    }))
    // 四轮标准赛制下第 4 轮合法。
    await api.send('123', '第四轮', key, 4, 4)
    expect(request).toHaveBeenCalledWith('/api/v1/debates/123/turns', expect.objectContaining({
      body: { round_no: 4, content: '第四轮', client_request_id: key },
    }))
    // 超过该会话赛制上限（四轮的第 5 轮）与三轮会话的第 4 轮都必须被拒。
    await expect(api.send('123', '第五轮', key, 5, 4)).rejects.toMatchObject({ kind: 'format' })
    await expect(api.send('123', '越界', key, 4, 3)).rejects.toMatchObject({ kind: 'format' })
    await expect(api.detail('../auth/me')).rejects.toMatchObject({ kind: 'format' })
  })

  it('真实 client 接受 202 生成中 DTO，不误判完成', async () => {
    const fetcher = vi.spyOn(globalThis, 'fetch').mockResolvedValue(reply(result(2), 202))
    try {
      const api = createDebateApi(createApiClient({ getCsrfToken: () => 'test-csrf' }))
      await expect(api.send('123', '原文', key, 2)).resolves.toMatchObject({ completed_rounds: 1, turn: { round_no: 2, status: 'generating' } })
    } finally { fetcher.mockRestore() }
  })

  it.each(['uuid', 'content', 'messages'])('真实 client 拒绝不属于本次请求或损坏的 %s 响应', async fault => {
    const data = result()
    if (fault === 'uuid') data.turn.client_request_id = '550e8400-e29b-41d4-a716-446655440001'
    if (fault === 'content') data.turn.messages[0]!.content = '另一个请求的原文'
    if (fault === 'messages') data.turn.messages[0]!.role = 'assistant'
    const fetcher = vi.spyOn(globalThis, 'fetch').mockResolvedValue(reply(data, 202))
    try { await expect(createDebateApi(createApiClient({ getCsrfToken: () => 'test-csrf' })).send('123', '原文', key)).rejects.toMatchObject({ kind: 'format' }) }
    finally { fetcher.mockRestore() }
  })

  it('真实 client 拒绝其它会话详情，即使结构完整', async () => {
    const data = { debate: { id: '124', topic, user_stance: 'pro', ai_stance: 'con', mode: 'quick_3', ai_style: 'logical', total_rounds: 3, status: 'active', completed_rounds: 0, created_at: time, updated_at: time }, turns: [], summary_status: { status: 'not_started', attempt_version: null } }
    const fetcher = vi.spyOn(globalThis, 'fetch').mockResolvedValue(reply(data))
    try { await expect(createDebateApi(createApiClient()).detail('123')).rejects.toMatchObject({ kind: 'format' }) }
    finally { fetcher.mockRestore() }
  })

  it('显式重试轮次携带 expected_attempt_version 并映射 409 attempt_version_conflict', async () => {
    const fetcher = vi.spyOn(globalThis, 'fetch')
      .mockResolvedValueOnce(reply({
        turn: {
          id: '2', round_no: 2, client_request_id: key, status: 'completed',
          attempt_version: 2, error_code: null,
          messages: [
            { id: '3', role: 'user', content: '原文', created_at: time },
            { id: '4', role: 'assistant', content: '重试成功回复', created_at: time },
          ],
        },
        completed_rounds: 2,
        query_url: '/api/v1/debates/123',
      }, 200))
      .mockResolvedValueOnce(new Response(JSON.stringify({
        error: {
          code: 'attempt_version_conflict',
          message: '请求未完成，请查看会话状态后再操作',
          details: { turn_id: '2', status: 'failed', attempt_version: 2, query_url: '/api/v1/debates/123' },
        },
        request_id: 'req-conflict',
      }), { status: 409, headers: { 'Content-Type': 'application/json', 'X-Request-ID': 'req-conflict' } }))
    try {
      const api = createDebateApi(createApiClient({ getCsrfToken: () => 'test-csrf' }))
      await expect(api.retryTurn('123', '2', 0)).rejects.toMatchObject({ kind: 'format' })
      const ok = await api.retryTurn('123', '2', 1)
      expect(ok.turn.attempt_version).toBe(2)
      expect(fetcher.mock.calls[0]?.[1]?.body).toBe('{"expected_attempt_version":1}')
      await expect(api.retryTurn('123', '2', 1)).rejects.toMatchObject({
        kind: 'http', status: 409, code: 'attempt_version_conflict',
      })
    } finally { fetcher.mockRestore() }
  })

  it('结构化总结请求与 Schema 校验：接受合法三栏，拒绝越界轮次或空数组', async () => {
    const fetcher = vi.spyOn(globalThis, 'fetch')
      .mockResolvedValueOnce(reply({
        status: 'completed', attempt_version: 1, schema_version: 1, content: sampleSummary,
      }, 201))
      .mockResolvedValueOnce(reply({
        status: 'completed', attempt_version: 2, schema_version: 1,
        // 第 5 轮超出任何已知赛制上限（quick_3 为 3、standard_4 为 4），必须被拒。
        content: { ...sampleSummary, strengths: [{ round_no: 5, point: '非法轮次', reason: '越界' }] },
      }, 200))
    try {
      const api = createDebateApi(createApiClient({ getCsrfToken: () => 'test-csrf' }))
      await expect(api.requestSummary('123', { retry: true })).rejects.toMatchObject({ kind: 'format' })
      const created = await api.requestSummary('123')
      expect(created.status).toBe('completed')
      expect(fetcher.mock.calls[0]?.[1]?.body).toBe('{"retry":false}')
      await expect(api.requestSummary('123', { retry: true, expectedAttemptVersion: 1 })).rejects.toMatchObject({ kind: 'format' })
      expect(fetcher.mock.calls[1]?.[1]?.body).toBe('{"retry":true,"expected_attempt_version":1}')
    } finally { fetcher.mockRestore() }
  })


  it('总结校验按会话赛制收窄轮号上限，不接受 quick_3 会话的第 4 轮引用', async () => {
    // 这是回归测试：summary/requestSummary 曾用全局上限 4 校验，
    // 导致 quick_3 会话的总结可以引用不存在的第 4 轮。
    const badSummary = { ...sampleSummary, strengths: [{ round_no: 4, point: '不存在的轮次', reason: '越界' }] }
    const fetcher = vi.spyOn(globalThis, 'fetch')
      .mockResolvedValueOnce(reply({ status: 'completed', attempt_version: 1, schema_version: 1, content: badSummary }, 200))
      .mockResolvedValueOnce(reply({ status: 'completed', attempt_version: 1, schema_version: 1, content: sampleSummary }, 200))
    try {
      const api = createDebateApi(createApiClient({ getCsrfToken: () => 'test-csrf' }))
      // 三轮会话：第 4 轮必须被判为格式错误。
      await expect(api.requestSummary('123', { totalRounds: 3 })).rejects.toMatchObject({ kind: 'format' })
      // 四轮会话：第 4 轮合法。
      const ok = await api.requestSummary('123', { totalRounds: 4 })
      expect(ok.status).toBe('completed')
    } finally { fetcher.mockRestore() }
  })
  it('本人历史列表校验分页参数与返回结构', async () => {
    const debateItem = { id: '123', topic, user_stance: 'pro', ai_stance: 'con', mode: 'quick_3', ai_style: 'logical', total_rounds: 3, status: 'completed', completed_rounds: 3, created_at: time, updated_at: time }
    const fetcher = vi.spyOn(globalThis, 'fetch').mockResolvedValue(reply({
      items: [debateItem], total: 1, page: 1, page_size: 10,
    }))
    try {
      const api = createDebateApi(createApiClient())
      await expect(api.list(0, 10)).rejects.toMatchObject({ kind: 'format' })
      await expect(api.list(1, 51)).rejects.toMatchObject({ kind: 'format' })
      const list = await api.list(1, 10)
      expect(list.items).toHaveLength(1)
      expect(list.total).toBe(1)
    } finally { fetcher.mockRestore() }
  })

  it('提供供成员 B 直接调用的 listTopics、createDebate、listDebates(page, pageSize)、getDebateDetail 函数与契约校验', async () => {
    const debateItem = {
      id: '123',
      topic,
      user_stance: 'pro' as const,
      ai_stance: 'con' as const,
      mode: 'quick_3' as const,
      ai_style: 'logical' as const,
      total_rounds: 3,
      status: 'active' as const,
      completed_rounds: 0,
      created_at: time,
      updated_at: time,
    }
    const detailData = {
      debate: debateItem,
      turns: [],
      summary_status: { status: 'not_started' as const, attempt_version: null },
    }
    const fetcher = vi.spyOn(globalThis, 'fetch')
      .mockResolvedValueOnce(reply({ items: [topic] }, 200))
      .mockResolvedValueOnce(reply(debateItem, 201))
      .mockResolvedValueOnce(reply({ ...debateItem, user_stance: 'con', ai_stance: 'pro' }, 201))
      .mockResolvedValueOnce(reply({ items: [debateItem], total: 1, page: 1, page_size: 20 }, 200))
      .mockResolvedValueOnce(reply({ items: [debateItem], total: 1, page: 2, page_size: 5 }, 200))
      .mockResolvedValueOnce(reply(detailData, 200))
    try {
      const client = createApiClient({ getCsrfToken: () => 'test-csrf' })

      const topics = await listTopics(client)
      expect(topics).toEqual([topic])
      expect(fetcher.mock.calls[0]?.[0]).toBe('/api/v1/topics')

      const createdByArgs = await createDebate('1', 'pro', client)
      expect(createdByArgs.id).toBe('123')
      expect(fetcher.mock.calls[1]?.[0]).toBe('/api/v1/debates')
      expect(fetcher.mock.calls[1]?.[1]?.body).toBe('{"topic_id":"1","user_stance":"pro"}')

      const createdByObject = await createDebate({ topic_id: '1', user_stance: 'con' }, client)
      expect(createdByObject.user_stance).toBe('con')
      expect(fetcher.mock.calls[2]?.[1]?.body).toBe('{"topic_id":"1","user_stance":"con"}')

      await expect(createDebate('0', 'pro', client)).rejects.toMatchObject({ kind: 'format' })
      await expect(
        createDebate({ topic_id: '1', user_stance: 'invalid' as 'pro' }, client),
      ).rejects.toMatchObject({ kind: 'format' })

      const defaultPage = await listDebates(undefined, undefined, client)
      expect(defaultPage.page).toBe(1)
      expect(defaultPage.page_size).toBe(20)
      expect(fetcher.mock.calls[3]?.[0]).toBe('/api/v1/debates?page=1&page_size=20')

      const customPage = await listDebates(2, 5, client)
      expect(customPage.page).toBe(2)
      expect(customPage.page_size).toBe(5)
      expect(fetcher.mock.calls[4]?.[0]).toBe('/api/v1/debates?page=2&page_size=5')

      await expect(listDebates(0, 20, client)).rejects.toMatchObject({ kind: 'format' })
      await expect(listDebates(1, 51, client)).rejects.toMatchObject({ kind: 'format' })

      const detail = await getDebateDetail('123', client)
      expect(detail.debate.id).toBe('123')
      expect(fetcher.mock.calls[5]?.[0]).toBe('/api/v1/debates/123')
      await expect(getDebateDetail('0', client)).rejects.toMatchObject({ kind: 'format' })
    } finally {
      fetcher.mockRestore()
    }
  })
})
