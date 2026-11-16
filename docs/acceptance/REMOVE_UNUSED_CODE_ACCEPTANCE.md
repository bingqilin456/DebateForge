# 确认废弃代码清理（2026-11-16）

## 基线与授权

- 更新远端引用后，从最新整合提交 `0884dae5936af8fadba1b83ab3999941fbf7119e` 新建 `codex/remove-unused-code`。该提交包含②创建统一 `aa8593b` 与④共享值整理成果；未以较旧的 main 替代基线。开始时工作区干净，未覆盖已有修改。
- 用户授权本任务本地清理、验证、Commit、普通 Push 到本分支；不含 PR、合并、部署、数据库写入或真实模型调用。仅逐文件删除受版本控制源码和专属测试，没有递归删除目录或清理本地数据。
- 先运行 CodeGraph explore；部分索引提示过期，改用实际文件、`git grep` 与配置核对，未初始化或改写索引。

## 删除依据

核对范围包括全仓受版本控制引用、Vue 模板事件、动态导入及组件解析、两个 HTML 入口、main.ts、auth 预览入口、Vite/Vitest/TypeScript/Playwright 配置、npm 脚本、CI、infra/deploy 脚本、文档及测试。未发现外部包导出或运行时按字符串加载下列废弃符号的入口。

| 删除项 | 证据与处理 |
| --- | --- |
| `frontend/src/api/health.ts`、`health.test.ts` | 正式 App 早已改为登录/辩论入口；`getLiveHealth` 只由专属测试调用。HTML、预览、构建、脚本均不加载该模块。88 行测试只服务旧封装，随模块删除。 |
| `client.ts` 的 `requestJson`、`withinTimeout` 的 health 参数及专用提示；前端 `LiveHealth` 类型 | 唯一调用链为上述废弃模块/测试；删除后无前端引用。保留业务 JSON 解析、总超时、取消及安全错误处理，真实 HTTP 慢响应头/慢正文测试仍在。 |
| TopicsView/HistoryView 的 `open-debate` 声明及 App 对应监听 | 两视图均不再发出此事件；实际分别只发 `created` / `select-debate`。保留“一次操作只交接一次”的测试断言。 |
| `createDebateApi` 的 `topics/list/create` 别名 | 生产调用均使用正式命名方法；旧别名仅残留在测试及旧状态类型中。保留正式 `listTopics/listDebates/createDebate`，原输入、分页、流派校验测试转至正式方法，没有删除这些有效测试。 |
| `DebateApi` 中旧创建/列表成员和相应测试桩 | 创建流程统一后状态机只处理已创建会话，未访问这些成员。移除强制桩及无用可选成员；保留实际调用的详情、JSON 发送、重试、总结与可选流式成员。 |

## 保留原因

- `quick_3`、`standard_4`、会话 `total_rounds`、轮次边界及历史读取测试：仍是正式历史兼容和数据校验能力。
- JSON `send`、总结查询/生成/重试及 SSE 认领恢复：正式端点和契约测试仍有用途，不按生产默认 SSE 就判废弃。
- 创建参数的位置式重载、顶层 `getDebateDetail`：现有测试及历史交接明确约定兼容/调用契约，本轮没有足够证据认定废弃，保留。`detail` 别名由状态机实际调用，保留。
- TopicsView 的 `createdDebate`、`creationUncertain` 及 App 的创建中/结果不明状态：承担成功交接前禁重提、结果不明时跨页保留提交锁及账号切换隔离，均有测试用途。
- FirstTurnPanel / firstTurn 名称、会话 ID 与地址同步：仍是实际详情、连续对辩及恢复入口，不因名称旧而重命名或删除。
- 后端 live/ready 端点和健康测试、OpenAPI `LiveHealth`、Nginx 精确健康路由、infra 探针及 t04-smoke：仍承担健康契约、部署与诊断；没有改动。认证预览及动态导入仍被浏览器测试使用，保留。
- T06 交接及 Bootstrap 学习笔记中的旧健康封装描述是历史记录，已有历史范围说明；本记录明确其删除时间，不回写旧证据为本轮实现。

## 验证

- `npm run typecheck`：通过。
- `npm run build`：通过，668 模块构建完成。
- 首次受限环境 `npm run test`：303 通过、2 失败，真实 HTTP 慢响应测试预期 timeout、实际 network。允许本地回环网络后，先定向 `src/api/client.test.ts` 40/40 通过，再全量 `npm run test` 18 文件、305/305 通过；未修改测试断言或超时来规避失败。基线 315 项减去旧健康封装专属 10 项，剩余有效测试均保留。
- `npm run test:e2e`：使用本机 Edge、Vite 与 API Mock，21/21 通过（59.6 秒），覆盖创建防重、刷新重登、历史三/四轮、结果丢失及 SSE 恢复；没有连接真实后端或模型。
- 最终受版本控制引用、文档本地链接及 `git diff --check` 检查通过；变更仅限前端清理及三份任务/验收导航文档，没有修改后端、依赖、构建配置或诊断脚本。
- 未执行后端/迁移/数据库集成、真实后端浏览器链路、Nginx 专项或真实模型验证：本次未改其代码或契约，复用既有证据，不声称完成新专项验收。

本任务技术验证与负责人签收、Phase 3 结项分开；不改变现有阶段风险或真实调用授权边界。
