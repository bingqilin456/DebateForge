/**
 * 把浏览器选区换算成服务端认可的原句锚点。
 *
 * 坐标约定：从 0 开始的 Unicode 码点半开区间 [startCp, endCp)，与后端锚点、
 * 复盘定位完全一致。浏览器 Selection/Range 的 offset 是 UTF-16 下标，直接使用
 * 会让 emoji 与补充平面字符错位，因此必须按码点重新计算。
 *
 * 落点选择（而不是逐字节点遍历）：以“从内容节点起点到选区起点”的文本码点数
 * 作为 start，以“从起点到选区终点”的码点数作为 end。这样即使批注把原文切成
 * 多个逐字节点，换算结果仍只依赖呈现出来的文本本身。
 */

export interface QuoteSelection {
  messageId: string
  startCp: number
  endCp: number
  quote: string
}

/** 找到包含当前选区的消息内容节点；选区跨节点或不在该消息内时返回 null。 */
function contentElementOf(node: Node | null): HTMLElement | null {
  if (!node) return null
  const element = node.nodeType === Node.ELEMENT_NODE ? (node as HTMLElement) : node.parentElement
  return element?.closest<HTMLElement>('[data-message-content]') ?? null
}

/**
 * 读取 `root` 内当前选区对应的码点区间。
 *
 * 返回 null 表示：没有选区、选区不在 root 内、选区跨出内容节点或选区为空白。
 * 调用方应据此提示“请先在本人发言中选一段原文”，而不是猜一个位置。
 */
export function readQuoteSelection(root: ParentNode | null): QuoteSelection | null {
  if (!root || typeof window === 'undefined') return null
  const selection = window.getSelection()
  if (!selection || selection.rangeCount === 0 || selection.isCollapsed) return null
  const range = selection.getRangeAt(0)
  const startElement = contentElementOf(range.startContainer)
  const endElement = contentElementOf(range.endContainer)
  if (!startElement || startElement !== endElement || !root.contains(startElement)) return null
  if (!startElement.contains(range.commonAncestorContainer)) return null
  const messageId = startElement.closest<HTMLElement>('[data-message-id]')?.dataset.messageId
  if (!messageId) return null

  // 从内容节点起点到选区起点/终点的文本，按码点数就是区间端点。
  const before = range.cloneRange()
  before.selectNodeContents(startElement)
  before.setEnd(range.startContainer, range.startOffset)

  const upToEnd = range.cloneRange()
  upToEnd.selectNodeContents(startElement)
  upToEnd.setEnd(range.endContainer, range.endOffset)

  const startCp = Array.from(before.toString()).length
  const endCp = Array.from(upToEnd.toString()).length
  const quote = Array.from(currentText(startElement)).slice(startCp, endCp).join('')
  if (endCp <= startCp || quote !== range.toString() || !/[^\p{White_Space}\uFEFF]/u.test(quote)) return null
  return { messageId, startCp, endCp, quote }
}

/** 内容节点的可读文本：优先 textContent，保证与渲染结果逐字一致。 */
function currentText(element: HTMLElement): string {
  return element.textContent ?? ''
}

/**
 * 在给定消息内容里按码点区间复核原句。
 *
 * 复盘/争点带来的是服务端已核对过的引用，但页面上的消息集合可能因刷新或
 * 版本差异变化；提交前再核对一次，避免把不匹配的坐标送进服务端。
 */
export function verifyQuoteInContent(content: string, startCp: number, endCp: number, quote: string): boolean {
  if (!Number.isInteger(startCp) || !Number.isInteger(endCp) || startCp < 0 || endCp <= startCp) return false
  const codepoints = Array.from(content)
  if (endCp > codepoints.length) return false
  return codepoints.slice(startCp, endCp).join('') === quote
}
