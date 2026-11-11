// @vitest-environment jsdom
import { createApp, h, nextTick, type App, type Component } from 'vue'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import LoginForm from './LoginForm.vue'
import RegisterForm from './RegisterForm.vue'
import { createAuthState } from '../../auth/state'

const fetchMock = vi.fn<typeof fetch>()
const user = { id: '42', username: 'alice' }
const mounted: Array<{ app: App, host: HTMLDivElement }> = []
function success(data: unknown, status = 200) {
  return new Response(JSON.stringify({ data, request_id: 'req-form' }), {
    status, headers: { 'Content-Type': 'application/json', 'X-Request-ID': 'req-form' },
  })
}
function failure(status: number, code: string, retryAfter?: string) {
  return new Response(JSON.stringify({ error: { code, message: '<img src=x onerror=private-detail>' }, request_id: 'req-form' }), {
    status, headers: { 'Content-Type': 'application/json', 'X-Request-ID': 'req-form', ...(retryAfter ? { 'Retry-After': retryAfter } : {}) },
  })
}
function mount(component: Component) {
  const auth = createAuthState()
  const authenticated = vi.fn()
  const registered = vi.fn()
  const switchMode = vi.fn()
  const host = document.createElement('div')
  document.body.append(host)
  const app = createApp(h(component, {
    auth, onAuthenticated: authenticated, onRegistered: registered, 'onShow-register': switchMode, 'onShow-login': switchMode,
  }))
  app.mount(host)
  mounted.push({ app, host })
  return { host, app, auth, authenticated, registered, switchMode }
}
function input(host: HTMLElement, name: string) {
  const element = host.querySelector<HTMLInputElement>(`input[name="${name}"]`)
  if (!element) throw new Error(`缺少 ${name} 输入框`)
  return element
}
async function fill(host: HTMLElement, username = 'alice', password = 'example-password') {
  for (const [name, value] of Object.entries({ username, password })) {
    const element = input(host, name)
    element.value = value
    element.dispatchEvent(new Event('input', { bubbles: true }))
  }
  await nextTick()
}
function submit(host: HTMLElement) {
  const form = host.querySelector('form')
  if (!form) throw new Error('缺少原生表单')
  form.dispatchEvent(new Event('submit', { bubbles: true, cancelable: true }))
}
beforeEach(() => { vi.stubGlobal('fetch', fetchMock); fetchMock.mockReset() })
afterEach(() => {
  for (const { app, host } of mounted.splice(0)) { app.unmount(); host.remove() }
  vi.unstubAllGlobals()
})

describe('独立登录/注册表单（实际 Vue + Arco DOM）', () => {
  it('登录发送归一化用户名和原样密码；只向 A 发出 User，不泄露凭据', async () => {
    fetchMock.mockResolvedValue(success({ user, csrf_token: 'memory-csrf' }))
    const { host, auth, authenticated } = mount(LoginForm)
    expect(input(host, 'username').id).toBeTruthy()
    expect(host.querySelector(`label[for="${input(host, 'username').id}"]`)).not.toBeNull()
    expect(input(host, 'password').type).toBe('password')
    expect(input(host, 'password').autocomplete).toBe('current-password')
    await fill(host, ' Alice ', '  example-password  ')
    submit(host)
    await vi.waitFor(() => expect(authenticated).toHaveBeenCalledWith(user))
    expect(auth.state.status).toBe('authenticated')
    expect(JSON.parse(String(fetchMock.mock.calls[0]![1]?.body))).toEqual({ username: 'alice', password: '  example-password  ' })
    expect(input(host, 'password').value).toBe('')
  })

  it('注册成功提示随后登录，不自动登录或发送第二个请求', async () => {
    fetchMock.mockResolvedValue(success(user, 201))
    const { host, registered, authenticated, auth } = mount(RegisterForm)
    expect(input(host, 'password').autocomplete).toBe('new-password')
    await fill(host)
    submit(host)
    await vi.waitFor(() => expect(registered).toHaveBeenCalledWith(user))
    expect(host.textContent).toContain('注册成功，请登录')
    expect(auth.state.status).toBe('unknown')
    expect(authenticated).not.toHaveBeenCalled()
    expect(fetchMock).toHaveBeenCalledOnce()
    expect(input(host, 'password').value).toBe('')
  })

  it.each([
    ['ab', 'example-password'], ['用户abc', 'example-password'],
    ['alice', 'short'], ['alice', '😀'.repeat(129)],
  ])('本地阻止非法输入 %#，无需请求后端', async (username, password) => {
    const { host } = mount(RegisterForm)
    await fill(host, username, password)
    submit(host)
    await nextTick()
    expect(host.querySelector('[role="alert"]')?.textContent).toBeTruthy()
    expect(fetchMock).not.toHaveBeenCalled()
  })

  it('允许 128 个非 BMP 码点，不被 HTML maxlength 按 UTF-16 截断', async () => {
    fetchMock.mockResolvedValue(success(user, 201))
    const { host, registered } = mount(RegisterForm)
    const password = '😀'.repeat(128)
    await fill(host, 'alice', password)
    submit(host)
    await vi.waitFor(() => expect(registered).toHaveBeenCalledOnce())
    expect(JSON.parse(String(fetchMock.mock.calls[0]![1]?.body)).password).toBe(password)
    expect(input(host, 'password').hasAttribute('maxlength')).toBe(false)
  })

  it('同步阻止重复提交；等待期间禁用字段与按钮，结束后恢复', async () => {
    let finish!: (response: Response) => void
    fetchMock.mockReturnValue(new Promise<Response>((resolve) => { finish = resolve }))
    const { host, authenticated } = mount(LoginForm)
    await fill(host)
    submit(host)
    submit(host)
    await nextTick()
    expect(fetchMock).toHaveBeenCalledOnce()
    expect(input(host, 'username').disabled).toBe(true)
    expect(host.querySelector<HTMLButtonElement>('button[type="submit"]')?.disabled).toBe(true)
    expect(host.querySelector('form')?.getAttribute('aria-busy')).toBe('true')
    finish(success({ user, csrf_token: 'csrf' }))
    await vi.waitFor(() => expect(authenticated).toHaveBeenCalledOnce())
    expect(input(host, 'username').disabled).toBe(false)
  })

  it('凭据错误保留用户名、清空密码，只有手动重填并提交才重试', async () => {
    fetchMock.mockResolvedValueOnce(failure(401, 'invalid_credentials')).mockResolvedValueOnce(success({ user, csrf_token: 'csrf' }))
    const { host, auth, authenticated } = mount(LoginForm)
    await fill(host)
    submit(host)
    await vi.waitFor(() => expect(host.textContent).toContain('用户名或密码错误'))
    expect(auth.state.status).toBe('unknown')
    expect(input(host, 'username').value).toBe('alice')
    expect(input(host, 'password').value).toBe('')
    expect(fetchMock).toHaveBeenCalledOnce()
    expect(authenticated).not.toHaveBeenCalled()
    await fill(host)
    submit(host)
    await vi.waitFor(() => expect(authenticated).toHaveBeenCalledOnce())
  })

  it.each([
    [RegisterForm, () => failure(409, 'username_conflict'), '用户名已被使用'],
    [LoginForm, () => failure(403, 'csrf_failed'), '安全校验失败'],
    [LoginForm, () => failure(429, 'rate_limited', '15'), '15 秒'],
    [LoginForm, () => failure(503, 'dependency_unavailable'), '服务暂时不可用'],
    [LoginForm, () => new Response('<html>private-proxy-detail</html>', { status: 503, headers: { 'Content-Type': 'text/html' } }), '非 JSON'],
  ] as const)('受控展示错误与限流，无原始 HTML、无自动重试 %#', async (component, response, message) => {
    fetchMock.mockResolvedValue(response())
    const { host } = mount(component)
    await fill(host)
    submit(host)
    await vi.waitFor(() => expect(host.querySelector('[role="alert"]')?.textContent).toContain(message))
    expect(host.textContent).not.toContain('private')
    expect(host.querySelector('img')).toBeNull()
    expect(fetchMock).toHaveBeenCalledOnce()
  })

  it('切换表单只发事件，由 A 决定页面切换，不自行操作路由', async () => {
    const { host, switchMode } = mount(LoginForm)
    host.querySelector<HTMLButtonElement>('button[type="button"]')?.click()
    await nextTick()
    expect(switchMode).toHaveBeenCalledOnce()
    expect(fetchMock).not.toHaveBeenCalled()
  })

  it('卸载后异步请求不再发组件事件，内存认证结果仍交给共享状态', async () => {
    let finish!: (response: Response) => void
    fetchMock.mockReturnValue(new Promise<Response>((resolve) => { finish = resolve }))
    const current = mount(LoginForm)
    await fill(current.host)
    submit(current.host)
    current.app.unmount()
    mounted.splice(mounted.findIndex((item) => item.app === current.app), 1)
    finish(success({ user, csrf_token: 'csrf' }))
    await vi.waitFor(() => expect(current.auth.state.status).toBe('authenticated'))
    expect(current.authenticated).not.toHaveBeenCalled()
    current.host.remove()
  })
})
