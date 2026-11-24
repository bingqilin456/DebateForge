import { GENERATION_TIMEOUT_MS, isRecord, type ApiClient } from './client'

const path = '/api/v1/dojo'

export interface DojoQuestion {
  id: string
  quote: string
  options: string[]
  source: 'builtin' | 'history' | 'ai'
  prompt?: string
}

export interface DojoFeedback {
  question_id: string
  choice: number
  correct: number
  explanation: string
  rewarded: boolean
  elo_delta: 0 | 5
}

interface DojoState {
  questions: DojoQuestion[]
  feedback: DojoFeedback[]
}

interface DojoHistoryResult {
  question?: DojoQuestion
  feedback?: DojoFeedback
  notice?: string
}

interface DojoTechnique {
  title?: string
  guide?: string
  quote?: string
  reason?: string
  source?: 'history' | 'ai'
  notice?: string
}

function isChoice(value: unknown): value is number {
  return typeof value === 'number' && Number.isInteger(value) && value >= 0 && value < 4
}

function isQuestion(value: unknown): value is DojoQuestion {
  return isRecord(value) && typeof value.id === 'string' && typeof value.quote === 'string'
    && Array.isArray(value.options) && value.options.length === 4 && value.options.every(option => typeof option === 'string')
    && (value.source === 'builtin' || value.source === 'history' || value.source === 'ai')
    && (value.prompt === undefined || typeof value.prompt === 'string')
}

function isFeedback(value: unknown): value is DojoFeedback {
  return isRecord(value) && typeof value.question_id === 'string' && isChoice(value.choice) && isChoice(value.correct)
    && typeof value.explanation === 'string' && typeof value.rewarded === 'boolean'
    && (value.elo_delta === 0 || value.elo_delta === 5)
}

function isState(value: unknown): value is DojoState {
  return isRecord(value) && Array.isArray(value.questions) && value.questions.every(isQuestion)
    && Array.isArray(value.feedback) && value.feedback.every(isFeedback)
}

function isHistory(value: unknown): value is DojoHistoryResult {
  return isRecord(value) && (value.question === undefined || isQuestion(value.question))
    && (value.feedback === undefined || (isFeedback(value.feedback) && isQuestion(value.question) && value.feedback.question_id === value.question.id))
    && (value.notice === undefined || typeof value.notice === 'string')
}

function isTechnique(value: unknown): value is DojoTechnique {
  return isRecord(value) && (value.notice === undefined || typeof value.notice === 'string')
    && (typeof value.notice === 'string' || (typeof value.title === 'string' && typeof value.guide === 'string'
      && typeof value.quote === 'string' && typeof value.reason === 'string'
      && (value.source === 'history' || value.source === 'ai')))
}

export function createDojoApi(client: ApiClient) {
  return {
    state: () => client.request(path, { expectedStatus: 200, validate: isState }),
    answer: (questionId: string, choice: number) => client.request(path + '/answer', {
      method: 'POST', body: { question_id: questionId, choice }, expectedStatus: 200, validate: isFeedback,
    }),
    history: () => client.request(path + '/history-quiz', {
      method: 'POST', body: {}, expectedStatus: 200, timeoutMs: GENERATION_TIMEOUT_MS, validate: isHistory,
    }),
    techniques: () => client.request(path + '/techniques', {
      method: 'POST', body: {}, expectedStatus: 200, timeoutMs: GENERATION_TIMEOUT_MS, validate: isTechnique,
    }),
  }
}
