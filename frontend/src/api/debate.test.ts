import { describe, expect, it, vi } from 'vitest'
import { createDebateApi } from './debate'
import { createApiClient } from './client'

const time = '2026-11-09T00:00:00Z'
const key = '550e8400-e29b-41d4-a716-446655440000'
const topic = { id: '1', title: '题目', description: '说明' }
function result() {
  return { turn: { id: '2', round_no: 1, client_request_id: key, status: 'generating', attempt_version: 1, error_code: null, messages: [{ id: '3', role: 'user', content: '原文', created_at: time }] }, completed_rounds: 0, query_url: '/api/v1/debates/123' }
}
function reply(data: unknown, status = 200) {
  return new Response(JSON.stringify({ data, request_id: 'req' }), { status, headers: { 'Content-Type': 'application/json', 'X-Request-ID': 'req' } })
}

describe('首轮 API 边界', () => {
  it('拒绝无效实体与不可信详情，禁止静默展示损坏响应', async () => {
    const fetcher = vi.spyOn(globalThis, 'fetch').mockResolvedValue(new Response(JSON.stringify({ data: { items: [{ id: '0', title: '题', description: '' }] }, request_id: 'req' }), { headers: { 'Content-Type': 'application/json', 'X-Request-ID': 'req' } }))
    try { await expect(createDebateApi(createApiClient()).topics()).rejects.toMatchObject({ kind: 'format' }) } finally { fetcher.mockRestore() }
  })
  it('生成使用 70 秒预算、允许 202，且保留输入原文和相同请求标识', async () => {
    const request = vi.fn().mockResolvedValue({})
    const api = createDebateApi({ request })
    await api.send('123', ' 原文 ', '550e8400-e29b-41d4-a716-446655440000')
    expect(request).toHaveBeenCalledWith('/api/v1/debates/123/turns', expect.objectContaining({ timeoutMs: 70_000, expectedStatus: [200, 201, 202], body: { round_no: 1, content: ' 原文 ', client_request_id: '550e8400-e29b-41d4-a716-446655440000' } }))
    await expect(api.detail('../auth/me')).rejects.toMatchObject({ kind: 'format' })
  })
  it('真实 client 接受 202 生成中 DTO，不误判完成', async () => {
    const fetcher = vi.spyOn(globalThis, 'fetch').mockResolvedValue(reply(result(), 202))
    try {
      const api = createDebateApi(createApiClient({ getCsrfToken: () => 'test-csrf' }))
      await expect(api.send('123', '原文', key)).resolves.toMatchObject({ completed_rounds: 0, turn: { status: 'generating' } })
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
    const data = { debate: { id: '124', topic, user_stance: 'pro', ai_stance: 'con', status: 'active', completed_rounds: 0, created_at: time, updated_at: time }, turns: [], summary_status: { status: 'not_started', attempt_version: null } }
    const fetcher = vi.spyOn(globalThis, 'fetch').mockResolvedValue(reply(data))
    try { await expect(createDebateApi(createApiClient()).detail('123')).rejects.toMatchObject({ kind: 'format' }) }
    finally { fetcher.mockRestore() }
  })
})
