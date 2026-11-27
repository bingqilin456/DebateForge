import '@arco-design/web-vue/dist/arco.css'
import '../../styles/theme.css'

const root = document.querySelector<HTMLElement>('#arena-poc')
if (!root) throw new Error('缺少竞技场 Visual POC 挂载点')

if (import.meta.env.DEV) {
  const [{ createApp }, { default: Preview }] = await Promise.all([
    import('vue'),
    import('./NeonArenaPOC.vue'),
  ])
  const app = createApp(Preview)
  app.mount(root)
  import.meta.hot?.dispose(() => app.unmount())
} else {
  root.textContent = '此页是本地 Visual POC，不在正式站点启用。'
}
