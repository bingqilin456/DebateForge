import '@arco-design/web-vue/dist/arco.css'
import '../../styles/theme.css'

const root = document.querySelector<HTMLElement>('#issue-card-preview')
if (!root) throw new Error('缺少隔离预览挂载点')

// 仅独立 HTML 使用此入口；不修改正式 main/App，不在生产或非回环站点启用 Mock。
// 独立端口由 e2e/issue-card.vite.config.ts 固定，避免与共享 5173 端口争用。
if (import.meta.env.DEV && window.location.hostname === '127.0.0.1') {
  const [{ createApp }, { default: IssueCardPreview }, { createIssueCardPreviewMock, sessionPreviewStorage }] = await Promise.all([
    import('vue'), import('./IssueCardPreview.vue'), import('./mockIssueCard'),
  ])
  // 持久化边界可注入：预览默认用 sessionStorage，让“刷新读取人工作业”可观察。
  const mock = createIssueCardPreviewMock(sessionPreviewStorage())
  const app = createApp(IssueCardPreview, { mock })
  app.mount(root)
  import.meta.hot?.dispose(() => { app.unmount() })
} else {
  root.textContent = '本页只支持通过 127.0.0.1 的 Vite 开发服务器预览，不提供真实数据。'
}
