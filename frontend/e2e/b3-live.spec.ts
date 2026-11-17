import fs from 'node:fs'
import path from 'node:path'
import { execFileSync } from 'node:child_process'
import { fileURLToPath } from 'node:url'
import { expect, test, type Page, type Response } from '@playwright/test'
import type { AuthSession, Debate, DebateDetail, StrategyStatus, Turn } from '../src/types/api'

// B3 只补代理整合影响；既有 6 项 Vite/Fake、15 项 Mock 及解析器单测沿用原证据。
// 所有请求都到真实 Go/MySQL/Fake；本文件不使用 page.route、fetch 替换或伪造 SSE。
test.use({ trace: 'off', screenshot: 'off' })
test.describe.configure({ mode: 'serial' })
const ROOT = fileURLToPath(new URL('../../', import.meta.url))
const FIXED = '134b845e5f9352d83c711d763471d1452ba336c0'
const EXISTING_B2_BUILD = '270eed81ee9a29f53bc25c25cb9a9992e8b50252'
let backendBuildRevision = ''
// 公开的合成测试输入，不是真实账号口令；禁止替换成真实凭据或保存登录 trace。
const SYNTHETIC_PASSWORD = 'b3-synthetic-not-real-password'
const routeOf = (response: Response) => new URL(response.url()).pathname
const postTo = (url: string) => (response: Response) => routeOf(response) === url && response.request().method() === 'POST'
const speech = (round: number) => `第${round}轮：我先明确评价标准，再比较适用条件和可检验的证据。团队决策需要考虑信息完整性、机会成本与执行责任，不能用单一例子代替普遍结论。回应对方时应指出推理中的前提，并说明什么证据可以改变判断。`
function evidence(name: string) {
  const file = test.info().outputPath(name)
  fs.mkdirSync(path.dirname(file), { recursive: true })
  return file
}
function receipt(name: string, data: Record<string, unknown>) {
  fs.writeFileSync(evidence(name), JSON.stringify({ tested_sha: FIXED, backend_build_revision: backendBuildRevision, backend_source_equivalent_to: FIXED, recorded_at: new Date().toISOString(), chain: 'Edge -> Vite:5173 -> Nginx:18080 -> Go:8080 -> siyige_test -> Fake', ...data }, null, 2))
}
function identities(turns: Turn[]) {
  return turns.map(turn => ({ turn_id: turn.id, round_no: turn.round_no, client_request_id: turn.client_request_id, attempt_version: turn.attempt_version, message_ids: turn.messages.map(message => message.id) }))
}
async function detail(page: Page, id: string) {
  const response = await page.request.get(`/api/v1/debates/${id}`)
  expect(response.status()).toBe(200)
  return (await response.json() as { data: DebateDetail }).data
}
async function login(page: Page, username: string) {
  await page.getByLabel('用户名', { exact: true }).fill(username)
  await page.getByLabel('密码', { exact: true }).fill(SYNTHETIC_PASSWORD)
  await page.getByRole('button', { name: '登录', exact: true }).click()
  await expect(page.locator('[data-view-nav]')).toBeVisible()
  const response = await page.request.get('/api/v1/auth/me')
  expect(response.status()).toBe(200)
  return (await response.json() as { data: AuthSession }).data
}
async function registerAndLogin(page: Page) {
  const username = `b3_${Date.now()}_${Math.random().toString(36).slice(2, 7)}`
  await page.goto('/')
  await page.getByRole('button', { name: '还没有账号？去注册', exact: true }).click()
  await page.getByLabel('用户名', { exact: true }).fill(username)
  await page.getByLabel('密码', { exact: true }).fill(SYNTHETIC_PASSWORD)
  const registered = page.waitForResponse(postTo('/api/v1/auth/register'))
  await page.getByRole('button', { name: '注册', exact: true }).click()
  expect((await registered).status()).toBe(201)
  await expect(page.getByText('注册成功，请登录。', { exact: true })).toBeVisible()
  await expect(page.getByLabel('密码', { exact: true })).toHaveValue('')
  await expect(page.locator('[data-view-nav]')).toHaveCount(0)
  expect((await page.request.get('/api/v1/auth/me')).status()).toBe(401)
  await page.getByRole('button', { name: '已有账号？去登录', exact: true }).click()
  const session = await login(page, username)
  return { username, session }
}
async function create(page: Page) {
  const response = page.waitForResponse(postTo('/api/v1/debates'))
  await expect(page.getByRole('button', { name: '创建会话', exact: true })).toBeEnabled()
  await page.getByRole('button', { name: '创建会话', exact: true }).evaluate((node: HTMLButtonElement) => { node.click(); node.click() })
  const created = await response
  expect(created.status()).toBe(201)
  const debate = (await created.json() as { data: Debate }).data
  await expect(page).toHaveURL(new RegExp(`#debate=${debate.id}$`))
  await expect(page.locator('#first-content')).toBeEnabled()
  return debate
}
async function nginxStream(response: Response) {
  expect(response.status()).toBe(200)
  expect(response.headers()['content-type']).toContain('text/event-stream')
  expect(await response.headerValue('x-t15-sse-route')).toBe('matched')
  return { request_id: response.headers()['x-request-id'], nginx_route: 'matched' }
}

test.beforeEach(async ({ page }) => {
  test.skip(process.env.SIYIGE_BROWSER_LIVE !== '1', '需明确开启固定 test/Fake + Nginx 环境，不自动启动数据库')
  expect(execFileSync('git', ['--no-optional-locks', 'rev-parse', 'HEAD'], { cwd: ROOT, encoding: 'utf8' }).trim()).toBe(FIXED)
  const env = JSON.parse(fs.readFileSync(path.join(ROOT, '.cache/b3-live-env.json'), 'utf8')) as {
    status: string; tested_sha: string; backend_revision: string; source_equivalent_to?: string; app_env: string; ai_mode: string; db_addr: string; db_name: string; scenario: string
  }
  expect(env).toMatchObject({ status: 'ready', tested_sha: FIXED, app_env: 'test', ai_mode: 'fake', db_addr: '127.0.0.1:13307', db_name: 'siyige_test', scenario: 'slow' })
  // 允许复用已核实的 B2 Fake，但明确记录真实构建版本，不将它写成固定 SHA 新构建。
  // 任意旧版本均不接受；该 B2 启动记录已核实构建时后端无修改，且两提交 backend 完全相同。
  expect(env.backend_revision === FIXED || (env.backend_revision === EXISTING_B2_BUILD && env.source_equivalent_to === FIXED)).toBe(true)
  execFileSync('git', ['--no-optional-locks', 'diff', '--exit-code', env.backend_revision, FIXED, '--', 'backend', 'infra/sse/start-local.ps1'], { cwd: ROOT, encoding: 'utf8' })
  expect(execFileSync('git', ['--no-optional-locks', 'status', '--porcelain=v1', '--untracked-files=all', '--', 'backend', 'frontend/src'], { cwd: ROOT, encoding: 'utf8' }).trim()).toBe('')
  backendBuildRevision = env.backend_revision
  expect((await page.request.get('/api/health/ready')).status()).toBe(200)
})

test('B3 Nginx 真链路：注册、反方数据流派、策略、四轮、总结与重登历史', async ({ page }) => {
  test.setTimeout(90_000)
  const { username } = await registerAndLogin(page)
  const posts: string[] = []
  page.on('request', request => { if (request.method() === 'POST') posts.push(new URL(request.url()).pathname) })
  await page.locator('[data-view-tab="history"]').click()
  await expect(page.getByText('暂无历史会话，前往辩题列表开始一次练习吧。', { exact: true })).toBeVisible()
  await page.locator('[data-view-tab="topics"]').click()
  await page.locator('[data-topic-id="2"] input').check()
  await page.getByRole('radio', { name: '反方（反对）', exact: true }).check()
  await page.getByLabel('AI 陪练流派', { exact: true }).selectOption('data')
  const created = await create(page)
  expect(created).toMatchObject({ ai_style: 'data', user_stance: 'con', ai_stance: 'pro', mode: 'standard_4', total_rounds: 4 })
  await expect(page.locator('[aria-label="首轮辩论面板"] [data-ai-style="data"]')).toBeVisible()
  await expect(page.locator('[data-generate-strategy]')).toBeVisible()
  expect(posts.filter(url => url.endsWith('/strategy'))).toHaveLength(0)
  const strategyResponse = page.waitForResponse(postTo(`/api/v1/debates/${created.id}/strategy`))
  await page.locator('[data-generate-strategy]').evaluate((node: HTMLButtonElement) => { node.click(); node.click() })
  expect((await strategyResponse).status()).toBe(201)
  await expect(page.locator('[data-strategy-completed]')).toBeVisible()
  const strategy = (await (await page.request.get(`/api/v1/debates/${created.id}/strategy`)).json() as { data: StrategyStatus }).data
  expect(strategy.status).toBe('completed')
  if (strategy.status !== 'completed') throw new Error('策略没有真实保存成功')
  expect(strategy.ai_style).toBe('data')
  let timing: { first_length: number; next_length: number; elapsed_ms: number } | undefined
  const headers: { request_id: string | undefined; nginx_route: string }[] = []
  for (let round = 1; round <= created.total_rounds; round++) {
    await page.locator('#first-content').fill(speech(round))
    const response = page.waitForResponse(postTo(`/api/v1/debates/${created.id}/turns/stream`))
    await page.getByRole('button', { name: round === 1 ? '发送首轮' : `发送第 ${round} 轮`, exact: true }).evaluate((node: HTMLButtonElement) => { node.click(); node.click() })
    headers.push(await nginxStream(await response))
    await expect(page.locator('[data-stream-preview]')).toBeVisible()
    await expect(page.locator('[data-stream-text]')).not.toHaveText('正在等待首个回复片段…')
    await expect(page.locator('.message')).toHaveCount((round - 1) * 2)
    if (round === 1) {
      const first = await page.locator('[data-stream-text]').innerText(), at = Date.now()
      await expect.poll(async () => page.locator('[data-stream-text]').innerText()).not.toBe(first)
      const next = await page.locator('[data-stream-text]').innerText()
      timing = { first_length: [...first].length, next_length: [...next].length, elapsed_ms: Date.now() - at }
      expect(timing.next_length).toBeGreaterThan(timing.first_length)
      await page.screenshot({ path: evidence('nginx-incremental.png'), fullPage: true })
    }
    await expect(page.locator('[data-stream-preview]')).toHaveCount(0)
    await expect(page.locator('.message')).toHaveCount(round * 2)
  }
  await expect(page.locator('[data-summary-completed]')).toBeVisible()
  const saved = await detail(page, created.id)
  expect(saved.debate).toMatchObject({ status: 'completed', completed_rounds: 4, ai_style: 'data' })
  expect(saved.turns).toHaveLength(4)
  expect(saved.turns.every(turn => turn.status === 'completed' && turn.attempt_version === 1 && turn.messages.length === 2)).toBe(true)
  expect(new Set(saved.turns.map(turn => turn.client_request_id)).size).toBe(4)
  expect(new Set(saved.turns.flatMap(turn => turn.messages.map(message => message.id))).size).toBe(8)
  expect(saved.summary_status).toMatchObject({ status: 'completed', attempt_version: 1 })
  await page.reload()
  await expect(page.locator('[data-summary-completed]')).toBeVisible()
  expect(await detail(page, created.id)).toEqual(saved)
  await page.getByRole('button', { name: '退出登录', exact: true }).click()
  await login(page, username)
  await page.locator('[data-view-tab="history"]').click()
  const record = page.locator(`[data-history-id="${created.id}"]`)
  await expect(record).toContainText('4 / 4 轮')
  await expect(record.locator('[data-ai-style="data"]')).toBeVisible()
  const localTime = await page.evaluate(utc => new Date(utc).toLocaleString('zh-CN', { hour12: false }), saved.debate.created_at)
  await expect(record.locator('time').first()).toHaveText(localTime)
  await page.locator(`[data-debate-id="${created.id}"]`).click()
  await expect(page.locator('.message')).toHaveCount(8)
  await expect(page.locator('[data-summary-completed]')).toBeVisible()
  await expect(page.locator('[data-strategy-completed]')).toBeVisible()
  expect(await detail(page, created.id)).toEqual(saved)
  expect(posts.filter(url => url === '/api/v1/debates')).toHaveLength(1)
  expect(posts.filter(url => url.endsWith('/turns/stream'))).toHaveLength(4)
  expect(posts.filter(url => url.endsWith('/turns'))).toHaveLength(0)
  expect(posts.filter(url => url.endsWith('/strategy'))).toHaveLength(1)
  expect(posts.filter(url => url.endsWith('/summary'))).toHaveLength(1)
  receipt('nginx-full.json', { debate_id: created.id, ai_style: saved.debate.ai_style, stance: saved.debate.user_stance, total_rounds: saved.debate.total_rounds, timing, headers, turns: identities(saved.turns), strategy_prompt_version: strategy.prompt_version, summary_status: saved.summary_status.status, generation_posts: 6, reload_equal: true, relogin_history_equal: true, browser_timezone: 'Asia/Shanghai', local_created_at: localTime })
  await page.screenshot({ path: evidence('nginx-relogin-history.png'), fullPage: true })
  await page.getByRole('button', { name: '退出登录', exact: true }).click()
})

test('B3 Nginx 刷新中断：原键查询、持久失败、显式版本重试，无重复消息', async ({ page }) => {
  test.setTimeout(60_000)
  await registerAndLogin(page)
  const created = await create(page)
  expect(created.ai_style).toBe('logical')
  const posts: string[] = []
  page.on('request', request => { if (request.method() === 'POST') posts.push(new URL(request.url()).pathname) })
  await expect(page.locator('[data-generate-strategy]')).toBeVisible()
  // 策略可跳过；只发送一次首轮，页面刷新不能自动补发。
  await page.locator('#first-content').fill(speech(1).repeat(3))
  const response = page.waitForResponse(postTo(`/api/v1/debates/${created.id}/turns/stream`))
  await page.getByRole('button', { name: '发送首轮', exact: true }).click()
  const streaming = await response
  const header = await nginxStream(streaming)
  const original = streaming.request().postDataJSON() as { client_request_id: string }
  await expect(page.locator('[data-stream-preview]')).toBeVisible()
  await expect(page.locator('[data-stream-text]')).not.toHaveText('正在等待首个回复片段…')
  const running = (await detail(page, created.id)).turns.find(turn => turn.client_request_id === original.client_request_id)!
  expect(running.status).toBe('generating')
  await page.reload()
  await expect(page.locator('[data-retry-turn]')).toBeVisible()
  const failedDetail = await detail(page, created.id)
  const failed = failedDetail.turns[0]!
  expect(failed).toMatchObject({ id: running.id, client_request_id: original.client_request_id, status: 'failed', error_code: 'generation_cancelled', attempt_version: 1 })
  expect(failed.messages).toHaveLength(1)
  expect(failed.messages[0]!.id).toBe(running.messages[0]!.id)
  expect(posts).toHaveLength(1)
  await page.screenshot({ path: evidence('nginx-refresh-failed.png'), fullPage: true })
  const retried = page.waitForResponse(postTo(`/api/v1/debates/${created.id}/turns/${failed.id}/retry`))
  await page.locator('[data-retry-turn]').evaluate((node: HTMLButtonElement) => { node.click(); node.click() })
  const retry = await retried
  expect(retry.status()).toBe(200)
  expect(retry.request().postDataJSON()).toEqual({ expected_attempt_version: failed.attempt_version })
  await expect(page.locator('.message')).toHaveCount(2)
  const recovered = await detail(page, created.id)
  expect(recovered.turns).toHaveLength(1)
  const completed = recovered.turns[0]!
  expect(completed).toMatchObject({ id: failed.id, client_request_id: original.client_request_id, status: 'completed', attempt_version: 2 })
  expect(completed.messages[0]!.id).toBe(failed.messages[0]!.id)
  expect(new Set(completed.messages.map(message => message.id)).size).toBe(2)
  await page.reload()
  await expect(page.locator('.message')).toHaveCount(2)
  expect(await detail(page, created.id)).toEqual(recovered)
  expect(posts.filter(url => url.endsWith('/turns/stream'))).toHaveLength(1)
  expect(posts.filter(url => url.endsWith('/retry'))).toHaveLength(1)
  expect(posts.filter(url => url.endsWith('/strategy'))).toHaveLength(0)
  receipt('nginx-refresh-recovery.json', { debate_id: created.id, ai_style: created.ai_style, header, original_query: identities(failedDetail.turns), recovered: identities(recovered.turns), error_code: failed.error_code, queried_version: failed.attempt_version, sent_expected_version: retry.request().postDataJSON() as { expected_attempt_version: number }, stream_posts: 1, retry_posts: 1, strategy_posts: 0, reload_equal: true })
  await page.getByRole('button', { name: '退出登录', exact: true }).click()
})