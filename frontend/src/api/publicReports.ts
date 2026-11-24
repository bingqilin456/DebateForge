import { createApiClient, isRecord, type ApiClient } from './client'
import { isEntityId } from './debate'

export interface PublicReportSummary {
  id: string
  title: string
  published_at: string
}

interface PublicReportList {
  items: PublicReportSummary[]
  total: number
  page: number
  page_size: number
}

interface ReportScore {
  user: number[]
  assistant: number[]
  user_total: number
  assistant_total: number
  outcome: 'win' | 'loss' | 'draw'
  source: 'ai' | 'local'
}

export interface PublicReport {
  title: string
  user_stance: 'pro' | 'con'
  ai_stance: 'pro' | 'con'
  rounds: { number: number; speeches: { side: 'user' | 'assistant'; content: string }[] }[]
  annotations: { round: number; side: 'user' | 'assistant'; label: string; quote: string; reason: string; suggestion: string }[]
  feedback: string[]
  judge_comments: string[]
  score?: ReportScore
}

interface ReviewShareStatus {
  prompt_handled: boolean
  show_prompt: boolean
  share_id: string | null
}

export type ReviewShareAction = 'prompt' | 'publish' | 'withdraw'
const publicClient = createApiClient()
const publicReportsPath = '/api/v1/public/reports'
const ownerReportPath = (id: string) => `/api/v1/debates/${isEntityId(id) ? id : 'invalid'}/sharing`
export const isReportShareId = (id: unknown): id is string =>
  typeof id === 'string' && /^[0-9a-f]{8}(?:-[0-9a-f]{4}){3}-[0-9a-f]{12}$/.test(id)
const publicReportPath = (id: string) => `${publicReportsPath}/${isReportShareId(id) ? id : 'invalid'}`

export const publicReportLink = (id: string) => `#report=${encodeURIComponent(id)}`
export const publicReportMarkdownURL = (id: string) => `${publicReportPath(id)}/markdown`
export const ownReportMarkdownURL = (id: string) => `${ownerReportPath(id)}/markdown`

function isReportList(value: unknown): value is PublicReportList {
  return isRecord(value) && Array.isArray(value.items) && Number.isSafeInteger(value.total)
    && Number.isSafeInteger(value.page) && Number.isSafeInteger(value.page_size) && value.items.every(row =>
      isRecord(row) && isReportShareId(row.id) && typeof row.title === 'string' && typeof row.published_at === 'string')
}

function isScore(value: unknown): value is ReportScore {
  const isPoints = (points: unknown): boolean => Array.isArray(points) && points.length === 5 && points.every(Number.isFinite)
  return isRecord(value) && isPoints(value.user) && isPoints(value.assistant)
    && Number.isFinite(value.user_total) && Number.isFinite(value.assistant_total)
    && (value.outcome === 'win' || value.outcome === 'loss' || value.outcome === 'draw')
    && (value.source === 'ai' || value.source === 'local')
}

function isReport(value: unknown): value is PublicReport {
  const isSide = (side: unknown) => side === 'user' || side === 'assistant'
  const isStrings = (items: unknown) => Array.isArray(items) && items.every(item => typeof item === 'string')
  return isRecord(value) && typeof value.title === 'string'
    && (value.user_stance === 'pro' || value.user_stance === 'con')
    && (value.ai_stance === 'pro' || value.ai_stance === 'con') && value.user_stance !== value.ai_stance
    && isStrings(value.feedback) && isStrings(value.judge_comments)
    && Array.isArray(value.rounds) && value.rounds.every(round => isRecord(round) && Number.isSafeInteger(round.number)
      && Array.isArray(round.speeches) && round.speeches.every(speech => isRecord(speech)
        && isSide(speech.side) && typeof speech.content === 'string'))
    && Array.isArray(value.annotations) && value.annotations.every(note => isRecord(note)
      && isSide(note.side) && Number.isSafeInteger(note.round)
      && typeof note.label === 'string' && typeof note.quote === 'string'
      && typeof note.reason === 'string' && typeof note.suggestion === 'string')
    && (value.score === undefined || isScore(value.score))
}

function isShareStatus(value: unknown): value is ReviewShareStatus {
  return isRecord(value) && typeof value.prompt_handled === 'boolean' && typeof value.show_prompt === 'boolean'
    && (value.share_id === null || isReportShareId(value.share_id))
}

export function listPublicReports(options: { ownerId?: string; topicId?: string; page?: number; pageSize?: number }) {
  const params = new URLSearchParams({ page: String(options.page ?? 1), page_size: String(options.pageSize ?? 10) })
  if (options.ownerId) params.set('owner_id', options.ownerId)
  if (options.topicId) params.set('topic_id', options.topicId)
  return publicClient.request(`${publicReportsPath}?${params}`, { authenticated: false, expectedStatus: 200, validate: isReportList })
}

export function getPublicReport(id: string) {
  return publicClient.request(publicReportPath(id), { authenticated: false, expectedStatus: 200, validate: isReport })
}

export function getReviewSharing(client: ApiClient, id: string) {
  return client.request(ownerReportPath(id), { expectedStatus: 200, validate: isShareStatus })
}

export function changeReviewSharing(client: ApiClient, id: string, action: ReviewShareAction) {
  return client.request(ownerReportPath(id), { method: 'POST', body: { action }, expectedStatus: 200, validate: isShareStatus })
}
