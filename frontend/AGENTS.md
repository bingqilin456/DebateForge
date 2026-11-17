# frontend/ 局部规则

本文件是 `frontend/` 的局部规则，只补充本目录特有的实现与检查约定；项目边界、安全红线、全局契约不变量、授权与验收规则仍以根 [AGENTS.md](../AGENTS.md) 为准。进入本目录前先读根 AGENTS，再读本文件。

## 技术栈与编码

- Vue 3 + TypeScript + Arco Design + Vite，使用单文件组件与 Composition API；统一用 npm，保留 `package-lock.json`，不混用其他包管理器或锁文件。
- API 请求统一经 `src/api/` 的客户端封装，不散落裸 `fetch`；共享契约类型集中在 `src/types/api.ts`，避免 `any` 与重复接口类型。前端不保存模型密钥或其他秘密。
- 前端不硬编码轮数：轮次上限取自会话 `total_rounds`（由 `mode` 推导），赛制与 DTO 仍按契约校验。
- 用户与模型内容一律转义、以纯文本渲染，不使用 `v-html`；不把不可信 HTML 交给组件渲染。
- 时间在 API 中是 UTC，展示时按本地时区转换；JSON 中的 ID 按字符串处理。

## 检查命令（在 `frontend/` 下执行）

```powershell
npm ci
npm run typecheck
npm run test
npm run build
```

- 分别对应 `vue-tsc -b` 类型检查、`vitest run` 单测、`vite build` 生产构建；只改样式或文案时按影响范围选择，不无意义全量复跑。

## package scripts 与三种浏览器测试的区别

`frontend/package.json` 中三个 Playwright 入口目标不同，不可互相替代，也不得用其中一种的结果代替另一种的验收结论：

| 脚本 | 实际执行 | 需要的环境 | 用途 |
| --- | --- | --- | --- |
| `npm run test:e2e` | `e2e/views.spec.ts`、`e2e/style.spec.ts`、`e2e/stream.spec.ts` | 只需前端开发服务，响应由用例内隔离 Mock 提供 | CI 默认执行的无模型浏览器回归 |
| `npm run test:e2e:live` | `e2e/live.spec.ts` | 显式启动连接固定隔离库 `siyige_test` 且 `AI_MODE=fake` 的后端，并设置 `SIYIGE_BROWSER_LIVE=1` | 真实 Vite → Go → MySQL → Fake 的完整链路联调 |
| `npm run test:e2e:b2:live` | `e2e/strategy-fault-live.spec.ts`、`e2e/stream-live.spec.ts` | 同上再加上本机固定 Fake 故障夹具（会读取 Git 忽略目录中的准备文件） | B2 策略故障与流式故障的真实链路补测 |

- `playwright.config.ts` 固定 `baseURL` 为 `http://127.0.0.1:5173`、单 worker、不重试；仅当设置 `SIYIGE_BROWSER_EXECUTABLE` 时复用本机浏览器，否则使用 CI 的 Chromium。
- **live 与 b2:live 都不在 CI 中运行**，也不读取真实模型密钥；未启动对应后端与夹具时用例会跳过或失败，不得据此宣称已通过。
- 浏览器测试与 `npm run test`（Vitest）是不同层次：Vitest 不替代浏览器交互验收，浏览器 Mock 也不替代真实链路联调。
