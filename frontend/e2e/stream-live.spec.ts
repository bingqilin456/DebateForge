import fs from 'node:fs'
import path from 'node:path'
import { fileURLToPath } from 'node:url'
import { expect, test, type Page } from '@playwright/test'
import type { DebateDetail } from '../src/types/api'

// 真实 Go/MySQL/Fake 链路。仅 lost-done 用例在客户端丢弃真实 done 帧，明确区分于 Mock。
test.use({ trace: 'off', screenshot: 'off' })
test.describe.configure({ mode: 'serial' })
const root = fileURLToPath(new URL('../../', import.meta.url))
const folder = path.join(root, '.cache/b2-browser-evidence')
function fixture(kind: 'failure' | 'slow' | 'stream') {
  const config = JSON.parse(fs.readFileSync(path.join(root, '.cache/b2-live-env.json'), 'utf8')) as { status: string; base_sha: string; users: Record<string, { username: string; user_id: string }> }
  expect(config.status).toBe('ready')
  expect(config.base_sha).toBe('270eed81ee9a29f53bc25c25cb9a9992e8b50252')
  return config.users[kind]!
}
function evidence(name: string, extension = 'png') { fs.mkdirSync(folder, { recursive: true }); return path.join(folder, `${Date.now()}-${name}.${extension}`) }
async function login(page: Page, kind: 'failure' | 'slow' | 'stream') {
  const user = fixture(kind)
  expect((await page.request.get('/api/health/ready')).status()).toBe(200)
  await page.goto('/')
  await page.getByLabel('用户名', { exact: true }).fill(user.username)
  await page.getByLabel('密码', { exact: true }).fill('b2-browser-synthetic-password')
  await page.getByRole('button', { name: '登录', exact: true }).click()
  await expect(page.locator('[data-view-nav]')).toBeVisible()
  const me = (await (await page.request.get('/api/v1/auth/me')).json() as { data: { user: { id: string }; csrf_token: string } }).data
  expect(me.user.id).toBe(user.user_id)
  return me.csrf_token
}
async function create(page: Page) {
  const response = page.waitForResponse(value => new URL(value.url()).pathname === '/api/v1/debates' && value.request().method() === 'POST')
  await page.getByRole('button', { name: '创建会话', exact: true }).click()
  const result = await response
  expect(result.status()).toBe(201)
  const id = (await result.json() as { data: { id: string } }).data.id
  await expect(page).toHaveURL(new RegExp(`#debate=${id}$`))
  await expect(page.locator('#first-content')).toBeEnabled()
  return id
}
const speech = (round = 1) => `第${round}轮：先明确评价标准、适用条件与比较对象，再用可以检验的例子支持论点。回应对手时应区分事实判断与价值判断，说明证据为什么足以支持结论。`

test('B2 真实 Fake：Vite 逐块显示、四轮提交、总结与刷新，双击不双写', async ({ page }) => {
  test.skip(process.env.SIYIGE_BROWSER_LIVE !== '1', '需要固定 test/Fake 环境')
  test.setTimeout(90_000)
  await login(page, 'stream')
  const id = await create(page)
  let streams = 0, ordinary = 0
  page.on('request', request => {
    if (request.method() !== 'POST') return
    const name = new URL(request.url()).pathname
    if (name.endsWith('/turns/stream')) streams++
    if (name.endsWith('/turns')) ordinary++
  })
  let timing: { first_length: number; next_length: number; elapsed_ms: number } | null = null
  for (let round = 1; round <= 4; round++) {
    await page.locator('#first-content').fill(speech(round))
    await page.getByRole('button', { name: round === 1 ? '发送首轮' : `发送第 ${round} 轮`, exact: true }).evaluate((node: HTMLButtonElement) => { node.click(); node.click() })
    await expect(page.locator('[data-stream-preview]')).toBeVisible()
    await expect(page.locator('[data-stream-text]')).not.toHaveText('正在等待首个回复片段…')
    await expect(page.locator('.message')).toHaveCount((round - 1) * 2)
    if (round === 1) {
      const first = await page.locator('[data-stream-text]').innerText(), at = Date.now()
      await expect.poll(async () => page.locator('[data-stream-text]').innerText()).not.toBe(first)
      const next = await page.locator('[data-stream-text]').innerText()
      timing = { first_length: [...first].length, next_length: [...next].length, elapsed_ms: Date.now() - at }
      expect(timing.next_length).toBeGreaterThan(timing.first_length)
      await page.screenshot({ path: evidence('stream-incremental-preview'), fullPage: true })
    }
    await expect(page.locator('[data-stream-preview]')).toHaveCount(0)
    await expect(page.locator('.message')).toHaveCount(round * 2)
    await expect(page.locator('[aria-label="首轮辩论面板"] > p.muted').filter({ hasText: `已完成 ${round} / 4 轮` })).toBeVisible()
  }
  await expect(page.locator('[data-summary-completed]')).toBeVisible()
  const result = await page.request.get(`/api/v1/debates/${id}`)
  const saved = (await result.json() as { data: DebateDetail }).data
  expect(saved.turns).toHaveLength(4)
  expect(new Set(saved.turns.map(turn => turn.client_request_id)).size).toBe(4)
  for (const turn of saved.turns) {
    expect(turn.status).toBe('completed'); expect(turn.messages).toHaveLength(2)
    await expect(page.locator(`[data-round-no="${turn.round_no}"] .message.assistant p`)).toHaveText(turn.messages[1]!.content)
  }
  expect(streams).toBe(4); expect(ordinary).toBe(0)
  await page.reload()
  await expect(page.locator('.message')).toHaveCount(8)
  await expect(page.locator('[data-summary-completed]')).toBeVisible()
  fs.writeFileSync(evidence('stream-incremental-timing', 'json'), JSON.stringify({ base_sha: '270eed81ee9a29f53bc25c25cb9a9992e8b50252', timing, streams, ordinary, completed_rounds: 4 }, null, 2))
  await page.screenshot({ path: evidence('four-stream-rounds-saved'), fullPage: true })
  await page.getByRole('button', { name: '退出登录', exact: true }).click()
})

test('B2 真实取消：断线先 GET，持久失败后按查询版本 JSON 重试', async ({ page }) => {
  test.skip(process.env.SIYIGE_BROWSER_LIVE !== '1', '需要固定 test/Fake 环境')
  test.setTimeout(45_000)
  await login(page, 'failure')
  const id = await create(page)
  let streams = 0
  page.on('request', request => { if (request.method() === 'POST' && new URL(request.url()).pathname.endsWith('/turns/stream')) streams++ })
  await page.locator('#first-content').fill(speech().repeat(3))
  await page.getByRole('button', { name: '发送首轮', exact: true }).click()
  await expect(page.locator('[data-stream-text]')).not.toHaveText('正在等待首个回复片段…')
  await page.getByRole('button', { name: '停止等待并查询', exact: true }).click()
  const retry = page.locator('[data-retry-turn]')
  await expect(retry).toBeVisible()
  await expect(page.locator('[data-stream-preview]')).toHaveCount(0)
  const read = await page.request.get(`/api/v1/debates/${id}`)
  const saved = (await read.json() as { data: DebateDetail }).data
  expect(saved.turns).toHaveLength(1)
  const failed = saved.turns[0]!
  expect(failed.status).toBe('failed')
  expect(failed.error_code).toBe('generation_cancelled')
  await expect(retry).toContainText(`v${failed.attempt_version}`)
  await page.screenshot({ path: evidence('stream-cancelled-query-failed'), fullPage: true })
  const response = page.waitForResponse(value => value.request().method() === 'POST' && new URL(value.url()).pathname.endsWith(`/turns/${failed.id}/retry`))
  await retry.click()
  const retried = await response
  expect(retried.status()).toBe(200)
  expect(retried.request().postDataJSON()).toEqual({ expected_attempt_version: failed.attempt_version })
  await expect(page.locator('.message')).toHaveCount(2)
  const completed = (await (await page.request.get(`/api/v1/debates/${id}`)).json() as { data: DebateDetail }).data.turns[0]!
  expect(completed.client_request_id).toBe(failed.client_request_id)
  expect(completed.attempt_version).toBe(failed.attempt_version + 1)
  expect(streams).toBe(1)
  await page.getByRole('button', { name: '退出登录', exact: true }).click()
})

test('B2 真实 202 JSON：同键另一请求先认领，前端只 GET 恢复', async ({ page, baseURL }) => {
  test.skip(process.env.SIYIGE_BROWSER_LIVE !== '1', '需要固定 test/Fake 环境')
  test.setTimeout(45_000)
  const csrf = await login(page, 'slow')
  const id = await create(page), url = `/api/v1/debates/${id}/turns/stream`
  let background: Promise<number> | null = null, posts = 0
  page.on('request', request => { if (request.method() === 'POST' && new URL(request.url()).pathname === url) posts++ })
  await page.route('**/api/v1/debates/*/turns/stream', async route => {
    const body = route.request().postDataJSON() as { client_request_id: string }
    background = page.request.post(url, { headers: { Origin: baseURL!, 'X-CSRF-Token': csrf, Accept: 'text/event-stream' }, data: body }).then(response => response.status())
    await expect.poll(async () => {
      const result = (await (await page.request.get(`/api/v1/debates/${id}`)).json() as { data: DebateDetail }).data
      return result.turns.find(turn => turn.client_request_id === body.client_request_id)?.status
    }).toBe('generating')
    await route.continue()
  })
  await page.locator('#first-content').fill(speech().repeat(2))
  const replay = page.waitForResponse(value => value.request().method() === 'POST' && new URL(value.url()).pathname === url)
  await page.getByRole('button', { name: '发送首轮', exact: true }).click()
  const pending = await replay
  expect(pending.status()).toBe(202)
  expect(pending.headers()['content-type']).toContain('application/json')
  await expect(page.locator('.turn-generating')).toBeVisible()
  await expect(page.locator('[data-stream-preview]')).toHaveCount(0)
  await page.screenshot({ path: evidence('stream-real-202-json-query'), fullPage: true })
  await expect(page.locator('.message')).toHaveCount(2)
  expect(await background!).toBe(200)
  expect(posts).toBe(1)
  const saved = (await (await page.request.get(`/api/v1/debates/${id}`)).json() as { data: DebateDetail }).data
  expect(saved.turns).toHaveLength(1)
  expect(saved.turns[0]!.attempt_version).toBe(1)
  await page.getByRole('button', { name: '退出登录', exact: true }).click()
})

test('B2 真实提交后客户端丢 done：查询恢复完成，不重复发送', async ({ page }) => {
  test.skip(process.env.SIYIGE_BROWSER_LIVE !== '1', '需要固定 test/Fake 环境')
  test.setTimeout(45_000)
  // 仅作客户端丢帧注入：实际请求完整到达 Go，保留真实 start/delta，只丢弃实际 done。
  // 此用例不作为逐块时序证据；逐块证据由第一个不拦截流的用例承担。
  await page.addInitScript(() => {
    const original = window.fetch.bind(window)
    window.fetch = async (input, init) => {
      const response = await original(input, init)
      if (String(input).endsWith('/turns/stream') && response.status === 200 && response.headers.get('Content-Type')?.startsWith('text/event-stream')) {
        const wire = await response.text(), at = wire.indexOf('event: done')
        if (at < 0) throw new Error('真实响应没有 done，不能假装丢帧场景通过')
        return new Response(wire.slice(0, at), { status: response.status, headers: response.headers })
      }
      return response
    }
  })
  await login(page, 'stream')
  const id = await create(page)
  let posts = 0
  page.on('request', request => { if (request.method() === 'POST' && new URL(request.url()).pathname.endsWith('/turns/stream')) posts++ })
  await page.locator('#first-content').fill(speech())
  await page.getByRole('button', { name: '发送首轮', exact: true }).click()
  await expect(page.locator('.message')).toHaveCount(2)
  await expect(page.locator('[data-stream-preview]')).toHaveCount(0)
  expect(posts).toBe(1)
  const saved = (await (await page.request.get(`/api/v1/debates/${id}`)).json() as { data: DebateDetail }).data
  expect(saved.turns).toHaveLength(1)
  expect(saved.turns[0]!.status).toBe('completed')
  expect(saved.debate.completed_rounds).toBe(1)
  await page.screenshot({ path: evidence('stream-lost-done-query-recovered'), fullPage: true })
  await page.getByRole('button', { name: '退出登录', exact: true }).click()
})
