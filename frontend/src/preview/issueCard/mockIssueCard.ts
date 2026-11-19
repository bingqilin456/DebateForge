import { ApiError } from '../../api/client'
import type { IssueCardApi } from '../../api/issueCard'
import type { IssueCardCitation, IssueCardItem, IssueCardList } from '../../types/issueCard'
import { ISSUE_CARD_DISPLAY_CAP } from '../../debate/issueCard'

/**
 * 争点卡隔离 Mock：只在本机回环的独立预览入口使用。
 *
 * 目的：在共享路由/装配尚未整合时，也能看到完整的赛后行为（5 卡上限、双方定位、
 * 待核对降级、改名/合并/移除、刷新读取、失败与版本冲突、越权 404）。
 *
 * 约束：
 * - 纯内存状态，刷新页面即回到初始夹具；不代表生产持久化已配置。
 * - 不调用真实模型、不访问后端、不写任何持久层。
 * - 坐标由夹具原文按码点计算，覆盖精确、组合字符、重复歧义与失效四类。
 */

export const MOCK_DEBATE_ID = '9001'
/** 与 MOCK_DEBATE_ID 非等值时用于演示“越权 404”。 */
export const MOCK_OTHER_DEBATE_ID = '9002'

export interface MockBehaviors {
  /** 读取 / 整理 / 编辑都按越权 404 返回。 */
  forbidden: boolean
  /** 下一次整理注入一次受控失败。 */
  failAnalyzeOnce: boolean
  /** 下一次编辑注入一次版本冲突。 */
  conflictEditOnce: boolean
}

export interface IssueCardPreviewMock {
  api: IssueCardApi
  /** message_id → 原文，供引用回读校验与定位。 */
  contents: Record<string, string>
  messages: readonly { id: string; role: 'user' | 'assistant'; content: string }[]
  behaviors: MockBehaviors
  /** 当前保存的卡片（含人工编辑）；供预览页展示与测试断言。 */
  snapshot(): { items: IssueCardItem[]; analysis: IssueCardList['analysis'] }
  reset(): void
}

/**
 * 可注入的持久化边界：Mock 用它验证“刷新后读取已保存的人工作业”。
 *
 * 默认用 sessionStorage（刷新保留、关闭标签页清除）；测试可注入内存实现。
 * 这只是隔离 Mock 的持久化替身，不代表生产持久化或数据库已配置。
 */
export interface IssueCardPreviewStorage {
  read(): string | null
  write(value: string): void
  clear(): void
}

export function sessionPreviewStorage(): IssueCardPreviewStorage {
  const key = 'issue-card-preview-state-v1'
  return {
    read() {
      try { return window.sessionStorage.getItem(key) } catch { return null }
    },
    write(value: string) {
      try { window.sessionStorage.setItem(key, value) } catch { /* 隐私模式下忽略，退回内存态 */ }
    },
    clear() {
      try { window.sessionStorage.removeItem(key) } catch { /* 同上 */ }
    },
  }
}

export function memoryPreviewStorage(): IssueCardPreviewStorage {
  let value: string | null = null
  return {
    read() { return value },
    write(next) { value = next },
    clear() { value = null },
  }
}

const USER_1 = '第一轮用户发言：评价标准应当可检验，否则双方无法比较。'
const AI_1 = '第一轮回应：可检验不等于可量化，量化本身的成本也要计入讨论。'
// “抽样”在原文中出现两次：作为第 5 张卡的提出原文，只能待核对。
const USER_2 = '第二轮用户发言：抽样的范围可以先说明，抽样的成本也可以估算。'
// 组合字符夹具：e + U+0301 是一个字素簇，按码点切开就会渲染破碎。
const AI_2 = '第二轮回应：抽样范围可以事先说明，抽样仍需要可靠口径。e\u0301'
const USER_3 = '第三轮用户发言：口径可以由双方在赛前约定，约定本身也能减少争议。'
const AI_3 = '第三轮回应：约定口径不等于约定权重，权重差异仍然存在。'
const USER_4 = '第四轮用户发言：权重差异可以在结辩前澄清，澄清后仍可比较。'
const AI_4 = '第四轮回应：澄清过晚会影响整场比较，因此时点也需要约定。'

const MESSAGES: Array<{ id: string; role: 'user' | 'assistant'; content: string }> = [
  { id: '101', role: 'user', content: USER_1 },
  { id: '102', role: 'assistant', content: AI_1 },
  { id: '103', role: 'user', content: USER_2 },
  { id: '104', role: 'assistant', content: AI_2 },
  { id: '105', role: 'user', content: USER_3 },
  { id: '106', role: 'assistant', content: AI_3 },
  { id: '107', role: 'user', content: USER_4 },
  { id: '108', role: 'assistant', content: AI_4 },
]

function points(content: string): string[] { return Array.from(content) }

/** 按码点统计唯一命中；出现 0 次或多次都返回 null，调用方据此构造待核对。 */
function uniqueRange(content: string, quote: string): { start: number; end: number } | null {
  const list = points(content)
  const needle = points(quote)
  if (needle.length === 0) return null
  const starts: number[] = []
  for (let i = 0; i + needle.length <= list.length; i++) {
    let matched = true
    for (let j = 0; j < needle.length; j++) {
      if (list[i + j] !== needle[j]) { matched = false; break }
    }
    if (matched) starts.push(i)
  }
  if (starts.length !== 1) return null
  return { start: starts[0]!, end: starts[0]! + needle.length }
}

function citationOf(
  contents: Record<string, string>,
  messageId: string,
  quote: string,
  speaker: 'user' | 'assistant',
  turnNo: number,
  note: string,
): IssueCardCitation {
  const range = uniqueRange(contents[messageId] ?? '', quote)
  return {
    anchor: {
      message_id: messageId,
      quote,
      // 重复歧义或未命中：不带坐标，只显示“待核对”，不猜位置。
      start_cp: range?.start ?? null,
      end_cp: range?.end ?? null,
      resolved: range !== null,
    },
    speaker,
    turn_no: turnNo,
    note,
  }
}

function messageIdOf(turn: number, role: 'user' | 'assistant'): string {
  const base = 101 + (turn - 1) * 2
  return String(role === 'user' ? base : base + 1)
}

interface FixturePair {
  turn: number
  title: string
  userQuote: string
  aiQuote: string
  pending: string[]
}

const FIXTURE_PAIRS: FixturePair[] = [
  { turn: 1, title: '争点：评价标准是否可检验', userQuote: '评价标准应当可检验', aiQuote: '可检验不等于可量化', pending: ['仍待澄清：可量化是否是可检验的必要条件？'] },
  { turn: 2, title: '争点：抽样范围是否可先说明', userQuote: '抽样的范围可以先说明', aiQuote: '抽样仍需要可靠口径', pending: ['仍待澄清：抽样范围由谁核定？'] },
  { turn: 3, title: '争点：口径可否事先约定', userQuote: '口径可以由双方在赛前约定', aiQuote: '约定口径不等于约定权重', pending: ['仍待澄清：约定失败的兜底规则是什么？'] },
  { turn: 4, title: '争点：权重差异能否结辩前澄清', userQuote: '权重差异可以在结辩前澄清', aiQuote: '澄清过晚会影响整场比较', pending: [] },
  // 第 5 张：提出原文“抽样”出现两次，重复歧义只能待核对；相关回应仍可定位。
  { turn: 2, title: '争点：抽样成本是否已估算', userQuote: '抽样', aiQuote: '抽样范围可以事先说明', pending: ['仍待澄清：范围说明的详略标准是什么？'] },
]

function initialItems(contents: Record<string, string>): IssueCardItem[] {
  return FIXTURE_PAIRS.map((pair, index) => ({
    id: String(5001 + index),
    title: pair.title,
    title_source: 'ai' as const,
    explanation: '回应是否切中问题需本人核对双方原句：提出方要求一个可操作标准，回应方指出该标准的成本或适用边界，二者未必正面交锋。',
    origin: citationOf(contents, messageIdOf(pair.turn, 'user'), pair.userQuote, 'user', pair.turn, '提出位置'),
    responses: [citationOf(contents, messageIdOf(pair.turn, 'assistant'), pair.aiQuote, 'assistant', pair.turn, '相关回应是否切中问题，需本人核对。')],
    pending: pair.pending,
    notices: [],
    version: 1,
  }))
}

function copyItem(item: IssueCardItem): IssueCardItem {
  return {
    ...item,
    origin: { ...item.origin, anchor: { ...item.origin.anchor } },
    responses: item.responses.map(response => ({ ...response, anchor: { ...response.anchor } })),
    pending: [...item.pending],
    notices: [...item.notices],
  }
}

/**
 * 构造内存 Mock API；`api` 的形状与 createIssueCardApi 一致，
 * 面板组件因此不需要任何 Mock 专用分支。
 */
export function createIssueCardPreviewMock(storage: IssueCardPreviewStorage = memoryPreviewStorage()): IssueCardPreviewMock {
  const contents: Record<string, string> = Object.fromEntries(MESSAGES.map(message => [message.id, message.content]))
  const behaviors: MockBehaviors = { forbidden: false, failAnalyzeOnce: false, conflictEditOnce: false }
  let items: IssueCardItem[] = []
  let analysis: IssueCardList['analysis'] = { status: 'not_started', attempt_version: 0 }

  /** 每次读改写都把当前状态写进持久化边界；人工编辑因此能跨刷新保留。 */
  function persist() {
    storage.write(JSON.stringify({ items, analysis }))
  }

  function restore(): boolean {
    const raw = storage.read()
    if (!raw) return false
    try {
      const parsed = JSON.parse(raw) as { items?: IssueCardItem[]; analysis?: IssueCardList['analysis'] }
      if (!Array.isArray(parsed.items) || !parsed.analysis) return false
      items = parsed.items
      analysis = parsed.analysis
      return true
    } catch {
      // 损坏的 Mock 状态按未保存处理，不影响页面可用。
      return false
    }
  }

  function reset() {
    items = initialItems(contents)
    analysis = { status: 'completed', attempt_version: 1 }
    behaviors.forbidden = false
    behaviors.failAnalyzeOnce = false
    behaviors.conflictEditOnce = false
    storage.clear()
    persist()
  }
  if (!restore()) reset()

  function snapshotList(): IssueCardList {
    return {
      state: 'ready',
      display_cap: ISSUE_CARD_DISPLAY_CAP,
      analysis: { ...analysis },
      // 服务端已按默认上限截断；Mock 保持同一语义，避免前端看到超过 5 张。
      items: items.slice(0, ISSUE_CARD_DISPLAY_CAP).map(copyItem),
    }
  }

  function requireOwnership() {
    if (behaviors.forbidden) {
      throw new ApiError('http', '请求的资源不存在或不可访问。', { status: 404, code: 'not_found' })
    }
  }

  function findItem(cardId: string): IssueCardItem {
    const found = items.find(entry => entry.id === cardId)
    if (!found) throw new ApiError('http', '请求的资源不存在或不可访问。', { status: 404, code: 'not_found' })
    return found
  }

  const api: IssueCardApi = {
    async list() {
      requireOwnership()
      return snapshotList()
    },
    async analyze(_debateId, request = {}) {
      requireOwnership()
      // 重复触发只读回既有结果，不产生新卡、不改版本。
      if (analysis.status === 'completed' && !request.retry) return snapshotList()
      if (behaviors.failAnalyzeOnce) {
        behaviors.failAnalyzeOnce = false
        analysis = { status: 'failed', attempt_version: analysis.attempt_version + 1, error_code: 'model_error' }
        persist()
        throw new ApiError('http', 'AI 回复生成失败，请查询会话状态。', { status: 502, code: 'model_error' })
      }
      analysis = { status: 'completed', attempt_version: analysis.attempt_version + 1 }
      // 晚到整理只补兼容内容：人工标题保持，冲突写进 notices。
      items = items.map(item => item.title_source === 'user'
        ? { ...item, notices: [...new Set([...item.notices, '晚到分析未覆盖你的人工标题。'])] }
        : item)
      persist()
      return snapshotList()
    },
    async edit(_debateId, cardId, request) {
      requireOwnership()
      if (behaviors.conflictEditOnce) {
        behaviors.conflictEditOnce = false
        throw new ApiError('http', '请求失败，请稍后手动重试。', { status: 409, code: 'issue_card_version_conflict' })
      }
      const item = findItem(cardId)
      // 与真实服务端一致：版本不符返回 409，不覆盖已保存数据。
      if (item.version !== request.expected_version) {
        throw new ApiError('http', '请求失败，请稍后手动重试。', { status: 409, code: 'issue_card_version_conflict' })
      }
      if (request.action === 'rename') {
        const updated: IssueCardItem = { ...item, title: request.title, title_source: 'user', version: item.version + 1 }
        items = items.map(entry => entry.id === cardId ? updated : entry)
        persist()
        return copyItem(updated)
      }
      if (request.action === 'remove') {
        items = items.filter(entry => entry.id !== cardId)
        persist()
        return copyItem({ ...item, version: item.version + 1 })
      }
      const target = findItem(request.target_card_id)
      if (target.id === item.id) {
        throw new ApiError('http', '输入不符合要求，请检查后重试。', { status: 400, code: 'invalid_request' })
      }
      const merged: IssueCardItem = {
        ...target,
        // 合并保留双方原句链接：被合并卡的提出原文与相关回应都进入目标卡。
        responses: [
          ...target.responses,
          { ...item.origin, note: '已合并来源的提出原文' },
          ...item.responses,
        ].slice(0, 6),
        pending: [...target.pending, ...item.pending].slice(0, 5),
        notices: [...target.notices, `已合并「${item.title}」，其原句链接保留在本卡。`],
        version: target.version + 1,
      }
      items = items.filter(entry => entry.id !== cardId).map(entry => entry.id === target.id ? merged : entry)
      persist()
      return copyItem(merged)
    },
  }

  return {
    api,
    contents,
    messages: MESSAGES,
    behaviors,
    snapshot() {
      return { items: items.map(copyItem), analysis: { ...analysis } }
    },
    reset,
  }
}
