# Phase 2 最终整合 Spec 与执行清单

日期：2026-11-14。负责人本轮要求完成两个指定远程分支的整合、T08/T11 页面验收、累计回归、文档收尾和 PR 合入 `main`；明确授权分支、Commit、Push、PR、Merge，禁止真实模型调用、公网部署及启动 Phase 3。

## 输入、输出与边界

- 基线：`origin/codex/p3-format-stream`（`008d552`，Batch 0–5）与 `origin/feat/t08-t11-views`（`ad18f8c`）。开始时工作区干净；整合分支 `codex/phase2-final`。
- 输出：保留两分支有效成果；T08 选题/双立场创建并打开详情；T11 本人历史分页并打开既有详情；实际检查结果与可追溯 PR。
- 复用已有 API、Session、CSRF、DTO 和详情状态机，不增加接口、数据库迁移、赛制选择或其他 Phase 3 功能。
- 测试写入仅允许 `127.0.0.1:13307/siyige_test`（MySQL 8.4.13）；模型仅 Fake 或 disabled。开发库、生产库不操作。

## 页面契约与失败路径

| 范围 | 输入与权限 | 成功输出 | 失败路径与验收 |
| --- | --- | --- | --- |
| T08 | 已登录；启用辩题 ID 与 `pro/con`；POST 使用共享 CSRF | 五题与说明、双方相反立场；创建后只交接一次到会话详情 | 未选题禁提交；空列表明确提示；503/网络/格式异常受控提示并手动重试；创建结果未知锁定提交、引导历史核实；旧账号/卸载后的响应作废 |
| T11 | 本人 Session；`page/page_size`（默认 1/20） | 题目快照、状态、双方立场、按会话 `total_rounds` 展示轮数、本地时间；翻页与打开详情 | 空列表禁翻页；失败不冒充空列表；加载中防连点；末页消失只补查一次；401 清除旧内容；切换账号丢弃旧响应 |
| 整合 | `standard_4` 与历史 `quick_3` 并存 | 四轮显示 4/4、三轮显示 3/3；真实 Fake 四轮八消息和独立总结可重读 | 不回退赛制驱动、版本重试、幂等、租约及所有权保护；不宣称已实现 SSE 或其他目标功能 |

## 文件与验证映射

- `frontend/src/views/TopicsView.vue`、`HistoryView.vue` 及对应 Vitest：接入 B 页面并覆盖状态分支；`App.test.ts` 保留顶层视图与详情交接回归。
- `frontend/e2e/views.spec.ts`：Chromium 真实渲染，受控响应验证双立场、20+1 分页、刷新、空列表、503/网络/格式错误、创建结果未知、401、390px 移动端。
- `frontend/e2e/live.spec.ts`：显式开关下，经真实 Vite 代理连接 Go + 固定隔离 MySQL + Fake，验证新账号空历史、五题、双立场、四轮总结、21 条分页、刷新、退出重登和固定测试 MySQL 重启后重读。保留合成数据，不执行清库。
- `frontend/playwright.config.ts`、npm 锁文件、TS 配置与 CI：锁定 `@playwright/test`，浏览器验收加入现有前端门禁。
- PRD/PLAN/TASKS、README、文档导航与最终验收记录：区分已实现、已验证、未来目标及历史快照。

## 执行与准出清单

- [x] 同步远程、核对工作区，创建整合分支并合并队友提交。
- [x] 用混合赛制断言复现历史页固定三轮显示错误，再改为使用 DTO `total_rounds`；补齐 B 测试中的赛制字段。
- [x] 前端 typecheck、186 项单测、build；Chromium 页面测试及真实 Fake 联调全部通过。
- [x] 后端 `go test -p 1 -count=1 ./...`、`go vet ./...`、`go build ./...`；迁移工具独立模块三项检查。
- [x] 后端隔离 MySQL 与迁移集成测试通过，保留测试探针证据；原 SQL 迁移不改。
- [ ] 更新文档并检查链接、空白、秘密与改动范围；Commit/Push，创建 PR 触发 Actions。
- [ ] 当前 PR 全部门禁通过后普通 Merge 到 `main`，核对远程提交与主干 CI，登记正式结项；不进入 Phase 3。

逐项实际结果见 [最终整合验收记录](../acceptance/PHASE2_FINAL_ACCEPTANCE.md)。本清单中的授权来自负责人本轮指令，不延伸到后续阶段。
