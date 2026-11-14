import { expect, test } from '@playwright/test'
import { execFileSync } from 'node:child_process'
import { fileURLToPath } from 'node:url'

// 此用例要求显式启动连接固定隔离库 siyige_test 的 AI_MODE=fake 后端。
// 不读取或接受真实模型密钥；普通 CI 运行 views.spec.ts 的隔离响应测试。
test('真实 Vite → Go → MySQL：空历史、双立场、分页、四轮总结、刷新与重登', async ({ page, baseURL }, testInfo) => {
  test.skip(process.env.SIYIGE_BROWSER_LIVE !== '1', '仅在显式隔离 Fake 联调时执行')
  test.setTimeout(90_000)
  const username = `p2_${Date.now().toString(36)}`
  const password = 'phase2-browser-synthetic-password'
  const headers = { Origin: baseURL! }
  const registered = await page.request.post('/api/v1/auth/register', { headers, data: { username, password } })
  expect(registered.status()).toBe(201)
  await page.goto('/#history')
  await page.getByLabel('用户名', { exact: true }).fill(username)
  await page.getByLabel('密码', { exact: true }).fill(password)
  await page.getByRole('button', { name: '登录', exact: true }).click()
  await expect(page.getByText('暂无历史会话，前往辩题列表开始一次练习吧。')).toBeVisible()

  await page.locator('[data-view-tab="topics"]').click()
  await expect(page.locator('[data-topic-id]')).toHaveCount(5)
  await page.locator('[data-topic-id="1"] input').check()
  await page.locator('input[value="con"]').check()
  await page.getByRole('button', { name: '创建会话', exact: true }).click()
  await expect(page.getByText('已完成 0 / 4 轮')).toBeVisible()
  const debateId = new URL(page.url()).hash.replace('#debate=', '')
  await expect(page.locator('[aria-label="首轮辩论面板"]')).toContainText('你：反方 · AI：正方')
  for (let round = 1; round <= 4; round++) {
    await page.locator('#first-content').fill(`第${round}轮：我认为这一主张需要考虑适用条件，并用可检验的事例支持论点。`)
    await page.getByRole('button', { name: round === 1 ? '发送首轮' : `发送第 ${round} 轮`, exact: true }).click()
    await expect(page.locator('[aria-label="首轮辩论面板"] > p.muted').filter({ hasText: `已完成 ${round} / 4 轮` })).toBeVisible()
    await expect(page.locator('.message')).toHaveCount(round * 2)
  }
  await expect(page.locator('[data-summary-completed]')).toBeVisible()
  await page.reload()
  await expect(page.locator('.message')).toHaveCount(8)
  await expect(page.locator('[data-summary-completed]')).toBeVisible()

  // 非生成的会话创建补齐 21 条本人历史，用真实分页验证 20 + 1。
  const me = await page.request.get('/api/v1/auth/me')
  expect(me.status()).toBe(200)
  const session = await me.json()
  const writeHeaders = { ...headers, 'X-CSRF-Token': session.data.csrf_token }
  await page.locator('[data-view-tab="topics"]').click()
  await page.locator('[data-topic-id="2"] input').check()
  await page.locator('input[value="pro"]').check()
  await page.getByRole('button', { name: '创建会话', exact: true }).click()
  await expect(page.getByText('已完成 0 / 4 轮')).toBeVisible()
  await expect(page.locator('[aria-label="首轮辩论面板"]')).toContainText('你：正方 · AI：反方')
  for (let index = 0; index < 19; index++) {
    const created = await page.request.post('/api/v1/debates', { headers: writeHeaders, data: { topic_id: '1', user_stance: 'pro' } })
    expect(created.status()).toBe(201)
  }
  await page.locator('[data-view-tab="history"]').click()
  await expect(page.locator('[data-history-id]')).toHaveCount(20)
  await page.getByRole('button', { name: '下一页', exact: true }).click()
  await expect(page.locator('[data-history-id]')).toHaveCount(1)
  await expect(page.locator(`[data-history-id="${debateId}"]`)).toContainText('4 / 4 轮')
  await page.locator(`[data-debate-id="${debateId}"]`).click()
  await expect(page.locator('.message')).toHaveCount(8)
  await expect(page.locator('[data-summary-completed]')).toBeVisible()

  await page.getByRole('button', { name: '退出登录', exact: true }).click()
  await page.getByLabel('用户名', { exact: true }).fill(username)
  await page.getByLabel('密码', { exact: true }).fill(password)
  await page.getByRole('button', { name: '登录', exact: true }).click()
  await page.locator('[data-view-tab="history"]').click()
  await expect(page.locator('[data-history-id]')).toHaveCount(20)
  await page.getByRole('button', { name: '下一页', exact: true }).click()
  await page.locator(`[data-debate-id="${debateId}"]`).click()
  await expect(page.locator('.message')).toHaveCount(8)
  await expect(page.locator('[data-summary-completed]')).toBeVisible()
  if (process.env.SIYIGE_BROWSER_RESTART === '1') {
    // 只允许重启已核实身份和回环端口的固定测试容器，保留其数据卷。
    const inspected = JSON.parse(execFileSync('docker', ['--context', 'desktop-linux', 'inspect', 'siyige-t02-mysql-test-1'], { encoding: 'utf8' }))[0]
    expect(inspected.Config.Image).toBe('mysql:8.4.13')
    expect(inspected.Config.Labels['com.docker.compose.project']).toBe('siyige-t02')
    expect(inspected.Config.Labels['com.docker.compose.service']).toBe('mysql-test')
    expect(inspected.HostConfig.PortBindings['3306/tcp']).toEqual([{ HostIp: '127.0.0.1', HostPort: '13307' }])
    execFileSync('docker', ['--context', 'desktop-linux', 'compose', '--project-name', 'siyige-t02', '-f', 'infra/mysql/compose.yaml', 'restart', 'mysql-test'], {
      cwd: fileURLToPath(new URL('../../', import.meta.url)), stdio: 'pipe', timeout: 30_000,
    })
    await expect.poll(async () => (await page.request.get('/api/health/ready')).status(), { timeout: 30_000 }).toBe(200)
    await page.reload()
    await expect(page.locator('.message')).toHaveCount(8)
    await expect(page.locator('[data-summary-completed]')).toBeVisible()
  }
  await page.screenshot({ path: testInfo.outputPath('four-round-summary.png'), fullPage: true })
})
