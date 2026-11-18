import { describe, expect, it, vi } from 'vitest'
import { createApiClient } from './client'
import { createDebate, createDebateApi } from './debate'
import { CUSTOM_TOPIC_BACKGROUND_MAX_CHARS, CUSTOM_TOPIC_TITLE_MAX_CHARS } from '../shared/limits'

// V2-03 私有自定义辩题的客户端契约回归：严格二选一请求形状、
// 码点边界与前后端一致、空 topic.id 的快照可读、预设调用方保持不变。

const time = '2026-11-16T00:00:00Z'

function reply(data: unknown, status = 200) {
  return new Response(JSON.stringify({ data, request_id: 'req-custom' }), {
    status, headers: { 'Content-Type': 'application/json', 'X-Request-ID': 'req-custom' },
  })
}

function customDebate(title: string, description: string, id: string | null = null) {
  return {
    id: '321',
    topic: { id, title, description },
    user_stance: 'pro',
    ai_stance: 'con',
    mode: 'standard_4',
    ai_style: 'logical',
    total_rounds: 4,
    status: 'active',
    completed_rounds: 0,
    created_at: time,
    updated_at: time,
  }
}

describe('V2-03 自定义辩题客户端契约', () => {
  it('自定义创建只发送 custom_topic，保留原文且不带 topic_id', async () => {
    const title = '  是否应把远程办公设为默认？\n'
    const background = '背景：讨论协作成本。\t'
    const fetcher = vi.spyOn(globalThis, 'fetch').mockResolvedValueOnce(reply(customDebate(title, background), 201))
    try {
      const client = createApiClient({ getCsrfToken: () => 'test-csrf' })
      const created = await createDebate({ custom_topic: { title, background }, user_stance: 'pro', ai_style: 'empathic' }, client)
      expect(created.topic.id).toBeNull()
      expect(created.topic.title).toBe(title)
      const body = JSON.parse(String(fetcher.mock.calls[0]?.[1]?.body))
      expect(body).toEqual({ custom_topic: { title, background }, user_stance: 'pro', ai_style: 'empathic' })
      expect('topic_id' in body).toBe(false)
    } finally {
      fetcher.mockRestore()
    }
  })

  it('背景省略时不合成空字符串字段，也不替调用方补默认值', async () => {
    const fetcher = vi.spyOn(globalThis, 'fetch').mockResolvedValueOnce(reply(customDebate('仅标题', ''), 201))
    try {
      const client = createApiClient({ getCsrfToken: () => 'test-csrf' })
      await createDebate({ custom_topic: { title: '仅标题' }, user_stance: 'con' }, client)
      const body = JSON.parse(String(fetcher.mock.calls[0]?.[1]?.body))
      expect(body).toEqual({ custom_topic: { title: '仅标题' }, user_stance: 'con' })
    } finally {
      fetcher.mockRestore()
    }
  })

  it('标题边界按 Unicode 码点判定，与后端一致', async () => {
    const client = createApiClient({ getCsrfToken: () => 'test-csrf' })
    // 纯空白与空标题：客户端直接拒绝，不发请求。
    for (const title of ['', '   ', '\u3000\ufeff', '\u0085']) {
      await expect(createDebate({ custom_topic: { title }, user_stance: 'pro' }, client)).rejects.toMatchObject({ kind: 'format' })
    }
    for (const title of ['题'.repeat(CUSTOM_TOPIC_TITLE_MAX_CHARS + 1), '𠮷'.repeat(CUSTOM_TOPIC_TITLE_MAX_CHARS + 1)]) {
      await expect(createDebate({ custom_topic: { title }, user_stance: 'pro' }, client)).rejects.toMatchObject({ kind: 'format' })
    }
    // 补充平面字符按一个码点计：200 个允许，不应当成 400 个 UTF-16 码元。
    const fetcher = vi.spyOn(globalThis, 'fetch')
      .mockResolvedValueOnce(reply(customDebate('𠮷'.repeat(200), ''), 201))
      .mockResolvedValueOnce(reply(customDebate('题'.repeat(200), ''), 201))
    try {
      await expect(createDebate({ custom_topic: { title: '𠮷'.repeat(200) }, user_stance: 'pro' }, client)).resolves.toBeTruthy()
      await expect(createDebate({ custom_topic: { title: '题'.repeat(200) }, user_stance: 'pro' }, client)).resolves.toBeTruthy()
    } finally {
      fetcher.mockRestore()
    }
  })

  it('背景超过 1000 码点时客户端拒绝，恰好 1000 允许', async () => {
    const client = createApiClient({ getCsrfToken: () => 'test-csrf' })
    await expect(createDebate({
      custom_topic: { title: '题', background: '背'.repeat(CUSTOM_TOPIC_BACKGROUND_MAX_CHARS + 1) },
      user_stance: 'pro',
    }, client)).rejects.toMatchObject({ kind: 'format' })
    const fetcher = vi.spyOn(globalThis, 'fetch')
      .mockResolvedValueOnce(reply(customDebate('题', '背'.repeat(CUSTOM_TOPIC_BACKGROUND_MAX_CHARS)), 201))
    try {
      await expect(createDebate({
        custom_topic: { title: '题', background: '背'.repeat(CUSTOM_TOPIC_BACKGROUND_MAX_CHARS) },
        user_stance: 'pro',
      }, client)).resolves.toBeTruthy()
    } finally {
      fetcher.mockRestore()
    }
  })

  it('非字符串标题或背景类型错误都按格式错误拒绝', async () => {
    const client = createApiClient({ getCsrfToken: () => 'test-csrf' })
    const bad = [
      { custom_topic: { title: 1 }, user_stance: 'pro' },
      { custom_topic: { title: '题', background: 1 }, user_stance: 'pro' },
      { custom_topic: null, user_stance: 'pro' },
    ] as unknown as Parameters<typeof createDebate>[0][]
    for (const value of bad) {
      await expect(createDebate(value, client)).rejects.toMatchObject({ kind: 'format' })
    }
  })

  it('运行时拒绝两个选择键同时出现或同时缺失', async () => {
    const client = createApiClient({ getCsrfToken: () => 'test-csrf' })
    const cases = [
      { topic_id: '1', custom_topic: { title: '题' }, user_stance: 'pro' },
      { topic_id: '1', custom_topic: null, user_stance: 'pro' },
      { custom_topic: { title: '题' }, topic_id: null, user_stance: 'pro' },
      { user_stance: 'pro' },
    ] as unknown as Parameters<typeof createDebate>[0][]
    for (const value of cases) {
      await expect(createDebate(value, client)).rejects.toMatchObject({ kind: 'format' })
    }
  })

  it('预设调用方两种旧形状继续有效，请求键顺序不变', async () => {
    const fetcher = vi.spyOn(globalThis, 'fetch')
      .mockResolvedValueOnce(reply(customDebate('预设', '', '1'), 201))
      .mockResolvedValueOnce(reply(customDebate('预设', '', '1'), 201))
    try {
      const client = createApiClient({ getCsrfToken: () => 'test-csrf' })
      await expect(createDebate('1', 'pro', client)).resolves.toBeTruthy()
      expect(fetcher.mock.calls[0]?.[1]?.body).toBe('{"topic_id":"1","user_stance":"pro"}')
      await expect(createDebate({ topic_id: '1', user_stance: 'con', ai_style: 'data' }, client)).resolves.toBeTruthy()
      expect(fetcher.mock.calls[1]?.[1]?.body).toBe('{"topic_id":"1","user_stance":"con","ai_style":"data"}')
    } finally {
      fetcher.mockRestore()
    }
  })

  it('会话快照接受 null topic.id，但拒绝空标题', async () => {
    const fetcher = vi.spyOn(globalThis, 'fetch')
      .mockResolvedValueOnce(reply({ items: [customDebate('自定义', '背景')], total: 1, page: 1, page_size: 20 }, 200))
      .mockResolvedValueOnce(reply({ items: [{ ...customDebate('自定义', '背景'), topic: { id: null, title: '', description: '' } }], total: 1, page: 1, page_size: 20 }, 200))
    try {
      const client = createApiClient({ getCsrfToken: () => 'test-csrf' })
      const page = await createDebateApi(client).listDebates()
      expect(page.items[0]!.topic.id).toBeNull()
      expect(page.items[0]!.topic.title).toBe('自定义')
      await expect(createDebateApi(client).listDebates()).rejects.toMatchObject({ kind: 'format' })
    } finally {
      fetcher.mockRestore()
    }
  })
})
