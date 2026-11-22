// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it } from 'vitest'
import { readQuoteSelection, verifyQuoteInContent } from './selectQuote'

/**
 * V2-08 选段换算：浏览器选区是 UTF-16 下标，服务端坐标是 Unicode 码点半开区间。
 * 这里用同样的原文覆盖 emoji、补充平面字符与批注切分，防止两种下标混用。
 */

function mountContent(html: string): HTMLElement {
  document.body.innerHTML = `<div class="transcript"><article data-message-id="m-1"><p data-message-content>${html}</p></article></div>`
  return document.querySelector('[data-message-content]') as HTMLElement
}

/** 在单个文本节点内按 UTF-16 下标选区，模拟用户拖选。 */
function selectInNode(node: Text, start: number, end: number) {
  const range = document.createRange()
  range.setStart(node, start)
  range.setEnd(node, end)
  const selection = window.getSelection()!
  selection.removeAllRanges()
  selection.addRange(range)
}

describe('readQuoteSelection', () => {
  beforeEach(() => { window.getSelection()?.removeAllRanges() })
  afterEach(() => { document.body.innerHTML = '' })

  it('把 emoji 的 UTF-16 偏移换算成码点区间', () => {
    const element = mountContent('甲😀乙')
    const node = element.firstChild as Text
    // UTF-16 下标 0..3 对应码点区间 [0,3)：甲😀乙。
    selectInNode(node, 0, 3)
    expect(readQuoteSelection(document.body)).toEqual({ messageId: 'm-1', startCp: 0, endCp: 2, quote: '甲😀' })
  })

  it('补充平面字符各算一个码点，不按字节或 UTF-16 单元计数', () => {
    const element = mountContent('甲𠮷乙')
    const node = element.firstChild as Text
    selectInNode(node, 1, 3)
    expect(readQuoteSelection(document.body)).toEqual({ messageId: 'm-1', startCp: 1, endCp: 2, quote: '𠮷' })
  })

  it('批注把原文切成多个文本节点时仍按完整内容换算', () => {
    const element = mountContent('<span>甲</span><span>😀</span><span>乙</span>')
    const middle = (element.children[1]!.firstChild) as Text
    selectInNode(middle, 0, middle.length)
    expect(readQuoteSelection(document.body)).toEqual({ messageId: 'm-1', startCp: 1, endCp: 2, quote: '😀' })
  })

  it('没有选区、跨节点选区与纯空白选区都返回 null', () => {
    mountContent('甲😀乙')
    window.getSelection()?.removeAllRanges()
    expect(readQuoteSelection(document.body)).toBeNull()

    document.body.innerHTML = '<div class="transcript"><article data-message-id="m-1"><p data-message-content>甲</p></article><article data-message-id="m-2"><p data-message-content>乙</p></article></div>'
    const first = document.querySelectorAll('[data-message-content]')[0]!.firstChild as Text
    const second = document.querySelectorAll('[data-message-content]')[1]!.firstChild as Text
    const range = document.createRange()
    range.setStart(first, 0)
    range.setEnd(second, 1)
    const selection = window.getSelection()!
    selection.removeAllRanges()
    selection.addRange(range)
    expect(readQuoteSelection(document.body)).toBeNull()

    document.body.innerHTML = '<div class="transcript"><article data-message-id="m-1"><p data-message-content>  \uFEFF </p></article></div>'
    const blank = document.querySelector('[data-message-content]')!.firstChild as Text
    selectInNode(blank, 0, blank.length)
    expect(readQuoteSelection(document.body)).toBeNull()
  })
})

describe('verifyQuoteInContent', () => {
  it('按码点切片刻入并逐字比较', () => {
    expect(verifyQuoteInContent('甲😀乙', 1, 2, '😀')).toBe(true)
    expect(verifyQuoteInContent('甲😀乙', 0, 2, '甲😀')).toBe(true)
    // 坐标越界、逆序或原文不符都判失败。
    expect(verifyQuoteInContent('甲😀乙', 1, 9, '😀')).toBe(false)
    expect(verifyQuoteInContent('甲😀乙', 2, 1, '😀')).toBe(false)
    expect(verifyQuoteInContent('甲😀乙', 1, 2, '😁')).toBe(false)
    expect(verifyQuoteInContent('甲😀乙', -1, 2, '甲')).toBe(false)
  })
})
