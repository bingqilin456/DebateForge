import { isRecord, type ApiClient } from './client'
import type { NeonOpponent, NeonOpponentDraft, NeonTopic, NeonTopicCategory, NeonTopicDraft } from '../types/api'

export const neonCategoryLabels: Record<NeonTopicCategory, string> = {
  technology: '科技伦理',
  campus: '校园生活',
  society: '社会经济',
  philosophy: '哲学思辨',
}

export function neonTopicKey(topic: NeonTopic) {
  return topic.official ? topic.id : `custom-${topic.id}`
}

function hasCatalogFields(value: unknown): value is Record<string, unknown> {
  return isRecord(value) && typeof value.id === 'string' && Number.isSafeInteger(value.heat) && Number(value.heat) >= 0
    && typeof value.public === 'boolean' && typeof value.owned === 'boolean' && typeof value.official === 'boolean'
}

function isNeonTopic(value: unknown): value is NeonTopic {
  return hasCatalogFields(value) && typeof value.title === 'string' && typeof value.background === 'string'
    && typeof value.category === 'string' && Object.hasOwn(neonCategoryLabels, value.category)
}

function isNeonOpponent(value: unknown): value is NeonOpponent {
  return hasCatalogFields(value) && typeof value.name === 'string' && typeof value.tag === 'string'
    && typeof value.persona === 'string' && ['beginner', 'varsity', 'national'].some(item => item === value.difficulty)
    && (value.official ? ['logical', 'data', 'aggressive'].some(item => item === value.style) : value.style === undefined)
}

function listOf<T>(check: (value: unknown) => value is T) {
  return (value: unknown): value is { items: T[] } => isRecord(value) && Array.isArray(value.items) && value.items.every(check)
}

export function createNeonCatalogApi(client: ApiClient) {
  const topics = '/api/v1/neon-catalog/topics'
  const opponents = '/api/v1/neon-catalog/opponents'
  function visibility(path: string, id: string, isPublic: boolean) {
    return client.request(`${path}/${encodeURIComponent(id)}/visibility`, {
      method: 'POST', body: { public: isPublic }, expectedStatus: 200,
      validate: (value): value is { public: boolean } => isRecord(value) && value.public === isPublic,
    })
  }
  return {
    topics: () => client.request(topics, { expectedStatus: 200, validate: listOf(isNeonTopic) }),
    opponents: () => client.request(opponents, { expectedStatus: 200, validate: listOf(isNeonOpponent) }),
    saveTopic: (body: NeonTopicDraft, id?: string) => client.request(id ? `${topics}/${encodeURIComponent(id)}` : topics, {
      method: 'POST', body, expectedStatus: id ? 200 : 201, validate: isNeonTopic,
    }),
    saveOpponent: (body: NeonOpponentDraft, id?: string) => client.request(id ? `${opponents}/${encodeURIComponent(id)}` : opponents, {
      method: 'POST', body, expectedStatus: id ? 200 : 201, validate: isNeonOpponent,
    }),
    topicVisibility: (id: string, visible: boolean) => visibility(topics, id, visible),
    opponentVisibility: (id: string, visible: boolean) => visibility(opponents, id, visible),
  }
}
