# 演示图与线框源文件

本目录存放 [V2 页面设计与交互细化](../../PRD_V2_UI_DESIGN.md) 用到的演示图，以及可重新生成这些图的静态源文件。

## 文件

| 文件 | 用途 |
| --- | --- |
| `index.html` | 六屏静态线框，纯 HTML/CSS，无脚本、无外部依赖 |
| `shoot.js` | 用 Playwright 调本机 Edge 无头模式逐屏截图 |
| `shots/01-entry.png` | 开始练习 |
| `shots/02-workspace.png` | 辩论工作台（桌面，1280 px 级） |
| `shots/03-review.png` | 完赛复盘 |
| `shots/04-history.png` | 历史 |
| `shots/05-settings.png` | 设置：模型服务与额度 |
| `shots/06-mobile.png` | 辩论工作台（窄屏，390 px 级） |

## 效力

- 这些图是**线框示意**，不是视觉稿、不是已确认设计、不代表已实现。间距、配色、控件形态均为 `[设计建议]`。
- 图里的示例数据（辩题、批注文字、额度数字）都是虚构样例，不是真实会话记录或真实用量。
- 本轮只验证了渲染结果与布局；**没有**用产品代码做过浏览器验证，也没有比对真实组件。

## 重新渲染

需要 Node 与仓库内已安装的 Playwright（`frontend/node_modules`），以及本机 Edge：

```powershell
$env:NODE_PATH = "<仓库根>\frontend\node_modules"
node docs/product/ui/mockup/shoot.js "<仓库根>"
```

脚本会按 1360×1200（移动屏 430×1000）逐个元素截图，覆盖 `shots/` 下同名文件。改了 `index.html` 的布局或文案后重新跑一次即可。

## 维护

- 改线框时同步检查对应模块里的文字线框和状态表，避免图与文字不一致。
- 新增页面时：在 `index.html` 加一节带 `id="sN"` 的区块，在 `shoot.js` 的 `shots` 数组登记输出名，再在模块文档里引用。
- 不要把这些图当验收证据：验收证据在 `docs/acceptance/` 下，按实际执行的检查登记。
