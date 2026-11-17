import fs from 'node:fs'
import path from 'node:path'
import { fileURLToPath } from 'node:url'
import { expect, test, type Page } from '@playwright/test'

// 真实固定测试库 + A 交付的策略 Fake 选择器。无 route.fulfill，关闭网络 trace，保留合成数据。
test.use({ trace: 'off', screenshot: 'off' })
test.describe.configure({ mode: 'serial' })
const root = fileURLToPath(new URL('../../', import.meta.url))
function fixture(kind: 'failure' | 'slow') {
  const config = JSON.parse(fs.readFileSync(path.join(root, '.cache/b2-live-env.json'), 'utf8')) as {
    status: string; base_sha: string; users: Record<string, { username: string; user_id: string }>
  }
  expect(config.status).toBe('ready')
  expect(config.base_sha).toBe('270eed81ee9a29f53bc25c25cb9a9992e8b50252')
  const user = config.users[kind]!
  expect(user.user_id).toMatch(/^[1-9]\d*$/)
  expect(config.users.failure!.user_id).not.toBe(config.users.slow!.user_id)
  return user
}
async function login(page: Page, username: string) {
  await page.goto('/')
  await page.getByLabel('用户名', { exact: true }).fill(username)
  await page.getByLabel('密码', { exact: true }).fill('b2-browser-synthetic-password')
  await page.getByRole('button', { name: '登录', exact: true }).click()
  await expect(page.locator('[data-view-nav]')).toBeVisible()
}
async function create(page: Page) {
  await page.locator('[data-view-tab="topics"]').click()
  await page.locator('[data-topic-id="1"] input').check()
  const response = page.waitForResponse(value => new URL(value.url()).pathname === '/api/v1/debates' && value.request().method() === 'POST')
  await page.getByRole('button', { name: '创建会话', exact: true }).click()
  const created = await response
  expect(created.status()).toBe(201)
  const data = (await created.json() as { data: { id: string } }).data
  await expect(page).toHaveURL(new RegExp(`#debate=${data.id}$`))
  return data.id
}
function evidence(name: string) {
  const folder = path.join(root, '.cache/b2-browser-evidence')
  fs.mkdirSync(folder, { recursive: true })
  return path.join(folder, `${Date.now()}-${name}.png`)
}

test('B1 真实失败：持久 failed、刷新重登、查询版本后显式 retry 成功', async ({ page }) => {
  test.skip(process.env.SIYIGE_BROWSER_LIVE !== '1', '需要固定 test/Fake 故障装配')
  test.setTimeout(45_000)
  const user = fixture('failure')
  await login(page, user.username)
  const id = await create(page), url = `/api/v1/debates/${id}/strategy`
  let posts = 0
  page.on('request', request => { if (new URL(request.url()).pathname === url && request.method() === 'POST') posts++ })
  const card = page.getByRole('region', { name: '赛前策略卡', exact: true })
  await expect(card.locator('[data-generate-strategy]')).toBeEnabled()
  expect(posts).toBe(0)
  const initial = page.waitForResponse(value => new URL(value.url()).pathname === url && value.request().method() === 'POST')
  await card.locator('[data-generate-strategy]').click()
  const failed = await initial
  expect(failed.status()).toBe(503)
  expect((await failed.json() as { error: { code: string } }).error.code).toBe('model_unavailable')
  await expect(card).toContainText('当前版本 v1')
  expect(posts).toBe(1)
  await page.reload()
  await expect(card).toContainText('当前版本 v1')
  await page.getByRole('button', { name: '退出登录', exact: true }).click()
  await login(page, user.username)
  await page.goto(`/#debate=${id}`)
  await expect(card).toContainText('当前版本 v1')
  const latest = page.waitForResponse(value => new URL(value.url()).pathname === url && value.request().method() === 'GET')
  await card.locator('[data-prepare-strategy-retry]').click()
  const queried = await latest
  expect((await queried.json() as { data: { status: string; attempt_version: number } }).data).toMatchObject({ status: 'failed', attempt_version: 1 })
  await expect(card.locator('[data-retry-strategy]')).toHaveText('确认重试策略（v1）')
  expect(posts).toBe(1)
  const retry = page.waitForResponse(value => new URL(value.url()).pathname === url && value.request().method() === 'POST')
  await card.locator('[data-retry-strategy]').evaluate((node: HTMLButtonElement) => { node.click(); node.click() })
  const retried = await retry
  expect(retried.status()).toBe(200)
  expect(retried.request().postDataJSON()).toEqual({ retry: true, expected_attempt_version: 1 })
  expect((await retried.json() as { data: { status: string; attempt_version: number } }).data).toMatchObject({ status: 'completed', attempt_version: 2 })
  await expect(card.locator('[data-strategy-completed]')).toContainText('策略 v2')
  expect(posts).toBe(2)
  await page.screenshot({ path: evidence('strategy-failed-explicit-retry'), fullPage: true })
  await page.getByRole('button', { name: '退出登录', exact: true }).click()
})

test('B1 真实延迟：另一页查询 generating、重复 POST 202、最终 completed 版本不变', async ({ page, context, baseURL }) => {
  test.skip(process.env.SIYIGE_BROWSER_LIVE !== '1', '需要固定 test/Fake 延迟装配')
  test.setTimeout(45_000)
  const user = fixture('slow')
  await login(page, user.username)
  const id = await create(page), url = `/api/v1/debates/${id}/strategy`
  const card = page.getByRole('region', { name: '赛前策略卡', exact: true })
  await expect(card.locator('[data-generate-strategy]')).toBeEnabled()
  let originalPosts = 0, observerPosts = 0
  page.on('request', request => { if (new URL(request.url()).pathname === url && request.method() === 'POST') originalPosts++ })
  const original = page.waitForResponse(value => new URL(value.url()).pathname === url && value.request().method() === 'POST')
  await card.locator('[data-generate-strategy]').evaluate((node: HTMLButtonElement) => { node.click(); node.click() })
  await expect(card.locator('[data-query-strategy]')).toBeDisabled()
  await expect(card).toContainText('正在生成赛前策略')
  await expect.poll(async () => {
    const response = await page.request.get(url)
    return (await response.json() as { data: { status: string } }).data.status
  }, { timeout: 3_000, intervals: [100, 200] }).toBe('generating')
  const observer = await context.newPage()
  observer.on('request', request => { if (new URL(request.url()).pathname === url && request.method() === 'POST') observerPosts++ })
  await observer.goto(`/#debate=${id}`)
  const observedCard = observer.getByRole('region', { name: '赛前策略卡', exact: true })
  await expect(observedCard).toContainText('策略生成中（v1）')
  const generating = await page.request.get(url)
  expect((await generating.json() as { data: { status: string; attempt_version: number } }).data).toEqual({ status: 'generating', attempt_version: 1 })
  const me = await page.request.get('/api/v1/auth/me')
  const csrf = (await me.json() as { data: { csrf_token: string } }).data.csrf_token
  const replay = await page.request.post(url, { headers: { Origin: baseURL!, 'X-CSRF-Token': csrf }, data: {} })
  expect(replay.status()).toBe(202)
  expect((await replay.json() as { data: { status: string; attempt_version: number } }).data).toEqual({ status: 'generating', attempt_version: 1 })
  await observer.screenshot({ path: evidence('strategy-generating-real-202'), fullPage: true })
  const finished = await original
  expect(finished.status()).toBe(201)
  await expect(card.locator('[data-strategy-completed]')).toContainText('策略 v1')
  await expect(observedCard.locator('[data-strategy-completed]')).toContainText('策略 v1')
  const stored = await page.request.get(url)
  expect((await stored.json() as { data: { status: string; attempt_version: number } }).data).toMatchObject({ status: 'completed', attempt_version: 1 })
  expect(originalPosts).toBe(1)
  expect(observerPosts).toBe(0)
  await observer.screenshot({ path: evidence('strategy-delay-completed'), fullPage: true })
  await observer.close()
  await page.getByRole('button', { name: '退出登录', exact: true }).click()
})
