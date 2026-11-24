/**
 * 复盘引用与争点卡引用共用的原文定位。
 *
 * 之所以抽成共享工具：正式工作台的复盘面板与两个预览页都要“点引用 → 回到原文”，
 * 若各写一套，落点语义（码点区间、越界处理、是否清掉上一次标记）会漂移。
 * 视觉不在这里：`data-located-quote` 的外框统一由 styles/theme.css 提供，
 * 所有入口都引入了该样式文件，因此同一个定位函数在正式页与预览页看起来一致。
 *
 * 坐标约定：Unicode 码点半开区间 [startCp, endCp)，与后端 quote 锚点一致；
 * 不使用 UTF-16 下标，避免代理对（emoji）与组合字符算错位置。
 */

/** 命中后的落点：附上实际引用文本，调用方不必再自行切片。 */
export interface LocatedQuote {
  /** 承载 `data-located-quote` 的整段原文节点（不是逐字节点）。 */
  element: HTMLElement
  /** 引用区间对应的原文，逐码点精确切片。 */
  quote: string
}

/** 清掉同一发言流里上一次的落点，保证切换目标后只有一处可见框。 */
function clearLocatedIn(root: ParentNode): void {
  for (const element of root.querySelectorAll<HTMLElement>('[data-located-quote]')) {
    element.removeAttribute('data-located-quote')
  }
}

/** 复用批注标记 DOM 键，定位侧栏解释对应的原文。 */
export function locateAnnotationMarker(root: ParentNode | null, key: string): void {
  const candidates = root?.querySelectorAll<HTMLButtonElement>('[data-annotation-keys]')
  const marker = Array.from(candidates ?? []).find(element =>
    element.dataset.annotationKeys?.split('\u0001').includes(key),
  )
  marker?.scrollIntoView?.({ block: 'center' })
  marker?.focus({ preventScroll: true })
}

/**
 * 按 messageId 找到原文段落节点。
 *
 * 用 dataset 比对而不是拼 `[data-message-id="${id}"]` 选择器：ID 由后端下发，
 * 拼接选择器在含引号/方括号时会失效甚至注入，dataset 比对没有这个风险。
 */
function findContentElement(root: ParentNode, messageId: string): HTMLElement | null {
  for (const article of root.querySelectorAll<HTMLElement>('[data-message-id]')) {
    if (article.dataset.messageId !== messageId) continue
    const content = article.querySelector<HTMLElement>('[data-message-content]')
    if (content) return content
  }
  return null
}

/**
 * 在 `root` 内把 `messageId` 的原文按码点区间标注出来，并滚入视口、聚焦。
 *
 * 返回 null 表示“找不到可唯一定位的原文”或坐标不合法；此时保持原状，
 * 不猜位置也不清除已有落点，调用方负责提示“原文待核对”。
 */
export function locateQuoteText(
  root: ParentNode | null,
  messageId: string,
  startCp: number,
  endCp: number,
): LocatedQuote | null {
  if (!root || !messageId) return null
  if (!Number.isInteger(startCp) || !Number.isInteger(endCp)) return null

  const element = findContentElement(root, messageId)
  if (!element) return null

  const codepoints = Array.from(element.textContent ?? '')
  if (startCp < 0 || endCp > codepoints.length || endCp <= startCp) return null

  // 只清当前发言流内的旧落点：同页若有另一处独立发言流（如集成预览的两栏），各自保持自己的落点。
  clearLocatedIn(root)
  const quote = codepoints.slice(startCp, endCp).join('')
  element.setAttribute('data-located-quote', quote)
  element.scrollIntoView?.({ block: 'center' })
  element.focus?.({ preventScroll: true })

  return { element, quote }
}
