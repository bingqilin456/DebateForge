import { expect, test } from '@playwright/test'

test('个人模型：保存、主动校验和停用各有状态，输入密钥只提交一次', async ({ page }) => {
  let configured = false
  let verified = false
  let testingCalls = 0
  let savingCalls = 0
  const syntheticKey = 'synthetic-key-for-browser-mock'
  const current = () => configured
    ? { enabled: true, configured, verified, used: false, provider: 'custom', alias: '辩论练习', model: 'test-model', base_url: 'https://model.invalid/v1' }
    : { enabled: true, configured, verified, used: false }
  await page.route('**/api/v1/**', async route => {
    const url = new URL(route.request().url())
    function response(data: unknown) {
      return route.fulfill({ headers: { 'X-Request-ID': 'mock-neon-model' }, json: { data, request_id: 'mock-neon-model' } })
    }
    if (url.pathname.endsWith('/auth/me')) return response({ user: { id: '3', username: 'test_user' }, csrf_token: 'synthetic-csrf' })
    if (url.pathname.endsWith('/topics')) return response({ items: [] })
    if (url.pathname.endsWith('/settings/model/test')) {
      testingCalls++
      verified = true
      return response({ ok: true })
    }
    if (url.pathname.endsWith('/settings/model/remove')) {
      configured = false
      return response({ ok: true })
    }
    if (url.pathname.endsWith('/settings/model')) {
      if (route.request().method() === 'POST') {
        expect(route.request().postDataJSON()).toEqual({ provider: 'custom', alias: '辩论练习', model: 'test-model', base_url: 'https://model.invalid/v1', api_key: syntheticKey })
        savingCalls++
        configured = true
      }
      return response(current())
    }
    return route.fulfill({ status: 404, json: { error: { code: 'not_found', message: '测试路径不存在' } } })
  })
  await page.goto('/#topics')
  await page.getByRole('button', { name: '接入模型', exact: true }).click()
  const settings = page.getByRole('region', { name: '模型连接设置' })
  await expect(settings.getByText('本地演示', { exact: true })).toBeVisible()
  await settings.getByLabel('服务商').selectOption('custom')
  await settings.getByLabel('模型 ID').fill('test-model')
  await settings.getByLabel('显示别名').fill('辩论练习')
  await settings.getByLabel('API 地址').fill('https://model.invalid/v1')
  await settings.getByLabel('API Key').fill(syntheticKey)
  await settings.getByRole('button', { name: '保存配置' }).click()
  await expect(settings.getByText('连接待验证')).toBeVisible()
  await expect(settings.getByLabel('API Key')).toHaveValue('')
  expect(savingCalls).toBe(1)
  expect(testingCalls).toBe(0)
  expect(await page.evaluate(() => JSON.stringify(localStorage))).not.toContain(syntheticKey)
  await settings.getByRole('button', { name: '测试连接' }).click()
  await expect(settings.getByText('连接已验证')).toBeVisible()
  await expect(settings.getByText('尚无实际调用')).toBeVisible()
  expect(testingCalls).toBe(1)
  await settings.getByRole('button', { name: '停用个人模型' }).click()
  await settings.getByRole('button', { name: '确认停用' }).click()
  await expect(settings.getByText('本地演示', { exact: true })).toBeVisible()
})
