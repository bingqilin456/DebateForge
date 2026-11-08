import '@arco-design/web-vue/dist/arco.css'

const root = document.querySelector<HTMLElement>('#auth-preview')
if (!root) throw new Error('缺少本地预览挂载点')

// 仅独立 HTML 使用此入口；不修改正式 main/App，不在生产或非回环站点启用模拟。
if (import.meta.env.DEV && window.location.hostname === '127.0.0.1') {
  const [{ createApp }, { default: AuthPreview }, { createPreviewAuthMock }] = await Promise.all([
    import('vue'), import('./AuthPreview.vue'), import('./mockAuth'),
  ])
  const nativeFetch = window.fetch
  const mock = createPreviewAuthMock(window.location.origin)
  window.fetch = mock.fetch
  const app = createApp(AuthPreview, { mock })
  app.mount(root)
  import.meta.hot?.dispose(() => { app.unmount(); window.fetch = nativeFetch })
} else {
  root.textContent = '本页只支持通过 127.0.0.1 的 Vite 开发服务器预览，不提供真实认证。'
}
