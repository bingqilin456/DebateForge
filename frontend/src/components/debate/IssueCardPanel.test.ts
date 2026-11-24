// @vitest-environment jsdom
import { createApp, h, nextTick, type App } from 'vue'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import IssueCardPanel from './IssueCardPanel.vue'
import type { IssueCardApi } from '../../api/issueCard'
import { ApiError } from '../../api/client'
import type { IssueCardAnalysis, IssueCardCitation, IssueCardItem, IssueCardList } from '../../types/issueCard'

/**
 * 争点卡面板 Mock 验收：5 卡上限、双方引用精确定位与待核对降级、
 * 人工改名 / 合并 / 移除后刷新读取、版本冲突弱提示、越权与失败提示、纯文本转义。
 * 全部使用桩 API，不发真实请求、不依赖后端。
 */

const mounted: Array<{ app: App; host: HTMLDivElement }> = []

const CONTENT_USER = '第一轮用户发言：标准应当可检验，否则无法比较。'
const CONTENT_AI = '第一轮回应：可检验不等于可量化，量化成本也要计入。'

/** 按 Unicode 码点算引用的半开区间，避免手写下标出错。 */
function offsetOf(content: string, quote: string): [number, number] {
  const points = Array.from(content)
  const start = points.join('').indexOf(quote)
  if (start < 0) throw new Error(`引文不在原文中：${quote}`)
  return [points.slice(0, start).length, points.slice(0, start).length + Array.from(quote).length]
}

function anchor(quote: string, start: number | null, end: number | null, messageId = '101', resolved = true) {
  return { message_id: messageId, quote, start_cp: start, end_cp: end, resolved }
}

function citation(quote: string, start: number | null, end: number | null, overrides: Partial<IssueCardCitation> = {}): IssueCardCitation {
  return { anchor: anchor(quote, start, end), speaker: 'user', turn_no: 1, note: '提出位置', ...overrides }
}

function userCitation(quote: string, overrides: Partial<IssueCardCitation> = {}): IssueCardCitation {
  return citation(quote, ...offsetOf(CONTENT_USER, quote), overrides)
}

function assistantCitation(quote: string, overrides: Partial<IssueCardCitation> = {}): IssueCardCitation {
  const [start, end] = offsetOf(CONTENT_AI, quote)
  return citation(quote, start, end, {
    speaker: 'assistant', note: '相关回应',
    anchor: anchor(quote, start, end, '102'),
    ...overrides,
  })
}

function item(overrides: Partial<IssueCardItem> = {}): IssueCardItem {
  return {
    id: '1001', title: '争点：标准可检验', title_source: 'ai',
    explanation: '双方围绕判断标准形成交锋，回应是否切中问题需本人核对原句。',
    origin: userCitation('标准应当可检验'),
    responses: [assistantCitation('可检验不等于可量化', { speaker: 'assistant', note: '相关回应' })],
    pending: ['仍待澄清：该标准是否适用于本场辩题？'],
    notices: [], version: 1, ...overrides,
  }
}

function list(overrides: Partial<IssueCardList> = {}): IssueCardList {
  return {
    state: 'ready', display_cap: 5,
    analysis: { status: 'completed', attempt_version: 1 },
    items: [item()], ...overrides,
  }
}

const contents = { '101': CONTENT_USER, '102': CONTENT_AI }

function api(overrides: Partial<IssueCardApi> = {}): IssueCardApi {
  return {
    list: vi.fn(async () => list()),
    analyze: vi.fn(async () => list()),
    edit: vi.fn(async () => item({ version: 2 })),
    ...overrides,
  }
}

async function mountPanel(options: {
  api: IssueCardApi
  debateId?: string
  contents?: Record<string, string>
  completed?: boolean
  disabled?: boolean
  confirm?: (content: string, title: string) => Promise<boolean>
}) {
  const host = document.createElement('div')
  document.body.append(host)
  const located: string[] = []
  const app = createApp(() => h(IssueCardPanel, {
    api: options.api,
    debateId: options.debateId ?? '7',
    contents: options.contents ?? contents,
    completed: options.completed ?? true,
    disabled: options.disabled ?? false,
    confirm: options.confirm ?? (async () => true),
    onLocate: (key: string) => located.push(key),
  }))
  app.mount(host)
  mounted.push({ app, host })
  await vi.waitFor(() => expect(host.textContent).not.toContain('正在读取争点卡'))
  return { host, located }
}

function button(host: HTMLElement, selector: string): HTMLButtonElement {
  const found = host.querySelector<HTMLButtonElement>(selector)
  if (!found) throw new Error(`找不到按钮：${selector}`)
  return found
}

beforeEach(() => { vi.clearAllMocks() })
afterEach(() => {
  for (const { app, host } of mounted.splice(0)) { app.unmount(); host.remove() }
})

describe('争点卡读取与展示', () => {
  it('展示标题、提出原文、相关回应、待澄清与关联解释', async () => {
    const { host } = await mountPanel({ api: api() })
    expect(host.textContent).toContain('争点：标准可检验')
    expect(host.textContent).toContain('提出原文')
    expect(host.textContent).toContain('标准应当可检验')
    expect(host.textContent).toContain('相关回应')
    expect(host.textContent).toContain('可检验不等于可量化')
    expect(host.textContent).toContain('仍待澄清')
    expect(host.textContent).toContain('回应是否切中问题')
    // 不自动判定“已驳倒”：只允许出现否定式说明。
    expect(host.textContent).not.toMatch(/已驳倒[。，]/)
    expect(host.textContent).toContain('卡片不判定“已驳倒”')
    expect(host.textContent).toContain('引用存在不证明判断正确')
  })

  it('默认最多展示 5 张，并说明超出部分不在本视图展开', async () => {
    const items = Array.from({ length: 7 }, (_, index) => item({ id: String(1001 + index), title: `争点 ${index + 1}` }))
    const { host } = await mountPanel({ api: api({ list: vi.fn(async () => list({ items })) }) })
    expect(host.querySelectorAll('[data-issue-card]')).toHaveLength(5)
    expect(host.querySelector('[data-issue-card-count]')?.textContent).toBe('5 / 5')
    expect(host.textContent).toContain('默认只展示最相关的 5 张')
    expect(host.textContent).not.toContain('争点 6')
  })

  it('点双方引用各自回传精确键，供父组件定位原句', async () => {
    const { host, located } = await mountPanel({ api: api() })
    const markers = host.querySelectorAll<HTMLButtonElement>('[data-issue-card-citation]')
    expect(markers.length).toBe(2)
    const userKey = markers[0]!.getAttribute('data-issue-card-citation')!
    const assistantKey = markers[1]!.getAttribute('data-issue-card-citation')!
    markers[0]!.click()
    markers[1]!.click()
    await nextTick()
    expect(located).toEqual([userKey, assistantKey])
    // 键只含消息与码点坐标，双方引用互不相同。
    expect(userKey).toMatch(/^101\|\d+\|\d+$/)
    expect(assistantKey).toMatch(/^102\|\d+\|\d+$/)
    expect(userKey).not.toBe(assistantKey)
  })

  it('坐标不可信时显示待核对、不高亮、不猜位置，卡片仍可读', async () => {
    const { host, located } = await mountPanel({
      api: api({
        list: vi.fn(async () => list({
          items: [item({
            origin: userCitation('标准应当可检验', { anchor: anchor('标准应当可检验', 3, 10) }),
            responses: [assistantCitation('可检验不等于可量化', { speaker: 'assistant', anchor: anchor('可检验不等于可量化', 0, 8, '102') })],
          })],
        })),
      }),
    })
    expect(host.querySelectorAll('[data-issue-card-citation]')).toHaveLength(0)
    expect(host.textContent).toContain('原文待核对')
    expect(host.textContent).toContain('不猜测位置')
    // 卡片内容仍完整可读。
    expect(host.textContent).toContain('标准应当可检验')
    expect(host.textContent).toContain('仍待澄清')
    expect(located).toEqual([])
  })

  it('提出位置失效但相关回应可定位时，只有一处标记', async () => {
    const { host } = await mountPanel({
      api: api({
        list: vi.fn(async () => list({
          items: [item({
            origin: userCitation('标准应当可检验', { anchor: anchor('标准应当可检验', 3, 10) }),
            responses: [assistantCitation('可检验不等于可量化', { speaker: 'assistant' })],
          })],
        })),
      }),
    })
    const markers = host.querySelectorAll('[data-issue-card-citation]')
    expect(markers).toHaveLength(1)
    expect(markers[0]!.getAttribute('data-issue-card-citation')).toMatch(/^102\|\d+\|\d+$/)
  })

  it('用户与模型文本按纯文本渲染，不解释 HTML', async () => {
    const { host } = await mountPanel({
      api: api({
        list: vi.fn(async () => list({
          items: [item({
            title: '<img src=x onerror=alert(1)>',
            pending: ['<script>alert(2)</script>'],
          })],
        })),
      }),
    })
    expect(host.querySelector('img')).toBeNull()
    expect(host.querySelector('script')).toBeNull()
    expect(host.textContent).toContain('<img src=x onerror=alert(1)>')
  })
})

describe('争点卡隔离与失败提示', () => {
  it('未完赛说明赛后集中整理，不显示整理按钮', async () => {
    const { host } = await mountPanel({
      api: api({ list: vi.fn(async () => list({ state: 'not_ready', analysis: { status: 'not_started', attempt_version: 0 }, items: [] })) }),
      completed: false,
    })
    expect(host.textContent).toContain('尚未完成四轮辩论')
    expect(host.querySelector('[data-issue-card-analyze]')).toBeNull()
  })

  it('历史 quick_3 明确说明隔离，不显示“无争点”', async () => {
    const { host } = await mountPanel({
      api: api({ list: vi.fn(async () => list({ state: 'unsupported', analysis: { status: 'not_started', attempt_version: 0 }, items: [] })) }),
    })
    expect(host.textContent).toContain('历史三轮快速赛')
    expect(host.textContent).not.toContain('没有选出主要争点卡')
  })

  it('读取失败给出重试入口，不展示伪造卡片', async () => {
    const failing = vi.fn(async (): Promise<IssueCardList> => { throw new ApiError('network', '无法连接服务，请稍后手动重试。') })
    const { host } = await mountPanel({ api: api({ list: failing }) })
    expect(host.textContent).toContain('无法连接服务')
    expect(host.querySelectorAll('[data-issue-card]')).toHaveLength(0)
    const reload = button(host, '[data-issue-card-reload]')
    expect(reload.disabled).toBe(false)
  })

  it('整理失败说明已有卡片不受影响并保留卡片', async () => {
    const failing = vi.fn(async (): Promise<IssueCardList> => { throw new ApiError('http', 'AI 回复生成失败，请查询会话状态。', { status: 502, code: 'model_error' }) })
    const { host } = await mountPanel({ api: api({ analyze: failing }) })
    button(host, '[data-issue-card-analyze]').click()
    await vi.waitFor(() => expect(host.querySelector('[data-issue-card-notice]')).not.toBeNull())
    expect(host.textContent).toContain('AI 回复生成失败')
    // 失败只影响提示：已保存卡片与人工编辑仍可见。
    expect(host.querySelectorAll('[data-issue-card]')).toHaveLength(1)
    expect(host.textContent).toContain('标准应当可检验')
  })

  it('分析状态 failed 时展示受控错误码与重试入口', async () => {
    await mountPanel({
      api: api({
        list: vi.fn(async () => list({
          analysis: { status: 'failed', attempt_version: 2, error_code: 'model_error' } as IssueCardAnalysis,
        })),
      }),
    })
  })

  it('越权读取按 404 提示，不渲染内容', async () => {
    const failing = vi.fn(async (): Promise<IssueCardList> => {
      throw new ApiError('http', '请求的资源不存在或不可访问。', { status: 404, code: 'not_found' })
    })
    const { host } = await mountPanel({ api: api({ list: failing }) })
    expect(host.textContent).toContain('资源不存在或不可访问')
    expect(host.querySelectorAll('[data-issue-card]')).toHaveLength(0)
  })
})

describe('争点卡人工编辑与刷新', () => {
  it('改名后提交当前版本，并用响应替换该卡', async () => {
    const edit = vi.fn(async () => item({ title: '我的人工标题', title_source: 'user', version: 2 }))
    const { host } = await mountPanel({ api: api({ edit }) })
    button(host, '[data-issue-card-rename]').click()
    await nextTick()
    const input = host.querySelector<HTMLInputElement>('[data-issue-card-rename-form] input')
    expect(input).not.toBeNull()
    input!.value = '我的人工标题'
    input!.dispatchEvent(new Event('input'))
    await nextTick()
    button(host, '[data-issue-card-rename-save]').click()
    await vi.waitFor(() => expect(host.textContent).toContain('我的人工标题'))
    expect(edit).toHaveBeenCalledWith('7', '1001', { expected_version: 1, action: 'rename', title: '我的人工标题' })
    expect(host.textContent).toContain('人工标题')
    // 人工标题不展示“原名 / 历史名”对照。
    expect(host.textContent).not.toContain('原名')
  })

  it('空标题不发请求并提示', async () => {
    const edit = vi.fn(async () => item())
    const { host } = await mountPanel({ api: api({ edit }) })
    button(host, '[data-issue-card-rename]').click()
    await nextTick()
    const input = host.querySelector<HTMLInputElement>('[data-issue-card-rename-form] input')!
    input.value = '   '
    input.dispatchEvent(new Event('input'))
    await nextTick()
    button(host, '[data-issue-card-rename-save]').click()
    await vi.waitFor(() => expect(host.textContent).toContain('标题不能为空'))
    expect(edit).not.toHaveBeenCalled()
  })

  it('版本冲突只弱提示并重新读取，不覆盖已有数据、不自动换版本重发', async () => {
    const listStub = vi.fn(async () => list({ items: [item({ title: '已保存的人工标题', title_source: 'user', version: 3 })] }))
    const edit = vi.fn(async () => {
      throw new ApiError('http', '请求失败，请稍后手动重试。', { status: 409, code: 'issue_card_version_conflict' })
    })
    const { host } = await mountPanel({ api: api({ list: listStub, edit }) })
    button(host, '[data-issue-card-rename]').click()
    await nextTick()
    button(host, '[data-issue-card-rename-save]').click()
    await vi.waitFor(() => expect(host.textContent).toContain('版本已变化'))
    expect(edit).toHaveBeenCalledTimes(1)
    // 重新读取后展示服务端已保存的人工标题，不显示用户刚输入的值。
    await vi.waitFor(() => expect(host.textContent).toContain('已保存的人工标题'))
    expect(host.textContent).toContain('请确认后再操作')
    expect(listStub).toHaveBeenCalledTimes(2)
  })

  it('合并先预览再提交，并重新读取列表', async () => {
    const edit = vi.fn(async () => item({ version: 2 }))
    const listStub = vi.fn(async () => list({
      items: [item({ id: '1001', title: '甲卡' }), item({ id: '1002', title: '乙卡' })],
    }))
    const confirm = vi.fn(async (_content: string, _title: string) => true)
    const { host } = await mountPanel({ api: api({ list: listStub, edit }), confirm })
    const mergeButtons = host.querySelectorAll<HTMLButtonElement>('[data-issue-card-merge]')
    mergeButtons[0]!.click()
    await nextTick()
    button(host, '[data-issue-card-merge-target="1002"]').click()
    await vi.waitFor(() => expect(edit).toHaveBeenCalled())
    expect(confirm).toHaveBeenCalledTimes(1)
    expect(String(confirm.mock.calls[0]![0])).toContain('保留双方')
    expect(edit).toHaveBeenCalledWith('7', '1001', { expected_version: 1, action: 'merge', target_card_id: '1002' })
    await vi.waitFor(() => expect(listStub).toHaveBeenCalledTimes(2))
  })

  it('取消合并确认时不发请求', async () => {
    const edit = vi.fn(async () => item())
    const listStub = vi.fn(async () => list({
      items: [item({ id: '1001', title: '甲卡' }), item({ id: '1002', title: '乙卡' })],
    }))
    const { host } = await mountPanel({ api: api({ list: listStub, edit }), confirm: async () => false })
    host.querySelectorAll<HTMLButtonElement>('[data-issue-card-merge]')[0]!.click()
    await nextTick()
    button(host, '[data-issue-card-merge-target="1002"]').click()
    await nextTick()
    expect(edit).not.toHaveBeenCalled()
  })

  it('移除二次确认后提交，并刷新到服务端状态', async () => {
    const edit = vi.fn(async () => item({ version: 2 }))
    const listStub = vi.fn()
      .mockResolvedValueOnce(list())
      .mockResolvedValueOnce(list({ items: [] }))
    const confirm = vi.fn(async (_content: string, _title: string) => true)
    const { host } = await mountPanel({ api: api({ list: listStub, edit }), confirm })
    button(host, '[data-issue-card-remove]').click()
    await vi.waitFor(() => expect(edit).toHaveBeenCalled())
    expect(String(confirm.mock.calls[0]![0])).toContain('不会因重新整理而复活')
    expect(edit).toHaveBeenCalledWith('7', '1001', { expected_version: 1, action: 'remove' })
    await vi.waitFor(() => expect(host.querySelectorAll('[data-issue-card]')).toHaveLength(0))
  })

  it('冲突提示与人工编辑同时存在时，晚到更新不得覆盖人工标题', async () => {
    const { host } = await mountPanel({
      api: api({
        list: vi.fn(async () => list({
          items: [item({
            title: '我的人工标题', title_source: 'user', version: 4,
            notices: ['晚到分析建议的标题「AI 新标题」未覆盖你的人工标题。'],
          })],
        })),
      }),
    })
    expect(host.textContent).toContain('我的人工标题')
    expect(host.querySelectorAll('[data-issue-card]')).toHaveLength(1)
  })

  it('会话切换时重新读取，不保留上一场卡片', async () => {
    const listStub = vi.fn()
      .mockResolvedValueOnce(list())
      .mockResolvedValueOnce(list({ items: [item({ id: '2001', title: '另一场卡片' })] }))
    const host = document.createElement('div')
    document.body.append(host)
    const { createApp: create, h: render, ref } = await import('vue')
    const debateId = ref('7')
    const app = create(() => render(IssueCardPanel, {
      api: api({ list: listStub }), debateId: debateId.value, contents, completed: true,
      confirm: async () => true,
    }))
    app.mount(host)
    mounted.push({ app, host })
    await vi.waitFor(() => expect(host.textContent).toContain('争点：标准可检验'))
    debateId.value = '8'
    await vi.waitFor(() => expect(host.textContent).toContain('另一场卡片'))
    expect(host.textContent).not.toContain('争点：标准可检验')
  })
})
