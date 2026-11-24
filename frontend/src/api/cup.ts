import { isRecord, type ApiClient } from './client'
import type { AIStyle, DebateDifficulty } from '../types/api'

export const cupStageIds = ['prelim', 'quarter', 'semi', 'final'] as const
export type CupStageId = typeof cupStageIds[number]

export interface CupStage {
  id: CupStageId
  name: string
  difficulty: DebateDifficulty
  ai_style: AIStyle
  min_elo: number
  win_reward: number
  explanation: string
}

export interface CupStatus {
  elo: number
  rank?: number
  unlocked_stage: CupStageId
  recommended_stage: CupStageId
  stages: CupStage[]
}

function isStageId(value: unknown): value is CupStageId {
  return cupStageIds.some(id => id === value)
}

function isCupStatus(value: unknown): value is CupStatus {
  if (!isRecord(value) || !Number.isSafeInteger(value.elo) || !isStageId(value.unlocked_stage)
    || !isStageId(value.recommended_stage) || !Array.isArray(value.stages) || value.stages.length !== cupStageIds.length
    || (value.rank !== undefined && (!Number.isSafeInteger(value.rank) || Number(value.rank) < 1))) return false
  return value.stages.every((stage, index) => isRecord(stage) && stage.id === cupStageIds[index]
    && typeof stage.name === 'string' && typeof stage.explanation === 'string'
    && ['beginner', 'varsity', 'national'].some(difficulty => difficulty === stage.difficulty)
    && ['logical', 'aggressive', 'empathic', 'data'].some(style => style === stage.ai_style)
    && Number.isSafeInteger(stage.min_elo) && Number.isSafeInteger(stage.win_reward))
}

export function getCupStatus(client: ApiClient) {
  return client.request('/api/v1/cup', { expectedStatus: 200, validate: isCupStatus })
}
