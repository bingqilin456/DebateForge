import { expect, test } from '@playwright/test'

test('完整训练赛通过 Vite、Go、隔离 MySQL 和 Fake 保存并恢复回看', async ({ page, baseURL }) => {
  test.skip(process.env.SIYIGE_BROWSER_LIVE !== '1', '仅在显式隔离 Fake 联调时执行')
  test.setTimeout(60_000)
  const username = `tv2_${Date.now().toString(36)}`
  const password = 'training-test-only-password'
  const registered = await page.request.post('/api/v1/auth/register', {
    headers: { Origin: baseURL! }, data: { username, password },
  })
  expect(registered.status()).toBe(201)
  await page.goto('/#topics')
  await page.getByLabel('用户名', { exact: true }).fill(username)
  await page.getByLabel('密码', { exact: true }).fill(password)
  await page.getByRole('button', { name: '登录', exact: true }).click()
  await expect(page.locator('[data-topic-id="1"] input')).toBeVisible()
  const session = await page.request.get('/api/v1/auth/me')
  expect(session.status()).toBe(200)
  const { data: { csrf_token: csrf } } = await session.json() as { data: { csrf_token: string } }
  // Neon 的选题页统一创建四轮对局。由历史契约创建夹具，再验证旧训练页仍可恢复。
  const created = await page.request.post('/api/v1/training-debates', {
    headers: { Origin: baseURL!, 'X-CSRF-Token': csrf },
    data: { topic_id: '1', user_stance: 'pro', ai_style: 'logical' },
  })
  expect(created.status()).toBe(201)
  const { data: { id } } = await created.json() as { data: { id: string } }
  await page.goto(`/#training=${id}`)
  const route = page.url()
  const contents = ['主张考虑适用条件', '驳论需要可验证依据', '自由辩应逐项确认标准', '结辩收束为三项条件']

  const write = async (index: number) => {
    await page.locator('#training-content').fill(contents[index]!)
    await page.getByRole('button', { name: '发送本步发言' }).click()
  }
  const ai = async () => {
    await page.locator('[data-generate-ai]').click()
  }
  await write(0)
  await ai()
  await write(1)
  await ai()
  await expect(page.locator('[data-issue-card]')).not.toHaveCount(0)
  await page.getByRole('button', { name: '保存并退出' }).click()
  await page.goto(route)
  await expect(page.locator('[data-step-no="4"]')).toBeVisible()
  await expect(page.locator('[data-review-panel]')).toHaveCount(0)
  await write(2)
  await ai()
  await page.getByRole('button', { name: '进入结辩', exact: true }).click()
  await ai()
  await write(3)
  await expect(page.locator('[data-step-no="8"]')).toBeVisible()
  await page.getByRole('button', { name: '生成复盘', exact: true }).click()
  await expect(page.locator('[data-review-gap-note]')).toBeVisible()
  await page.locator('[data-legend-locate="argument_structure"]').click()
  await expect(page.locator('[data-step-no="1"] [data-located-quote]')).toBeVisible()
  await page.locator('[data-repractice="argument_structure"]').click()
  await page.getByLabel('改写原句').fill('将标准写成可以逐项核对的适用条件。')
  await page.getByRole('button', { name: '提交改写' }).click()
  await expect(page.locator('[data-training-repractice-item]')).toContainText('逐项核对')
  await page.reload()
  await expect(page.locator('[data-training-repractice-item]')).toContainText('逐项核对')
  await expect(page.locator('[data-review-gap-note]')).toBeVisible()
})
