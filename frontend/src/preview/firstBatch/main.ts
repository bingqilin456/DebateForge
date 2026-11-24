import '@arco-design/web-vue/dist/arco.css'
import '../../styles/theme.css'

const root = document.querySelector<HTMLElement>('#first-batch')
if (!root) throw new Error('缺少赛后预览挂载点')
root.style.cssText = 'max-width: 1280px; margin: auto; padding: clamp(12px, 3vw, 32px);'
if (import.meta.env.DEV && location.hostname === '127.0.0.1') {
  const [{ createApp }, { default: Preview }, { createIssueCardPreviewMock, sessionPreviewStorage }] = await Promise.all([
    import('vue'), import('./FirstBatchPreview.vue'), import('../issueCard/mockIssueCard'),
  ])
  const storage = sessionPreviewStorage()
  const app = createApp(Preview, { mock: createIssueCardPreviewMock(storage) })
  app.mount(root)
  import.meta.hot?.dispose(() => app.unmount())
} else {
  root.textContent = '此页是本地演示，不在正式站点启用。'
}
