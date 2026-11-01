import { ApiError, requestJson } from './client'
import type { LiveHealth } from '../types/api'

export async function getLiveHealth(url = '/health/live', timeoutMs = 3000): Promise<LiveHealth> {
  const value = await requestJson(url, timeoutMs)
  if (typeof value !== 'object' || value === null || !('status' in value) || value.status !== 'alive') {
    throw new ApiError('format', '健康响应不符合约定，请检查后端接口。')
  }
  return { status: 'alive' }
}
