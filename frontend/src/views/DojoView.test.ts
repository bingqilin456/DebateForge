// @vitest-environment jsdom
import { createApp, h } from 'vue'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { createApiClient } from '../api/client'
import DojoView from './DojoView.vue'

const question = { id: 'straw-man', quote: '将对方立场推到极端。', options: ['稻草人', '诉诸权威', '循环论证', '滑坡'], source: 'builtin' }
function reply(data: unknown) {
  return new Response(JSON.stringify({ data, request_id: 'dojo-request' }), {
    status: 200, headers: { 'Content-Type': 'application/json', 'X-Request-ID': 'dojo-request' },
  })
}

afterEach(() => {
  vi.useRealTimers()
  vi.unstubAllGlobals()
})

describe('思辨道场', () => {
  it('参考计时结束后仍能作答，重复答对仍提示奖励已领取', async () => {
    vi.useFakeTimers()
    const fetchMock = vi.fn<typeof fetch>()
      .mockResolvedValueOnce(reply({ questions: [question], feedback: [] }))
      .mockResolvedValueOnce(reply({ question_id: question.id, choice: 0, correct: 0, explanation: '歪曲了立场。', rewarded: true, elo_delta: 5 }))
      .mockResolvedValueOnce(reply({ question_id: question.id, choice: 0, correct: 0, explanation: '歪曲了立场。', rewarded: true, elo_delta: 0 }))
    vi.stubGlobal('fetch', fetchMock)
    const award = vi.fn()
    const host = document.createElement('div')
    const app = createApp(() => h(DojoView, { client: createApiClient({ getCsrfToken: () => 'test-token' }), onRatingChanged: award }))
    app.mount(host)
    try {
      await vi.advanceTimersByTimeAsync(31_000)
      expect(host.textContent).toContain('仍可作答')
      const option = [...host.querySelectorAll('button')].find(button => button.textContent?.includes('A. 稻草人'))
      expect(option?.disabled).toBe(false)
      option?.click()
      await vi.advanceTimersByTimeAsync(0)
      expect(host.querySelector('.answer')?.textContent).toContain('+5 ELO')
      expect(award).toHaveBeenCalledTimes(1)
      option?.click()
      await vi.advanceTimersByTimeAsync(0)
      expect(host.textContent).toContain('本题奖励已领取')
      expect(award).toHaveBeenCalledTimes(1)
    } finally {
      app.unmount()
    }
  })
})
