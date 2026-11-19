import type { ReviewContent, ReviewQuote } from '../../types/api'

export function demoReview(contents: Record<string, string>): ReviewContent {
  const cite = (message_id: string, quote: string): ReviewQuote => {
    const content = contents[message_id] ?? ''
    const offset = content.indexOf(quote)
    if (offset < 0) return { message_id, quote, start_cp: null, end_cp: null, resolved: false }
    const start_cp = Array.from(content.slice(0, offset)).length
    return { message_id, quote, start_cp, end_cp: start_cp + Array.from(quote).length, resolved: true }
  }
  const criterion = cite('101', '评价标准应当可检验')
  const sample = cite('103', '抽样的范围可以先说明')
  return {
    dimensions: [
      { key: 'argument_structure', label: '论证结构', level: 3, level_label: '较稳', basis: '先提出评价标准，再说明没有共同标准就无法比较。', improvement: '补一句双方具体要用什么标准。', insufficient_reason: null, quote: criterion },
      { key: 'responsiveness', label: '回应针对性', level: 2, level_label: '发展中', basis: '回应了抽样可以怎么做，但没直接回答成本由谁承担。', improvement: '先回答对方担心的成本，再谈如何抽样。', insufficient_reason: null, quote: sample },
      { key: 'evidence_use', label: '证据使用', level: null, level_label: null, basis: '本场主要讨论抽样方法，没有足够实例可比较。', improvement: '', insufficient_reason: '本场未出现足够具体的材料。', quote: null },
      { key: 'concept_scope', label: '概念与范围', level: 2, level_label: '发展中', basis: '提到抽样范围，但没有明确哪些对象会被排除。', improvement: '用一句话界定抽样对象和排除条件。', insufficient_reason: null, quote: sample },
    ],
    key_issues: [{ text: '双方是否能用同一套标准比较结果？', quote: criterion }],
    strengths: [{ text: '指出了先明确抽样范围的必要性。', quote: sample }],
    improvements: [{ text: '把共同标准写成双方都能照着执行的步骤。', quote: criterion }],
  }
}
