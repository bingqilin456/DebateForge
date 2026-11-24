import { randomUUID } from 'node:crypto'
import { expect, test, type Page } from '@playwright/test'

async function signIn(page: Page, origin: string) {
  const username = `neon_${randomUUID().slice(0, 8)}`
  const password = randomUUID()
  const registration = await page.request.post('/api/v1/auth/register', { headers: { Origin: origin }, data: { username, password } })
  expect(registration.status()).toBe(201)
  await page.goto('/#topics')
  await page.getByLabel('用户名', { exact: true }).fill(username)
  await page.getByLabel('密码', { exact: true }).fill(password)
  await page.getByRole('button', { name: '登录', exact: true }).click()
  await expect(page.locator('[data-topic-id="1"] input')).toBeVisible()
}

test('Neon 首批：四轮、双边批注、持久化重评、示例隔离和跨账号不可读', async ({ browser, page, baseURL }) => {
  test.skip(process.env.SIYIGE_BROWSER_NEON !== '1', '仅在固定隔离 MySQL + Fake 批注和复盘环境运行')
  test.setTimeout(90_000)
  await signIn(page, baseURL!)
  await page.locator('[data-topic-id="1"] input').check()
  await page.locator('input[value="con"]').check()
  await page.getByRole('button', { name: '创建会话', exact: true }).click()
  await expect(page.getByText('已完成 0 / 4 轮')).toBeVisible()
  await expect(page.getByRole('list', { name: '单场轮次' }).getByRole('listitem')).toHaveCount(4)
  const route = new URL(page.url()).hash
  const debateId = route.replace('#debate=', '')
  for (let round = 1; round <= 4; round++) {
    const input = page.locator('#first-content')
    await input.fill(`第${round}轮。我提出一项可检验的判断标准，并给出适用范围。`)
    if (round === 2) await input.press('Control+Enter')
    else await page.getByRole('button', { name: '发送交锋', exact: true }).click()
    await expect(page.locator('.message')).toHaveCount(round * 2)
    if (round === 2) {
      await page.reload()
      await expect(page.locator('.message')).toHaveCount(4)
    }
  }
  await expect(page.locator('#first-content')).toHaveCount(0)
  await expect(page.locator('.message.assistant [data-annotation-markers-entry]').first()).toBeVisible()
  await page.getByRole('button', { name: '查看本场复盘' }).click()
  await page.getByRole('button', { name: '生成复盘', exact: true }).click()
  await expect(page.locator('[data-review-panel]')).toContainText('训练')
  await page.getByRole('button', { name: '修订并重评', exact: true }).first().click()
  const rewrite = '将判断标准改成可以逐项核对的条件，同时说明何时适用。'
  await page.getByLabel('改写原句').fill(rewrite)
  await page.getByRole('button', { name: '提交改写' }).click()
  await expect(page.locator('[data-training-repractice-item]')).toContainText(rewrite)
  await page.reload()
  await expect(page.locator('[data-training-repractice-item]')).toContainText(rewrite)

  await page.setViewportSize({ width: 390, height: 844 })
  await page.getByRole('button', { name: '菜单', exact: true }).click()
  await page.locator('[data-view-tab="review"]').click()
  await expect(page.getByRole('region', { name: '示例复盘', exact: true })).toBeVisible()
  await page.getByRole('button', { name: '返回本场', exact: true }).click()
  await expect(page).toHaveURL(new RegExp(`${route}$`))
  await expect(page.locator('[data-training-repractice-item]')).toContainText(rewrite)
  expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBeLessThanOrEqual(390)

  const visitor = await browser.newContext({ baseURL })
  try {
    const otherPage = await visitor.newPage()
    await signIn(otherPage, baseURL!)
    expect((await otherPage.request.get(`/api/v1/debates/${debateId}`)).status()).toBe(404)
    expect((await otherPage.request.get(`/api/v1/debates/${debateId}/repractice`)).status()).toBe(404)
  } finally {
    await visitor.close()
  }
})

test('Neon 第二批：模型配置落库，新练习局走本地辅助并按已完成轮次更新', async ({ page, baseURL }) => {
  test.skip(process.env.SIYIGE_BROWSER_NEON_SECOND !== '1', '仅在固定隔离 MySQL 和个人模型测试开关已启用时运行')
  await signIn(page, baseURL!)
  await page.getByRole('button', { name: '接入模型', exact: true }).click()
  const settings = page.getByRole('region', { name: '模型连接设置' })
  await expect(settings.getByText('本地演示', { exact: true })).toBeVisible()
  await settings.getByLabel('服务商').selectOption('deepseek')
  await settings.getByLabel('模型 ID').fill('synthetic-model')
  await settings.getByLabel('显示别名').fill('隔离测试模型')
  const key = randomUUID().replaceAll('-', '')
  await settings.getByLabel('API Key').fill(key)
  await settings.getByRole('button', { name: '保存配置' }).click()
  await expect(settings.getByText('连接待验证')).toBeVisible()
  await expect(settings.getByLabel('API Key')).toHaveValue('')
  const modelResponse = await page.request.get('/api/v1/settings/model')
  expect(modelResponse.status()).toBe(200)
  expect(await modelResponse.text()).not.toContain(key)
  // 联调只验证保存和读取，移除合成配置后才允许发送发言，避免外部服务调用。
  await settings.getByRole('button', { name: '停用个人模型' }).click()
  await settings.getByRole('button', { name: '确认停用' }).click()
  await expect(settings.getByText('本地演示', { exact: true })).toBeVisible()
  await page.keyboard.press('Escape')
  await expect(settings).toHaveCount(0)

  await page.locator('[data-topic-id="1"] input').check()
  await page.getByLabel('交锋难度').selectOption('national')
  await page.getByLabel('使用本地演示开练习局（不计正式积分）').check()
  await page.getByRole('button', { name: '创建会话', exact: true }).click()
  await expect(page).toHaveURL(/#debate=[1-9]\d*$/)
  const id = new URL(page.url()).hash.replace('#debate=', '')
  const url = `/api/v1/debates/${id}`
  const created = await page.request.get(url)
  expect((await created.json()).data.debate).toMatchObject({ practice: true, difficulty: 'national' })
  const before = await page.request.get(`${url}/arena`)
  expect((await before.json()).data).toMatchObject({ practice: true, rounds: [] })
  await page.getByRole('button', { name: 'AI 队友代打本轮', exact: true }).click()
  await expect(page.locator('#first-content')).not.toHaveValue('')
  expect((await (await page.request.get(url)).json()).data.turns).toHaveLength(0)
  await page.getByRole('button', { name: '发送交锋', exact: true }).click()
  await expect(page.locator('.message')).toHaveCount(2)
  const after = (await (await page.request.get(`${url}/arena`)).json()).data
  expect(after.rounds).toHaveLength(1)
  expect(after.rounds[0]).toMatchObject({ round_no: 1, source: 'local' })
  await page.reload()
  await expect(page.locator('.message')).toHaveCount(2)
  expect((await (await page.request.get(`${url}/arena`)).json()).data.rounds).toEqual(after.rounds)
})

test('Neon 第三批：杯赛练手、跨账号裁判使用与撤回、五维演示裁决刷新恢复', async ({ browser, page, baseURL }) => {
  test.skip(process.env.SIYIGE_BROWSER_NEON_THIRD !== '1', '仅在固定隔离 MySQL 和 Fake／个人模型测试服务中运行')
  test.setTimeout(90_000)
  const mutationHeaders = async (actor: Page) => {
    const session = await actor.request.get('/api/v1/auth/me')
    expect(session.status()).toBe(200)
    return { Origin: baseURL!, 'X-CSRF-Token': (await session.json()).data.csrf_token as string }
  }
  await signIn(page, baseURL!)
  const ownerHeaders = await mutationHeaders(page)
  const modelIds: string[] = []
  for (const [provider, alias] of [['deepseek', '合成深度裁判'], ['openai', '合成评语裁判']]) {
    const key = randomUUID().replaceAll('-', '')
    const saved = await page.request.post('/api/v1/judges/models', { headers: ownerHeaders, data: {
      provider, alias, api_key: key, model: 'synthetic-model',
    } })
    expect(saved.status()).toBe(201)
    const savedModel = await saved.text()
    expect(savedModel).not.toContain(key)
    modelIds.push(JSON.parse(savedModel).data.id)
  }
  const choices = (await (await page.request.get('/api/v1/judges/models')).json()).data.items
  expect(choices.map((item: { alias: string }) => item.alias)).toEqual(expect.arrayContaining(['合成深度裁判', '合成评语裁判']))
  const createdJudge = await page.request.post('/api/v1/judges', {
    headers: ownerHeaders, data: { name: '隔离样本裁判', role: '优先看证据与主张之间的关系' },
  })
  expect(createdJudge.status()).toBe(201)
  const judge = (await createdJudge.json()).data
  expect(judge.public).toBe(false)
  const judgePath = `/api/v1/judges/${judge.id}`
  const visitor = await browser.newContext({ baseURL })
  try {
    const guest = await visitor.newPage()
    await signIn(guest, baseURL!)
    const guestHeaders = await mutationHeaders(guest)
    const guestModels = (await (await guest.request.get('/api/v1/judges/models')).json()).data.items
    expect(guestModels.map((item: { id: string }) => item.id)).toEqual(['demo'])
    expect((await guest.request.get(judgePath)).status()).toBe(404)
    expect((await page.request.post(`${judgePath}/visibility`, { headers: ownerHeaders, data: { public: true } })).status()).toBe(200)
    expect((await guest.request.get(judgePath)).status()).toBe(200)
    const judges = (await (await guest.request.get('/api/v1/judges')).json()).data.items
    expect(judges.filter((item: { official: boolean }) => item.official)).toHaveLength(3)
    await expect(guest.getByLabel('当前 ELO')).toHaveText('ELO 1420')
    await guest.getByRole('button', { name: '按我的 ELO 智能匹配难度' }).click()
    await expect(guest.locator('[data-cup-stage="quarter"]')).toHaveAttribute('aria-pressed', 'true')
    await guest.locator('[data-cup-stage="final"]').click()
    await guest.locator('[data-topic-id="1"] input').check()
    await guest.getByRole('button', { name: '创建会话', exact: true }).click()
    await expect(guest).toHaveURL(/#debate=[1-9]\d*$/)
    const id = new URL(guest.url()).hash.replace('#debate=', '')
    const debatePath = `/api/v1/debates/${id}`
    expect((await (await guest.request.get(debatePath)).json()).data.debate).toMatchObject({
      cup_stage: 'final', practice: true, difficulty: 'national',
    })
    for (let round = 1; round <= 4; round++) {
      await guest.locator('#first-content').fill(`第${round}轮。主张应该由可核验的证据与清楚的条件支撑。`)
      await guest.getByRole('button', { name: '发送交锋', exact: true }).click()
      await expect(guest.locator('.message')).toHaveCount(round * 2)
    }
    const response = await guest.request.post(`${debatePath}/verdict`, {
      headers: guestHeaders, data: { seats: [
        { judge_id: judge.id, model_id: 'demo' },
        { judge_id: judges.find((item: { official: boolean }) => item.official).id, model_id: 'demo' },
      ] },
    })
    expect(response.ok()).toBe(true)
    const verdict = (await response.json()).data
    expect(verdict.score.user).toHaveLength(5)
    expect(verdict.score.assistant).toHaveLength(5)
    expect(verdict.elo_delta ?? null).toBeNull()
    expect((await page.request.post(`${judgePath}/visibility`, { headers: ownerHeaders, data: { public: false } })).status()).toBe(200)
    expect((await guest.request.get(judgePath)).status()).toBe(404)
    await guest.reload()
    await guest.getByText('五维评分与裁判团', { exact: true }).click()
    await expect(guest.getByLabel('五维评分', { exact: true })).toBeVisible()
    await expect(guest.getByLabel('当前 ELO')).toHaveText('ELO 1420')
    expect((await (await guest.request.get(`${debatePath}/verdict`)).json()).data.score).toEqual(verdict.score)
  } finally {
    await visitor.close()
    for (const modelId of modelIds) {
      await page.request.post(`/api/v1/judges/models/${modelId}/remove`, { headers: ownerHeaders, data: {} })
    }
  }
})
