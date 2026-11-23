import { GENERATION_TIMEOUT_MS, isRecord, type ApiClient } from './client'
import type { RepracticeRequest, RepracticeVersion } from '../types/api'
import { isEntityId } from './debate'

function isVersion(value: unknown): value is RepracticeVersion {
  return isRecord(value) && isEntityId(value.id) && Number.isSafeInteger(value.attempt_version)
    && typeof value.attempt_version === 'number' && value.attempt_version >= 1
    && (value.status === 'generating' || value.status === 'completed' || value.status === 'failed')
    && typeof value.rewrite === 'string' && isRecord(value.feedback)
    && typeof value.feedback.change === 'string' && typeof value.feedback.reason === 'string'
    && typeof value.feedback.new_problem === 'string'
}

export function createRepracticeApi(client: ApiClient, id: string) {
  const url = `/api/v1/debates/${id}/repractice`
  return {
    list: () => client.request(url, {
      expectedStatus: 200,
      validate: (value): value is RepracticeVersion[] => Array.isArray(value) && value.every(isVersion),
    }),
    submit: (body: RepracticeRequest) => client.request(url, {
      method: 'POST', body, timeoutMs: GENERATION_TIMEOUT_MS, expectedStatus: 200, validate: isVersion,
    }),
    retry: (item: RepracticeVersion) => client.request(`${url}/${item.id}/retry`, {
      method: 'POST', body: { expected_attempt_version: item.attempt_version },
      timeoutMs: GENERATION_TIMEOUT_MS, expectedStatus: 200, validate: isVersion,
    }),
  }
}
