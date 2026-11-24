import { isRecord, type ApiClient } from './client'

export interface ProfileIdentity {
  nickname: string
  avatar_kind: 'seal' | 'image'
  avatar_seal: string
  avatar_image: string
}

export interface PublicProfile extends ProfileIdentity {
  elo: number
  rank_label: string
  win_rate: number
  cup_stage: string
}

interface OwnProfile extends PublicProfile {
  games: number
  wins: number
  elo_history: { elo: number; created_at: string }[]
  vulnerabilities: { label: string; count: number }[]
}

interface Leaderboard {
  items: (PublicProfile & { user_id: string; rank: number })[]
  page: number
  page_size: number
  total: number
}

function isIdentity(value: unknown): value is ProfileIdentity {
  return isRecord(value) && typeof value.nickname === 'string'
    && typeof value.avatar_seal === 'string' && typeof value.avatar_image === 'string'
    && (value.avatar_kind === 'seal' ? !value.avatar_image
      : value.avatar_kind === 'image' && /^data:image\/jpeg;base64,[a-zA-Z0-9+/]+=*$/.test(value.avatar_image))
}

function isProfile(value: unknown): value is PublicProfile {
  return isRecord(value) && typeof value.rank_label === 'string' && Number.isSafeInteger(value.elo)
    && typeof value.win_rate === 'number' && value.win_rate >= 0 && value.win_rate <= 100
    && typeof value.cup_stage === 'string' && isIdentity(value)
}

function isOwnProfile(value: unknown): value is OwnProfile {
  return isRecord(value) && Number.isSafeInteger(value.games) && Number.isSafeInteger(value.wins)
    && Array.isArray(value.elo_history) && Array.isArray(value.vulnerabilities)
    && value.elo_history.every(item => isRecord(item) && Number.isSafeInteger(item.elo) && typeof item.created_at === 'string')
    && value.vulnerabilities.every(item => isRecord(item) && typeof item.label === 'string' && Number.isSafeInteger(item.count))
    && isProfile(value)
}

function isLeaderboard(value: unknown): value is Leaderboard {
  return isRecord(value) && Number.isSafeInteger(value.page) && value.page_size === 10 && Number.isSafeInteger(value.total)
    && Array.isArray(value.items) && value.items.every(item => isRecord(item) && typeof item.user_id === 'string'
      && /^[1-9][0-9]*$/.test(item.user_id) && Number.isSafeInteger(item.rank) && isProfile(item))
}

export function getProfile(client: ApiClient) {
  return client.request('/api/v1/profile', { expectedStatus: 200, validate: isOwnProfile })
}

export function saveProfile(client: ApiClient, body: ProfileIdentity) {
  return client.request('/api/v1/profile', { method: 'POST', body, expectedStatus: 200, validate: isIdentity })
}

export function getLeaderboard(client: ApiClient, page: number) {
  return client.request(`/api/v1/leaderboard?page=${page}`, { expectedStatus: 200, validate: isLeaderboard })
}

export function getPublicProfile(client: ApiClient, userId: string) {
  return client.request(`/api/v1/public/profiles/${encodeURIComponent(userId)}`, {
    authenticated: false, expectedStatus: 200, validate: isProfile,
  })
}
