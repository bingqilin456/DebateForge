import { randomUUID } from 'node:crypto'
import { expect, test, type Page } from '@playwright/test'

async function session(page: Page, origin: string) {
  const username = `share_${randomUUID().slice(0, 8)}`
  const password = randomUUID()
  expect((await page.request.post('/api/v1/auth/register', { headers: { Origin: origin }, data: { username, password } })).status()).toBe(201)
  expect((await page.request.post('/api/v1/auth/login', { headers: { Origin: origin }, data: { username, password } })).status()).toBe(200)
  const profile = await page.request.get('/api/v1/auth/me')
  expect(profile.status()).toBe(200)
  const current = (await profile.json()).data
  return { id: current.user.id as string, headers: { Origin: origin, 'X-CSRF-Token': current.csrf_token as string }, username, password }
}

test('公开复盘：定时询问只出现一次，需本人确认，撤回使原链接和导出失效', async ({ browser, page, baseURL }) => {
  test.skip(process.env.SIYIGE_BROWSER_NEON_FOURTH !== '1', '仅在固定隔离 MySQL + Fake 中运行')
  test.setTimeout(90_000)
  const owner = await session(page, baseURL!)
  const guestContext = await browser.newContext({ baseURL })
  try {
    const guestPage = await guestContext.newPage()
    const guest = await session(guestPage, baseURL!)
    const topics = '/api/v1/public/reports?topic_id=1'
    const create = async () => {
      const res = await page.request.post('/api/v1/debates', {
        headers: owner.headers, data: { topic_id: '1', user_stance: 'pro', ai_style: 'logical' },
      })
      expect(res.status()).toBe(201)
      return (await res.json()).data.id as string
    }
    const id = await create()
    const path = `/api/v1/debates/${id}`
    const change = (action: string) => page.request.post(`${path}/sharing`, { headers: owner.headers, data: { action } })
    expect((await change('publish')).status()).toBe(409)
    const submit = async (debateId: string) => {
      for (let round = 1; round <= 4; round++) {
        const result = await page.request.post(`/api/v1/debates/${debateId}/turns`, {
          headers: owner.headers, data: { round_no: round, content: `第 ${round} 轮：需要清晰且可以检验的判断标准。`, client_request_id: randomUUID() },
        })
        expect(result.status()).toBe(201)
      }
    }
    await submit(id)
    const state = async () => (await (await page.request.get(`${path}/sharing`)).json()).data
    expect(await state()).toMatchObject({ prompt_handled: false, show_prompt: false, share_id: null })

    await page.goto(`/#debate=${id}`)
    await page.getByRole('button', { name: '查看本场复盘' }).click()
    const dialog = page.getByRole('dialog', { name: '公开这场复盘？' })
    await expect(dialog).toBeVisible({ timeout: 20_000 })
    await dialog.getByRole('button', { name: '保留私密' }).click()
    await expect(dialog).toBeHidden()
    expect(await state()).toMatchObject({ prompt_handled: true, show_prompt: false, share_id: null })
    expect((await (await change('prompt')).json()).data.show_prompt).toBe(false)

    const location = `/api/v1/public/reports?owner_id=${owner.id}`
    expect((await (await guestPage.request.get(location)).json()).data.items).toHaveLength(0)
    expect((await guestPage.request.get(`${path}/sharing/markdown`)).status()).toBe(404)
    expect((await guestPage.request.post(`${path}/sharing`, { headers: guest.headers, data: { action: 'publish' } })).status()).toBe(404)

    await page.reload()
    await page.getByRole('button', { name: '查看本场复盘' }).click()
    await page.getByRole('button', { name: '公开复盘', exact: true }).click()
    await dialog.getByRole('button', { name: '确认公开' }).click()
    await expect(page.getByText('复盘已公开。')).toBeVisible()
    const first = (await state()).share_id as string
    expect((await (await change('publish')).json()).data.share_id).toBe(first)

    const ownItems = (await (await guestPage.request.get(location)).json()).data.items
    expect(ownItems.map((item: { id: string }) => item.id)).toEqual([first])
    const topicItems = (await (await guestPage.request.get(topics)).json()).data.items
    expect(topicItems.map((item: { id: string }) => item.id)).toContain(first)

    await guestPage.goto(`/#report=${first}`)
    await expect(guestPage.getByText('公开复盘 · 只读')).toBeVisible()
    await expect(guestPage.getByRole('heading', { name: '第 4 轮', exact: true })).toBeVisible()
    await expect(guestPage.getByRole('button', { name: '修订并重评' })).toHaveCount(0)
    const markdown = await guestPage.request.get(`/api/v1/public/reports/${first}/markdown`)
    expect(markdown.headers()['cache-control']).toBe('no-store')
    expect(markdown.headers()['content-type']).toBe('text/markdown; charset=utf-8')
    const text = await markdown.text()
    expect(text).toContain('第 4 轮')
    expect(text).toContain('需要清晰且可以检验的判断标准。')

    await page.getByRole('button', { name: '撤回公开' }).click()
    await expect(page.getByText('已撤回公开，原链接已失效。')).toBeVisible()
    expect((await guestPage.request.get(`/api/v1/public/reports/${first}/markdown`)).status()).toBe(404)
    expect((await (await guestPage.request.get(location)).json()).data.items).toHaveLength(0)
    await guestPage.reload()
    await expect(guestPage.getByRole('alert')).toHaveText('这场复盘未公开或已撤回。')
    await guestPage.goto('/#history')
    const history = guestPage.getByRole('region', { name: '历史会话视图' })
    await expect(history.getByText('暂无历史会话', { exact: false })).toBeVisible()
  } finally {
    await guestContext.close()
  }
})

test('个人档案：头像保存和重登、道场只奖励一次、公开主页仅展示公开资料', async ({ page, browser, baseURL }) => {
  test.skip(process.env.SIYIGE_BROWSER_NEON_FOURTH !== '1', '仅在固定隔离 MySQL + Fake 中运行')
  const owner = await session(page, baseURL!)
  await page.goto('/#dojo')
  const dojo = page.getByRole('region', { name: '思辨道场' })
  await expect(dojo.getByRole('heading', { name: '30 秒逻辑排雷' })).toBeVisible()
  await dojo.getByRole('button', { name: /^A\./ }).click()
  await expect(dojo.locator('.answer')).toContainText('+5 ELO')
  await expect(page.getByLabel('当前 ELO')).toHaveText('ELO 1425')
  await dojo.getByRole('button', { name: /^A\./ }).click()
  await expect(dojo.locator('.answer')).toContainText('本题奖励已领取')
  await expect(page.getByLabel('当前 ELO')).toHaveText('ELO 1425')
  await dojo.getByRole('button', { name: 'AI 按历史对局出题' }).click()
  await expect(dojo.getByRole('status').filter({ hasText: /历史|内置/ }).first()).toBeVisible()
  await page.locator('[data-view-tab="history"]').click()
  const card = page.getByRole('region', { name: '个人档案' })
  await expect(card.getByText('1420 → 1425')).toBeVisible()
  await card.getByRole('button', { name: '编辑资料' }).click()
  await card.getByLabel('昵称').fill('青山辩手')
  await card.getByLabel('印章文字').fill('山')
  await card.getByRole('button', { name: '保存资料' }).click()
  await expect(card.getByRole('heading', { name: '青山辩手' })).toBeVisible()
  await expect(page.locator('.account-identity')).toContainText('青山辩手')
  await page.reload()
  await expect(card.getByRole('heading', { name: '青山辩手' })).toBeVisible()
  await card.getByRole('button', { name: '编辑资料' }).click()
  await card.getByRole('radio', { name: '图片' }).check()
  const png = await page.evaluate(() => {
    const canvas = document.createElement('canvas')
    canvas.width = canvas.height = 10
    const ctx = canvas.getContext('2d')!
    ctx.fillStyle = '#a32a32'
    ctx.fillRect(0, 0, 10, 10)
    return canvas.toDataURL('image/png').split(',')[1]!
  })
  await card.getByLabel('头像图片').setInputFiles({ name: 'synthetic.png', mimeType: 'image/png', buffer: Buffer.from(png, 'base64') })
  await expect(card.locator('form img')).toBeVisible()
  await card.getByRole('button', { name: '保存资料' }).click()
  await expect(card.getByText('资料已保存。')).toBeVisible()

  await page.getByRole('button', { name: '退出登录' }).click()
  await page.getByLabel('用户名', { exact: true }).fill(owner.username)
  await page.getByLabel('密码', { exact: true }).fill(owner.password)
  await page.getByRole('button', { name: '登录', exact: true }).click()
  await expect(card.locator('.profile-heading img')).toBeVisible()
  await expect(card.getByRole('heading', { name: '青山辩手' })).toBeVisible()
  const guest = await browser.newContext({ baseURL })
  try {
    const visit = await guest.newPage()
    await visit.goto(`/#profile=${owner.id}`)
    await expect(visit.getByRole('heading', { name: '青山辩手' })).toBeVisible()
    await expect(visit.getByRole('heading', { name: '已公开复盘' })).toBeVisible()
    await expect(visit.getByText('ELO 成长')).toHaveCount(0)
    await expect(visit.getByText('本人历史会话')).toHaveCount(0)
  } finally { await guest.close() }
})
