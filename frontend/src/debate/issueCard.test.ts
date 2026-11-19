// @vitest-environment jsdom
import { describe, expect, it } from 'vitest'
import {
  ISSUE_CARD_DISPLAY_CAP,
  buildIssueCardViews,
  findIssueCardMarker,
  issueCardAnalysisMessage,
  issueCardMergePreview,
  issueCardRemovePreview,
  issueCardStateMessage,
  issueCardTitleAccessibleName,
  projectIssueCardCitation,
  splitCitation,
} from './issueCard'
import type { IssueCardAnchor, IssueCardCitation, IssueCardItem, IssueCardList } from '../types/issueCard'

function anchor(quote: string, start: number | null, end: number | null, resolved = true): IssueCardAnchor {
  return { message_id: '101', quote, start_cp: start, end_cp: end, resolved }
}

function citation(quote: string, start: number | null, end: number | null, resolved = true, speaker: 'user' | 'assistant' = 'user'): IssueCardCitation {
  return { anchor: anchor(quote, start, end, resolved), speaker, turn_no: 1, note: '核对说明' }
}

function item(overrides: Partial<IssueCardItem> = {}): IssueCardItem {
  return {
    id: '1001', title: '争点：标准可检验', title_source: 'ai', explanation: '回应是否切中问题需要本人核对原句。',
    origin: citation('标准应当可检验', 0, 7), responses: [citation('可检验不等于可量化', 0, 8, true, 'assistant')],
    pending: ['仍待澄清：前提是否被接受？'], notices: [], version: 1, ...overrides,
  }
}

function list(items: IssueCardItem[], overrides: Partial<IssueCardList> = {}): IssueCardList {
  return {
    state: 'ready', display_cap: ISSUE_CARD_DISPLAY_CAP,
    analysis: { status: 'completed', attempt_version: 1 }, items, ...overrides,
  }
}

const contents = { '101': '标准应当可检验' }

describe('争点卡引用回读校验', () => {
  it('精确命中才给坐标，并按码点回读', () => {
    const view = projectIssueCardCitation(citation('标准应当可检验', 0, 7), contents)
    expect(view.status).toBe('verified')
    expect(view.start).toBe(0)
    expect(view.end).toBe(7)
    expect(splitCitation(contents['101']!, view)).toEqual({
      before: '', quote: '标准应当可检验', after: '', verified: true,
    })
  })

  it('坐标与原文不一致、越界、倒序时降级为待核对且不猜位置', () => {
    for (const anchorView of [
      citation('标准应当可检验', 1, 8),
      citation('标准应当可检验', 0, 99),
      citation('标准应当可检验', 5, 2),
      citation('标准应当可检验', null, null, false),
    ]) {
      const view = projectIssueCardCitation(anchorView, contents)
      expect(view.status).toBe('pending')
      expect(view.start).toBeNull()
      expect(view.end).toBeNull()
      // 待核对仍保留原句供本人核对。
      expect(view.citation.anchor.quote).toBe('标准应当可检验')
    }
  })

  it('原文缺失或消息不属于本场时降级，不抛错', () => {
    const view = projectIssueCardCitation(citation('标准应当可检验', 0, 7), {})
    expect(view.status).toBe('pending')
  })

  it('组合字符切中可见字素时降级为待核对', () => {
    const combining = { '101': 'e\u0301x' }
    const good = projectIssueCardCitation(citation('e\u0301', 0, 2), combining)
    expect(good.status).toBe('verified')
    const split = projectIssueCardCitation(citation('e', 0, 1), combining)
    expect(split.status).toBe('pending')
  })

  it('UTF-16 下标不是码点下标：emoji 不错标', () => {
    const content = '🌱理由：标准应当可检验'
    const emojiContents = { '101': content }
    const points = Array.from(content)
    const start = points.indexOf('标')
    const view = projectIssueCardCitation(citation('标准应当可检验', start, start + 7), emojiContents)
    expect(view.status).toBe('verified')
    expect(splitCitation(content, view).before).toBe('🌱理由：')
  })
})

describe('争点卡视图', () => {
  it('默认最多展示 5 张，保持服务端顺序与身份', () => {
    const items = Array.from({ length: 7 }, (_, index) => item({ id: String(1001 + index), title: `争点 ${index + 1}` }))
    const views = buildIssueCardViews(list(items), contents)
    expect(views).toHaveLength(ISSUE_CARD_DISPLAY_CAP)
    expect(views.map(view => view.item.id)).toEqual(['1001', '1002', '1003', '1004', '1005'])
  })

  it('双方引用各自校验：提出位置失效时相关回应仍可定位', () => {
    const views = buildIssueCardViews(list([
      item({ origin: citation('标准应当可检验', 3, 10) }),
    ]), contents)
    expect(views[0]!.origin.status).toBe('pending')
    expect(views[0]!.responses[0]!.status).toBe('pending')
    expect(views[0]!.canLocate).toBe(false)

    const mixed = buildIssueCardViews(list([
      item({ origin: citation('标准应当可检验', 3, 10), responses: [citation('标准应当可检验', 0, 7, true, 'assistant')] }),
    ]), contents)
    expect(mixed[0]!.origin.status).toBe('pending')
    expect(mixed[0]!.responses[0]!.status).toBe('verified')
    expect(mixed[0]!.canLocate).toBe(true)
  })

  it('隔离状态给出原因，不写成“没有争点”', () => {
    expect(issueCardStateMessage('not_ready')).toContain('完赛后')
    expect(issueCardStateMessage('unsupported')).toContain('三轮快速赛')
    expect(issueCardStateMessage('ready')).toBe('')
    const views = buildIssueCardViews(list([item()], { state: 'unsupported', items: [] }), contents)
    expect(views).toEqual([])
  })

  it('分析失败说明保留已有卡片，不伪装成未开始', () => {
    const message = issueCardAnalysisMessage({ status: 'failed', attempt_version: 2, error_code: 'model_error' })
    expect(message).toContain('model_error')
    expect(message).toContain('不受影响')
    expect(issueCardAnalysisMessage({ status: 'not_started', attempt_version: 0 })).toContain('手动整理')
  })

  it('人工标题只标注来源，不展示原名对照', () => {
    expect(issueCardTitleAccessibleName(item({ title_source: 'user', title: '人工义' }))).toContain('人工标题')
    const preview = issueCardMergePreview(item({ title: '甲' }), item({ id: '1002', title: '乙' }))
    expect(preview).toContain('甲')
    expect(preview).toContain('乙')
    expect(preview).toContain('保留双方')
    const remove = issueCardRemovePreview(item({ title: '甲' }))
    expect(remove).toContain('不会因重新整理而复活')
  })

  it('按 dataset 查找标记，不拼接 CSS 选择器', () => {
    const host = document.createElement('div')
    host.innerHTML = '<button data-issue-card-citation="101|0|7"></button>'
    expect(findIssueCardMarker(host, '101|0|7')).not.toBeNull()
    expect(findIssueCardMarker(host, '101|1|8')).toBeNull()
    expect(findIssueCardMarker(null, '101|0|7')).toBeNull()
  })
})
