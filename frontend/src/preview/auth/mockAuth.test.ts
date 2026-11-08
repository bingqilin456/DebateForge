import { describe, expect, it } from 'vitest'
import { createPreviewAuthMock } from './mockAuth'

const credentials = JSON.stringify({ username: 'preview_user', password: 'preview-pass-123' })
function mock() {
  const result = createPreviewAuthMock('http://127.0.0.1:5173')
  result.controls.delayMs = 0
  return result
}

describe('开发预览专用模拟认证', () => {
  it('注册返回 201 但不创建会话，me 仍为 401', async () => {
    const preview = mock()
    const registered = await preview.fetch('/api/v1/auth/register', { method: 'POST', body: credentials })
    expect(registered.status).toBe(201)
    const body: unknown = await registered.json()
    expect(body).toMatchObject({ data: { id: '42', username: 'preview_user' } })
    const me = await preview.fetch('/api/v1/auth/me')
    expect(me.status).toBe(401)
    expect(preview.state.counts).toMatchObject({ register: 1, login: 0, me: 1 })
  })

  it('成功登录后能查询模拟会话，正确 CSRF 退出为无正文 204', async () => {
    const preview = mock()
    await preview.fetch('/api/v1/auth/login', { method: 'POST', body: credentials })
    expect((await preview.fetch('/api/v1/auth/me')).status).toBe(200)
    expect((await preview.fetch('/api/v1/auth/logout', { method: 'POST' })).status).toBe(403)
    const exited = await preview.fetch('/api/v1/auth/logout', {
      method: 'POST', headers: { 'X-CSRF-Token': 'preview-csrf-only' },
    })
    expect(exited.status).toBe(204)
    expect(await exited.text()).toBe('')
    expect((await preview.fetch('/api/v1/auth/me')).status).toBe(401)
  })

  it.each([
    ['rejected', 'login', 401, 'invalid_credentials'],
    ['rejected', 'register', 409, 'username_conflict'],
    ['rate_limited', 'login', 429, 'rate_limited'],
    ['unavailable', 'register', 503, 'dependency_unavailable'],
  ] as const)('可控失败场景 %s / %s', async (scenario, operation, status, code) => {
    const preview = mock()
    preview.controls.scenario = scenario
    const response = await preview.fetch(`/api/v1/auth/${operation}`, { method: 'POST', body: credentials })
    expect(response.status).toBe(status)
    expect(await response.json()).toMatchObject({ error: { code } })
    if (status === 429) expect(response.headers.get('Retry-After')).toBe('15')
    expect(preview.state.pending).toBe(0)
    expect(JSON.stringify(preview.state)).not.toContain('preview-pass-123')
  })

  it('网络失败不会退回真实 fetch', async () => {
    const preview = mock()
    preview.controls.scenario = 'network'
    await expect(preview.fetch('/api/v1/auth/login', { method: 'POST', body: credentials })).rejects.toBeInstanceOf(TypeError)
    expect(preview.state.lastStatus).toBe('network')
    expect(preview.state.pending).toBe(0)
  })

  it.each(['https://other.example/api/v1/auth/login', '/api/v1/debates', '/health/live'])
  ('拒绝模拟范围外的请求 %s，不发送到后端', async (url) => {
    await expect(mock().fetch(url)).rejects.toBeInstanceOf(TypeError)
  })

  it('取消等待会释放 pending，且不建立迟到的模拟会话', async () => {
    const preview = mock()
    preview.controls.delayMs = 1_500
    const controller = new AbortController()
    const result = preview.fetch('/api/v1/auth/login', { method: 'POST', body: credentials, signal: controller.signal })
    expect(preview.state.pending).toBe(1)
    const rejected = expect(result).rejects.toMatchObject({ name: 'AbortError' })
    controller.abort()
    await rejected
    expect(preview.state.pending).toBe(0)
    preview.controls.delayMs = 0
    expect((await preview.fetch('/api/v1/auth/me')).status).toBe(401)
  })
})
