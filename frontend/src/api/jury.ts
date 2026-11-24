import { GENERATION_TIMEOUT_MS, isRecord, type ApiClient } from './client'
import type { PersonalModelInput } from '../types/api'

export interface Judge { id: string; name: string; role: string; public: boolean; owned: boolean; official: boolean }
export interface JuryModel { id: string; alias: string; source: 'local' | 'ai' }
export interface SeatChoice { judge_id: string; model_id: string }
interface Seat extends SeatChoice {
  id: string
  judge_name: string
  model_alias: string
  status: string
  source: string
  error_code?: string
  user?: number[]
  assistant?: number[]
  reason?: string
}
export interface Verdict {
  status: string
  seats: Seat[]
  score?: { user: number[]; assistant: number[]; user_total: number; assistant_total: number; outcome: 'win' | 'loss' | 'draw'; source?: 'local' | 'ai' }
  elo_delta?: number
}

function isJudge(v: unknown): v is Judge {
  return isRecord(v) && typeof v.id === 'string' && typeof v.name === 'string' && typeof v.role === 'string'
    && typeof v.public === 'boolean' && typeof v.owned === 'boolean' && typeof v.official === 'boolean'
}
function isModel(v: unknown): v is JuryModel {
  return isRecord(v) && typeof v.id === 'string' && typeof v.alias === 'string' && (v.source === 'local' || v.source === 'ai')
}
function isDimensions(v: unknown) {
  return Array.isArray(v) && v.length === 5 && v.every(n => typeof n === 'number' && Number.isFinite(n) && n >= 0 && n <= 100)
}
function isVerdict(v: unknown): v is Verdict {
  return isRecord(v) && typeof v.status === 'string' && Array.isArray(v.seats)
    && v.seats.every(s => isRecord(s) && typeof s.id === 'string' && typeof s.status === 'string'
      && ['judge_id', 'judge_name', 'model_id', 'model_alias', 'source'].every(key => typeof s[key] === 'string')
      && (s.user === undefined || isDimensions(s.user)) && (s.assistant === undefined || isDimensions(s.assistant)))
    && (v.elo_delta == null || typeof v.elo_delta === 'number')
    && (v.score == null || (isRecord(v.score) && isDimensions(v.score.user) && isDimensions(v.score.assistant)
      && typeof v.score.user_total === 'number' && typeof v.score.assistant_total === 'number'
      && ['win', 'loss', 'draw'].includes(String(v.score.outcome))))
}
function listOf<T>(check: (v: unknown) => v is T) {
  return (v: unknown): v is { items: T[] } => isRecord(v) && Array.isArray(v.items) && v.items.every(check)
}

export function createJuryApi(client: ApiClient, debateId: string) {
  const catalog = '/api/v1/judges'
  const path = `/api/v1/debates/${encodeURIComponent(debateId)}/verdict`
  return {
    catalog: () => client.request(catalog, { expectedStatus: 200, validate: listOf(isJudge) }),
    models: () => client.request(`${catalog}/models`, { expectedStatus: 200, validate: listOf(isModel) }),
    addModel: (body: PersonalModelInput) => client.request(`${catalog}/models`, {
      method: 'POST', body, expectedStatus: 201, validate: isModel,
    }),
    removeModel: (id: string) => client.request(`${catalog}/models/${encodeURIComponent(id)}/remove`, {
      method: 'POST', body: {}, expectedStatus: 200,
      validate: (value): value is { ok: true } => isRecord(value) && value.ok === true,
    }),
    add: (name: string, role: string) => client.request(catalog, { method: 'POST', body: { name, role }, expectedStatus: [200, 201], validate: isJudge }),
    visibility: (judge: Judge, visible: boolean) => client.request(`${catalog}/${encodeURIComponent(judge.id)}/visibility`, {
      method: 'POST', body: { public: visible }, expectedStatus: 200, validate: isJudge,
    }),
    verdict: () => client.request(path, { expectedStatus: 200, validate: isVerdict }),
    judge: (seats: SeatChoice[]) => client.request(path, {
      method: 'POST', body: { seats }, expectedStatus: [200, 201, 202], timeoutMs: GENERATION_TIMEOUT_MS, validate: isVerdict,
    }),
    retry: (id: string) => client.request(`${path}/seats/${encodeURIComponent(id)}/retry`, {
      method: 'POST', body: {}, expectedStatus: [200, 202], timeoutMs: GENERATION_TIMEOUT_MS, validate: isVerdict,
    }),
  }
}
