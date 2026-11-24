import { GENERATION_TIMEOUT_MS, isRecord, type ApiClient } from './client'
import type { PersonalModelInput, PersonalModelStatus } from '../types/api'

const path = '/api/v1/settings/model'

function isStatus(value: unknown): value is PersonalModelStatus {
  if (!isRecord(value) || 'api_key' in value || 'key' in value) return false
  if (typeof value.enabled !== 'boolean' || typeof value.configured !== 'boolean') return false
  if (!value.enabled && value.configured) return false
  if (value.provider !== undefined && !['deepseek', 'openai', 'doubao', 'custom'].includes(String(value.provider))) return false
  if (value.expires_at !== undefined && (typeof value.expires_at !== 'string' || !Number.isFinite(Date.parse(value.expires_at)))) return false
  return (['base_url', 'model', 'alias'] as const).every(key => value[key] === undefined || typeof value[key] === 'string')
    && (['verified', 'used'] as const).every(key => value[key] === undefined || typeof value[key] === 'boolean')
}

export function createPersonalModelApi(client: ApiClient) {
  function action(name: 'remove' | 'test') {
    return client.request(`${path}/${name}`, {
      method: 'POST', body: {}, expectedStatus: 200,
      ...(name === 'test' ? { timeoutMs: GENERATION_TIMEOUT_MS } : {}),
      validate: (value): value is { ok: true } => isRecord(value) && value.ok === true,
    })
  }
  return {
    get: () => client.request(path, { expectedStatus: 200, validate: isStatus }),
    save: (body: PersonalModelInput) => client.request(path, { method: 'POST', body, expectedStatus: 200, validate: isStatus }),
    test: () => action('test'),
    remove: () => action('remove'),
  }
}
