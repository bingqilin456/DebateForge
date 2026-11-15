import type { AIStyle } from '../types/api'

// 表单共用同一份标签；详情与历史只能按服务端保存值查标签，不反推或修改流派。
export const AI_STYLE_LABELS: Record<AIStyle, string> = {
  logical: '逻辑型',
  aggressive: '进攻型',
  empathic: '共情型',
  data: '数据型',
}
export const DEFAULT_AI_STYLE: AIStyle = 'logical'
