import { randomUUID } from 'node:crypto'
import { expect, test, type Page } from '@playwright/test'
import type { AIStyle, Debate, DebateListResult, StrategyStatus } from '../src/types/api'

// 仅对显式启动的 127.0.0.1:8080 Fake 后端及 127.0.0.1:13307/siyige_test 执行。
// 生成合成账户并保留测试数据。不读密码文件、不直写数据库、不调用真实模型。
// 真实 Session/CSRF 不应进入浏览器 trace；仅保存不含凭据的结果截图。
test.use({ trace: 'off', screenshot: 'off' })

test('T14 真实 Fake：四流派持久化、默认/非法值、防重复、跳过与 409 恢复', async ({ page, baseURL }, testInfo) => {
  test.skip(process.env.SIYIGE_BROWSER_LIVE !== '1', '需要明确启用固定隔离 Fake 联调')
  test.setTimeout(180_000)
  expect(baseURL).toBe('http://127.0.0.1:5173')
  expect((await page.request.get('/api/health/ready')).status()).toBe(200)
  const username = `t14_${Date.now().toString(36)}`
  const password = 't14-browser-synthetic-password'
  const origin = { Origin: baseURL! }
  const registered = await page.request.post('/api/v1/auth/register', { headers: origin, data: { username, password } })
  expect(registered.status()).toBe(201)
  await page.goto('/')
  const login = async () => {
    await page.getByLabel('用户名', { exact: true }).fill(username)
    await page.getByLabel('密码', { exact: true }).fill(password)
    await page.getByRole('button', { name: '登录', exact: true }).click()
    await expect(page.locator('[data-view-nav]')).toBeVisible()
  }
  await login()
  const me = await page.request.get('/api/v1/auth/me')
  expect(me.status()).toBe(200)
  const session = await me.json() as { data: { csrf_token: string } }
  const writeHeaders = { ...origin, 'X-CSRF-Token': session.data.csrf_token }
  const createBodies: unknown[] = []
  const strategyPosts = new Map<string, number>()
  page.on('request', request => {
    if (request.method() !== 'POST') return
    const path = new URL(request.url()).pathname
    if (path === '/api/v1/debates') createBodies.push(request.postDataJSON() as unknown)
    if (path.endsWith('/strategy')) strategyPosts.set(path, (strategyPosts.get(path) ?? 0) + 1)
  })
  // 只延后真实请求的发送，以便观察等待/双击；不伪造任何状态或响应。
  let releasePost: (() => void) | null = null
  await page.route('**/api/v1/debates/*/strategy', async route => {
    if (route.request().method() === 'POST') await new Promise<void>(resolve => { releasePost = resolve })
    await route.continue()
  })
  const panel = () => page.getByRole('region', { name: '首轮辩论面板', exact: true })
  const card = () => page.getByRole('region', { name: '赛前策略卡', exact: true })
  const postCount = (id: string) => strategyPosts.get(`/api/v1/debates/${id}/strategy`) ?? 0
  async function createFromUI(style: AIStyle, topics = false): Promise<Debate> {
    if (topics) {
      await page.locator('[data-view-tab="topics"]').click()
      await page.locator('[data-topic-id="1"] input').check()
      await page.locator('input[value="pro"]').check()
    }
    await page.locator('[data-topic-id="1"] input').check()
    await expect(page.getByLabel('AI 陪练流派', { exact: true })).toHaveValue('logical')
    await page.getByLabel('AI 陪练流派', { exact: true }).selectOption(style)
    const count = createBodies.length
    const response = page.waitForResponse(value => new URL(value.url()).pathname === '/api/v1/debates' && value.request().method() === 'POST')
    await page.getByRole('button', { name: '创建会话', exact: true }).evaluate((node: HTMLButtonElement) => { node.click(); node.click() })
    const created = await response
    expect(created.status()).toBe(201)
    const { data } = await created.json() as { data: Debate }
    expect(data.ai_style).toBe(style)
    expect(data.mode).toBe('standard_4')
    expect(data.total_rounds).toBe(4)
    await expect(page).toHaveURL(new RegExp(`#debate=${data.id}$`))
    await expect(panel().locator(`[data-ai-style="${style}"]`)).toBeVisible()
    await expect(card()).toContainText('尚未生成赛前策略')
    await expect(page.locator('select[name="ai_style"], select[name="mode"]')).toHaveCount(0)
    expect(createBodies).toHaveLength(count + 1)
    expect(createBodies.at(-1)).toEqual({ topic_id: '1', user_stance: 'pro', ai_style: style })
    expect(postCount(data.id)).toBe(0)
    return data
  }
  const styles: AIStyle[] = ['logical', 'aggressive', 'empathic', 'data']
  const saved = new Map<AIStyle, string>()
  const generationStart = Date.now()
  for (const style of styles) {
    const debate = await createFromUI(style, style !== 'logical')
    saved.set(style, debate.id)
    await page.reload()
    await expect(panel().locator(`[data-ai-style="${style}"]`)).toBeVisible()
    await expect(card()).toContainText('尚未生成赛前策略')
    expect(postCount(debate.id)).toBe(0)
    const response = page.waitForResponse(value => new URL(value.url()).pathname === `/api/v1/debates/${debate.id}/strategy` && value.request().method() === 'POST')
    await card().locator('[data-generate-strategy]').evaluate((node: HTMLButtonElement) => { node.click(); node.click() })
    await expect.poll(() => postCount(debate.id)).toBe(1)
    await expect(card().locator('[data-query-strategy]')).toBeDisabled()
    await expect(panel().locator('form[data-first-turn] button[type="submit"]')).toBeDisabled() // 空发言校验，不依赖策略
    const release = releasePost as (() => void) | null
    expect(release).not.toBeNull()
    release!(); releasePost = null
    const generated = await response
    expect(generated.status()).toBe(201)
    expect(generated.request().postDataJSON()).toEqual({})
    const { data } = await generated.json() as { data: StrategyStatus }
    expect(data.status).toBe('completed')
    if (data.status !== 'completed') throw new Error('Fake 未返回完成策略')
    expect(data.ai_style).toBe(style)
    expect(data.schema_version).toBe(1)
    expect(data.prompt_version).toBe(process.env.SIYIGE_B3_EXPECTED_STRATEGY_PROMPT ?? 'strategy-v1')
    await expect(card().locator('[data-strategy-completed]')).toBeVisible()
    await page.reload()
    await expect(card().locator('[data-strategy-completed]')).toContainText(`（${style}）`)
    expect(postCount(debate.id)).toBe(1)
  }
  await page.screenshot({ path: testInfo.outputPath('data-strategy-live.png'), fullPage: true })
  async function assertHistory(target: Page) {
    await target.locator('[data-view-tab="history"]').click()
    for (const [style, id] of saved) {
      await expect(target.locator(`[data-history-id="${id}"] [data-ai-style="${style}"]`)).toBeVisible()
    }
  }
  await assertHistory(page)
  await page.screenshot({ path: testInfo.outputPath('four-styles-history-live.png'), fullPage: true })
  await page.locator(`[data-debate-id="${saved.get('aggressive')!}"]`).click()
  await expect(panel().locator('[data-ai-style="aggressive"]')).toBeVisible()
  await expect(card().locator('[data-strategy-completed]')).toContainText('（aggressive）')
  await page.getByRole('button', { name: '退出登录', exact: true }).click()
  await login()
  await assertHistory(page)
  for (const [style, id] of saved) {
    await page.locator(`[data-debate-id="${id}"]`).click()
    await expect(panel().locator(`[data-ai-style="${style}"]`)).toBeVisible()
    await expect(card().locator('[data-strategy-completed]')).toContainText(`（${style}）`)
    expect(postCount(id)).toBe(1)
    await page.locator('[data-view-tab="history"]').click()
  }
  // 重新登录后 CSRF 会更新，只在内存取新值；不输出或写入验收资料。
  const current = await (await page.request.get('/api/v1/auth/me')).json() as { data: { csrf_token: string } }
  writeHeaders['X-CSRF-Token'] = current.data.csrf_token
  const list = async () => {
    const result = await page.request.get('/api/v1/debates?page=1&page_size=20')
    expect(result.status()).toBe(200)
    return (await result.json() as { data: DebateListResult }).data
  }
  const before = (await list()).total
  for (const invalid of [null, '', 'Logical', 'unknown', 7, true, []]) {
    const result = await page.request.post('/api/v1/debates', { headers: writeHeaders, data: { topic_id: '1', user_stance: 'pro', ai_style: invalid } })
    expect(result.status()).toBe(400)
    expect((await result.json() as { error: { code: string } }).error.code).toBe('invalid_request')
  }
  expect((await list()).total).toBe(before)
  const omitted = await page.request.post('/api/v1/debates', { headers: writeHeaders, data: { topic_id: '1', user_stance: 'pro' } })
  expect(omitted.status()).toBe(201)
  const defaultDebate = (await omitted.json() as { data: Debate }).data
  expect(defaultDebate.ai_style).toBe('logical')
  await page.goto(`/#debate=${defaultDebate.id}`)
  await expect(card().locator('[data-generate-strategy]')).toBeEnabled()
  await page.locator('#first-content').fill('不生成策略也能直接提出论点，并说明判断依据和适用范围。')
  await page.locator('form[data-first-turn] button[type="submit"]').click()
  await expect(panel().locator('.message')).toHaveCount(2)
  await expect(card().locator('[data-generate-strategy]')).toHaveCount(0)
  expect(postCount(defaultDebate.id)).toBe(0)

  // 真实后端共享 6 次/分钟防刷（含冲突 POST）。等待额度窗口，不绕过 Gate 或重试请求。
  const remaining = 61_000 - (Date.now() - generationStart)
  if (remaining > 0) await page.waitForTimeout(remaining)
  await page.getByRole('button', { name: '开始新的练习', exact: true }).click()
  const race = await createFromUI('data')
  const response = page.waitForResponse(value => new URL(value.url()).pathname === `/api/v1/debates/${race.id}/strategy` && value.request().method() === 'POST')
  await card().locator('[data-generate-strategy]').evaluate((node: HTMLButtonElement) => { node.click(); node.click() })
  await expect.poll(() => postCount(race.id)).toBe(1)
  // 在页面完成无 turn 预检查之后，让另一真实请求先写入首轮；只延迟请求，不伪造响应。
  const turn = await page.request.post(`/api/v1/debates/${race.id}/turns`, { headers: writeHeaders, data: { round_no: 1, content: '另一标签页已开始立论，赛前策略此时不得再创建。', client_request_id: randomUUID() } })
  expect(turn.status()).toBe(201)
  const release = releasePost as (() => void) | null
  expect(release).not.toBeNull()
  release!(); releasePost = null
  const conflict = await response
  expect(conflict.status()).toBe(409)
  expect((await conflict.json() as { error: { code: string } }).error.code).toBe('round_conflict')
  await expect(card()).toContainText('已重新查询；不会自动重试')
  await expect(card()).toContainText('已存在发言或会话已结束')
  await expect(card().locator('[data-generate-strategy]')).toHaveCount(0)
  expect(postCount(race.id)).toBe(1)
  await page.getByRole('button', { name: '查询状态', exact: true }).click()
  await expect(panel().locator('.message')).toHaveCount(2)
  const unchanged = await page.request.get(`/api/v1/debates/${race.id}/strategy`)
  expect(unchanged.status()).toBe(200)
  expect((await unchanged.json() as { data: StrategyStatus }).data).toEqual({ status: 'not_started', attempt_version: null })
  await page.screenshot({ path: testInfo.outputPath('round-conflict-live.png'), fullPage: true })
  testInfo.annotations.push({ type: 'live-evidence', description: '真实 Fake：四流派成功各一次；七种非法值无新增；默认/跳过；刷新/重登/历史；实际 409 只读恢复。未注入持久 failed。' })
})
