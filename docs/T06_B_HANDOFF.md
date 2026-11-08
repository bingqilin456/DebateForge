# T06 B 部分交接与本地验证

日期：2026 年 11 月 7 日。依据本轮用户明确授权，在原仓库直接实施 T06 的 B 部分。PB 和 T01 的历史结项不重验、不回退。正式依据为 [API](API.md)、[ARCHITECTURE](ARCHITECTURE.md)、[PRD](PRD.md) F01 与 [TASKS](TASKS.md) T06，不使用旧个人任务包覆盖正式契约。

**状态：B 部分实现及本地自动化验证已完成，待项目负责人审查；T06 整体仍为进行中，未通过整体验收。** A 的入口/首轮面板集成、真实 T03/T05 浏览器联调均未执行。

## 授权与文件范围

- 已确认原工作目录可写，开始时 Git 工作区干净；未复制仓库，未改分支，未提交、推送或部署。
- 不修改 `frontend/src/App.vue`、`frontend/src/main.ts`、`frontend/vite.config.ts`、健康 API/原有健康测试或后端。
- 不实现数据库、服务端认证、辩题、首轮面板、真实模型或部署。本次 frontend 认证代码不代表服务端 T03 已完成。

| 文件（相对仓库根目录） | 修改内容 |
| --- | --- |
| `frontend/src/api/client.ts` | 共享传输、业务信封/错误校验、CSRF、204、超时及 PB 兼容入口 |
| `frontend/src/api/client.test.ts` | client 契约、错误、CSRF、HTTP 总超时及回归测试 |
| `frontend/src/api/auth.ts` | 注册、登录、me、退出四个正式认证 API |
| `frontend/src/api/auth.test.ts` | 请求/响应、ID、CSRF 与非法凭据验证 |
| `frontend/src/types/api.ts` | 复用 AuthCredentials、User、AuthSession，保留 LiveHealth |
| `frontend/src/auth/credentials.ts` | 用户名归一化与 Unicode/字节长度校验 |
| `frontend/src/auth/credentials.test.ts` | 输入边界、空白与非 BMP 字符测试 |
| `frontend/src/auth/state.ts` | 只读共享状态、私有内存 CSRF、恢复与认证代次保护 |
| `frontend/src/auth/state.test.ts` | 恢复、失效、退出失败、旧响应与并发认证测试 |
| `frontend/src/components/auth/AuthForm.vue` | 两种表单共用的字段、校验、请求及受控反馈 |
| `frontend/src/components/auth/LoginForm.vue` | 独立登录组件与公开事件 |
| `frontend/src/components/auth/RegisterForm.vue` | 独立注册组件与公开事件 |
| `frontend/src/components/auth/AuthForm.test.ts` | 实际挂载 Vue/Arco 的 DOM 交互测试 |
| `frontend/package.json`、`frontend/package-lock.json` | 仅新增精确锁定的测试依赖 jsdom |
| `AGENTS.md`、`README.md`、`docs/PLAN.md`、`docs/TASKS.md` | 仅同步本轮 T06 B 授权/进展；不更改 PB/T01 结论或其它任务授权 |
| `docs/T06_B_HANDOFF.md` | 本交接、代码说明、验证与待执行项 |

## 给 A 的接线说明

1. 入口导入 `frontend/src/auth/state.ts` 的 **同一个 `authState`**，在挂载时显式 `await authState.restore()`。初始 `status` 为 `unknown`；有效 me 恢复为 `authenticated`，有效 `401 unauthenticated` 恢复为 `anonymous`。503、超时和代理 HTML 不等于退出：保留原状态、显示受控提示并允许用户手动恢复重试。`restore()` 会合并同时进行的 me；认证写操作期间调用它不另发请求。
2. 导入 `LoginForm.vue`、`RegisterForm.vue`，不传 `auth` 时默认使用上述共享实例；如注入测试/独立实例，表单与面板必须使用同一实例，且挂载后不替换实例。
3. 登录事件为 `authenticated(user)`、`show-register()`；注册事件为 `registered(user)`、`show-login()`。事件只含公开 User，不含密码、CSRF 或 Session。`registered` 只表示注册成功，可切到登录表单，**不自动登录**。页面切换由 A 决定，不需要新增 Router/Pinia。
4. 面板请求必须复用 **`authState.client`**，不要另建无认证钩子的 client。JSON 请求传 `expectedStatus` 与运行时 `validate` 类型守卫，成功只返回 `data`；守卫应按正式 DTO 校验，不能以类型断言代替。首轮可能返回 200/201/202，可显式用 `expectedStatus: [200, 201, 202]` 并根据 `turn.status` 展示/查询；本次未实现该面板逻辑。
5. 生成请求显式传 `timeoutMs: GENERATION_TIMEOUT_MS`（从 client 导出，70 秒）；普通业务默认 5 秒，作为正式“短超时”的本地选择，不改变服务端预算。健康请求默认 3 秒。超时均包括读取正文。client 不自动重试任何请求，不生成新幂等键、不发起生成或轮询；生成状态不明时按 API 由 A 查询详情，2 秒间隔、最多 120 秒。
6. 退出调用 `await authState.logout()`。204 后清用户与 CSRF；401 失效也清状态，但保留错误让入口按码处理；503/网络错误不假装已退出。退出结果不明时可提示用户显式恢复查询，不自动重发。
7. 监听 `authState.state.status` 和 `user?.id`，登录失效/切换用户时，A 负责清空首轮面板中的旧用户内容、轮询和待提交数据。共享 client 只清认证，不擅自跳转或操作业务面板。
8. 本地继续使用 `http://127.0.0.1:5173` 与现有 Vite 原路径代理，不换成 localhost，不添加 CORS，不手写 Origin/Cookie/Authorization。Cookie 与 Origin 由浏览器处理；登录/注册不带旧 CSRF，其它 Session POST 使用内存 `X-CSRF-Token`。

最小接线片段（供 A 使用，未写入入口）：

```ts
import { authState } from './auth/state'
import LoginForm from './components/auth/LoginForm.vue'
import RegisterForm from './components/auth/RegisterForm.vue'
import { ApiError, GENERATION_TIMEOUT_MS } from './api/client'

const auth = authState.state // 保持响应式代理，不把 user/status 解构成一次性快照。
// onMounted 中显式调用 restore 并 catch；界面根据 auth.status 分支。
// <LoginForm :auth="authState" @authenticated="..." @show-register="..." />
// <RegisterForm :auth="authState" @registered="..." @show-login="..." />
// 生成请求 options 使用 timeoutMs: GENERATION_TIMEOUT_MS。
// catch 中使用 error instanceof ApiError，再按 status/code 分支。
```

## 关键代码解释

- **client 的边界**：健康接口不套业务信封，保留 `requestJson`/`getLiveHealth` 现有行为；业务只接受同源 `/api/v1/` 路径。GET 不发正文，只有有正文的 POST 才加 JSON Content-Type；退出 POST 无正文。业务 JSON 必须有信封及与响应头一致的 request_id。204 单独返回 void，不调用 json()，也不要求 Content-Type。
- **失败不能伪装成功或退出**：代理 HTML、坏 JSON、非法 DTO/信封、错误状态都是受控失败；只有受保护请求的有效 `401 unauthenticated` 清认证。`401 invalid_credentials`、403、503 和网络失败不做此转换。ApiError 保留 kind/status/code/requestId/retryAfterSeconds，不保存原始错误正文；页面只显示本地中文映射，不直接输出未知服务端 message。
- **竞态与退出**：私有 version 在认证写操作/状态变化时递增。请求发出时捕获 version，旧请求迟到的 401 不清新 Session；旧 me 成功也不能覆盖新登录。登录/退出不排队、不重发，进行中第二个认证写操作直接受控拒绝，避免已知并发下 Cookie 响应乱序。认证写操作失败会释放 pending。
- **仅内存**：CSRF 是 state 模块闭包中的变量，不放入 localStorage/sessionStorage，也不在公开响应式状态中。刷新后必须通过 Cookie/me 恢复；User 的 ID 始终为字符串。Session 原值从不由 JS 读取。前端校验只改善体验，不替代服务端权限、CSRF、密码哈希与限流验证。
- **表单链路**：`submit → 共享凭据校验 → authState.login/register → auth API → client`。用户名 trim/转小写，密码原样发送，按 Unicode 码点与 UTF-8 字节校验，不用 HTML maxlength 截断非 BMP 字符。同步提交锁防双击；请求结束清密码，卸载后不发迟到组件事件；状态结果仍归共享状态管理。

## 依赖与官方资料核对

仅新增 devDependency **jsdom 28.1.0，MIT**。它是长期维护的 DOM 模拟器，也是 Vitest 官方支持的环境；用于实际 Vue/Arco 挂载与输入/提交事件测试，不进入生产依赖。已从 npm 官方 registry 核对该版本 engines/许可证，兼容锁定的 Node 24.11.1；当前 jsdom 31.0.0 的 engines 不兼容，未盲用最新版。替代为 happy-dom（另一 DOM 模拟方案）或真实浏览器组件测试；本轮不新增 @vue/test-utils、Axios、Pinia 或 Router。

查阅 Vue reactivity/helpers/lifecycle、Vitest environment、Vite JavaScript/build 官方文档及 MDN Fetch 文档；Arco 的 inputAttrs、disabled、htmlType 等额外核对已安装官方包的声明文件，并通过真实组件 DOM 测试验证。资料入口：[Vue](https://vuejs.org/api/reactivity-core.html)、[Vue useId](https://vuejs.org/api/composition-api-helpers.html#useid)、[Vitest](https://vitest.dev/guide/environment.html)、[jsdom](https://github.com/jsdom/jsdom)、[Arco](https://arco.design/vue/component/input)、[Vite](https://vite.dev/guide/api-javascript.html)、[Fetch](https://developer.mozilla.org/en-US/docs/Web/API/Fetch_API/Using_Fetch)。

默认用户目录 npm cache 受限，因此只把 npm cache 配到已授权仓库的 `.npm/`（原忽略规则已覆盖），未改全局 npm 配置、未提升权限；仓库实际写入检查已通过。安装命令为 `npm install --save-dev --save-exact jsdom@28.1.0`，安装时 npm audit 报告 0 vulnerabilities，不代表服务端或公网安全验收。

## 实际验证记录

环境：Windows、Node 24.11.1、npm 11.6.2；沿用现有 Vue 3.5.44、Vitest 5.0.5、Vite 8.3.5。以下命令在原仓库根目录执行：

| 命令/检查 | 结果 |
| --- | --- |
| 初始 `npm --prefix frontend test` | 原健康检查 9 项通过 |
| client 测试先行 | 初始 33 项失败（createApiClient 尚不存在）；随后实现转绿 |
| auth/credentials/state 测试先行 | 初始 3 个 suite 因目标模块不存在加载失败，随后实现转绿 |
| 表单测试先行 | 初始因独立组件不存在加载失败，随后实现转绿 |
| 技术自查补测 | 未知错误码原型名称 3 项失败、显式多成功状态 3 项失败，均修复后转绿 |
| 首次 typecheck | 暴露公开请求选项类型不能命名（TS4058/TS4023），导出类型后修复 |
| 最终 `npm --prefix frontend run typecheck` | 通过 |
| 最终 `npm --prefix frontend test` | 6 个文件、111 项全部通过：client 40、auth API 15、凭据 15、状态 16、表单 16、原健康 9 |
| 最终 `npm --prefix frontend run build` | 通过；入口仍为原 PB 健康页，不宣称表单已集成进 App |
| 两个独立表单额外生产模式打包 | 通过；缓存输出到 `frontend/node_modules/.cache/t06-b-build/`，不改 App、不创建部署配置 |
| Git/文档/范围检查 | `git diff --check` 通过；实际 20 个变更文件与清单一致，本地文档链接有效；入口/健康 API 与原测试/后端/正式 API 和架构契约均未修改；已有锁文件包版本无变化，缓存与产物被原规则忽略；有限敏感值模式及源码人工检查未发现真实凭据 |

额外打包可在 frontend 目录复现（仅本地产物，不发布）：

```powershell
node --input-type=module -e "import { build } from 'vite'; await build({ configFile: 'vite.config.ts', build: { outDir: 'node_modules/.cache/t06-b-build', emptyOutDir: false, lib: { entry: { LoginForm: 'src/components/auth/LoginForm.vue', RegisterForm: 'src/components/auth/RegisterForm.vue' }, formats: ['es'] }, rolldownOptions: { external: ['vue', '@arco-design/web-vue'] } } });"
```

测试包括真实本地 HTTP 的慢响应头/慢分块正文与取消、真实 Vue/Arco DOM 的提交/禁用/提示行为；认证响应使用受控 Fake fetch，不是假称真实后端。技术审查已核对认证错误分支、CSRF 生命周期、迟到响应、退出失败、Unicode 边界、原健康兼容及入口范围，发现的问题均已补测修复。

## 明确待执行与验收边界

- 只读检查当前后端，正式运行路由仍仅 `GET /health/live`；**真实 T03/T05 接口未就绪，真实认证/辩题/创建/首轮/刷新浏览器联调未执行**。
- A 集成 App、页面切换、恢复/退出反馈、面板登录失效清理、生成状态查询，并按 API 对真实链路留证；本次没有替 A 集成入口。
- 真实 Cookie 属性/会话吊销、Origin/CSRF 拒绝、服务端限流、密码存储、数据库与费用控制未在本轮验收；需要 T03/T05 及对应任务专项验证。未运行后端测试或人工浏览器认证验收；DOM 模拟器不能替代它们。
- PB 和 T01 保持结项。T06 不标记“已完成”或“整体验收通过”，Phase 1 不认定通过；项目负责人最终审查尚待进行。未追填实际人时，不虚构成员学习/人工讲解登记。

## 2026-11-08 本地提交前复核

用户已在自己的 PowerShell 中创建 `feat/t06-b-auth`，原有 T06-B 改动保留。助手仅使用只读 Git 命令复核元数据，未尝试暂存、提交或推送。

- 变更范围再次核对为本交接清单中的 20 个文件，A 的入口、后端与正式契约仍不在改动范围。
- 按用户明确要求，将 AGENTS 中的旧 `codex/` 默认改为按用途使用 `feat/`、`fix/`、`docs/` 等前缀；没有扩大任何操作授权。
- 本次实际重新执行 `npm.cmd --prefix frontend run typecheck`、`npm.cmd --prefix frontend test`、`npm.cmd --prefix frontend run build`，均通过；测试为 6 个文件、111 项全部通过。
- `git diff --check` 通过；常见私钥/API Key 模式与冲突标记检查未命中，不代表穷尽秘密类型或完成安全专项验收。
- 此处只记录提交前验证，不宣称已产生 commit、已推送或 T06 整体验收通过。真实 T03/T05 和浏览器认证联调仍待执行。
## 2026-11-08 本地模拟预览与真实浏览器交互补验

本节只补本地预览与交互核验，不重写前述 B 实现，不重做 PB/T01。上面的 20 文件是上一批交付记录；**本轮增量仅以下 6 个文件**，T06 整体仍为“进行中”。未修改 A 的正式入口、首轮面板、后端、正式契约或依赖清单。

| 本轮文件 | 用途 |
| --- | --- |
| `frontend/auth-preview.html` | 独立 HTML，不替换正式 index.html |
| `frontend/src/preview/auth/main.ts` | 仅开发模式且 hostname 为 127.0.0.1 时装配预览；HMR 卸载恢复本页 fetch |
| `frontend/src/preview/auth/AuthPreview.vue` | 复用 LoginForm/RegisterForm 与 createAuthState，提供模拟场景、延迟和无敏感正文的计数 |
| `frontend/src/preview/auth/mockAuth.ts` | 仅内存模拟 4 个认证端点，拒绝其它路径/origin，不回退真实 fetch，不保存账号或密码 |
| `frontend/src/preview/auth/mockAuth.test.ts` | 新增模拟器范围、取消、注册不登录及错误信封的针对性测试 |
| `docs/T06_B_HANDOFF.md` | 在原文档补充本节，不另建准备文档 |

### 打开方式与人工复查路径

本轮已确认并复用原仓库现有的 `127.0.0.1:5173` Vite 开发服务器，预览 HTML 和模块均返回 200；没有另开端口、终止旧服务器或更改 Vite 配置。先前 PowerShell 端口查询未显示监听，启动探测因端口占用退出，随后用 netstat 与实际 HTTP 响应确认现有服务并复用。

直接打开 **`http://127.0.0.1:5173/auth-preview.html`**。若开发服务器已经停止，才从仓库根目录执行 `npm.cmd --prefix frontend run dev`，再打开同一地址；5173 已占用时先确认并复用当前仓库服务，不结束他人的进程。不要双击 HTML 文件，也不需要启动后端。正式 `/` 入口没有被替换。

1. 页首固定标注“仅本地模拟，不是真实注册登录，也不是真实后端联调”。请勿输入真实密码。
2. 示例输入为 `preview_user` / `preview-pass-123`，两者仅用于演示；“模拟成功”接受任意通过原表单校验的输入，并不校验真实账号口令。
3. 先用空表单或短密码提交：应出现校验提示，登录计数不增加。随后选择“模拟成功 + 1.5 秒”，输入有效示例并双击登录：等待中字段/按钮禁用，只增加 1 次登录；结束后出现成功提示且密码为空。
4. 点击“重置预览”后转到注册，成功应只增加注册次数，登录次数仍为 0、认证状态仍为未登录；需要用户自己点击“去登录”，不自动登录。
5. “登录 401 / 注册 409”“429”“503”“模拟网络失败”用于检查失败提示；“6 秒延迟”用于触发既有 client 的 5 秒短超时。请求结束后应清密码、恢复按钮，不能自动重发。

模拟会话只活在当前页面内存，刷新即清空。mock 不写 Cookie/localStorage/sessionStorage，不展示密码、请求正文或 CSRF token，不代表真实 Session、Origin、CSRF 或限流已经实现。预览入口按 [Vite 开发常量](https://vite.dev/guide/env-and-mode.html) 用 DEV 守卫，并独立于默认 [HTML 构建入口](https://vite.dev/guide/build.html#multi-page-app)；本轮没有修改生产构建入口或创建部署配置。

### 本轮实际执行结果

工具：沿用本机已有的 Playwright 1.62.1 与系统 Microsoft Edge 156.0.4314.58，没有安装浏览器或新增项目依赖。**下表是 Codex 通过真实 Edge（headless）进行输入、点击、双击、键盘操作并查看截图的记录，不是 jsdom；用户后续手动复查的确认另见本文末尾，不以自动化操作替代人工结论，也不据此认定 T06 整体验收通过。**

| 浏览器交互检查 | 实际结果 |
| --- | --- |
| 独立页面与模拟标识 | 通过；原登录组件出现，初始化只有 1 次模拟 me，无登录/注册请求 |
| 空输入、非法用户名、短密码、129 个非 BMP 码点 | 通过；受控校验提示，模拟登录次数保持 0 |
| 登录等待与防重复 | 通过；双击再按 Enter 仅 1 次模拟登录，字段/按钮禁用且 aria-busy=true |
| 登录成功、归一化、密码清理 | 通过；带首尾空白/大写的用户名归一化；模拟状态更新，成功提示后密码为空 |
| 204 模拟退出 | 通过；1 次退出后未登录，无 JSON 空正文解析错误；没有操作真实 Cookie |
| 注册等待、合法非 BMP 密码、不自动登录 | 通过；12 个 emoji 可提交，双击/Enter 只注册 1 次；登录次数 0，仍未登录，密码清空 |
| 注册 409 与登录 401 凭据错误 | 通过；对应中文提示，用户名保留、密码清空，不自动重试或误判登录成功 |
| 429 限流 | 通过；出现 15 秒提示，仅 1 次请求增量，密码清空、按钮恢复 |
| 503 与模拟网络失败 | 通过；均显示受控提示、清密码、不自动重试 |
| 5 秒超时与迟到响应 | 通过；6 秒模拟延迟触发短超时，等待解除、密码清空；原延迟窗口过去后未迟到登录或重试 |
| 用户显式重试 | 通过；重新输入密码并按 Enter 才增加下一次登录，成功后再次清密码 |
| 刷新与存储隔离 | 通过；刷新清空模拟会话；新浏览器上下文 Cookie/localStorage/sessionStorage 均为 0；没有真实 /api/v1/auth/ 网络请求 |
| 桌面与移动视口 | 已查看 1280×960 桌面和 390×844 移动截图；390px 页面无横向溢出，标签点击及 Tab 可聚焦字段 |
| 运行时错误 | 本轮浏览器 pageerror 与 console.error 均为 0 |

交互脚本共记录 **16 项通过**（上表合并展示），本机证据为 `frontend/test-results/t06-b-auth-preview/results.json` 及同目录 `desktop.png`、`login-waiting.png`、`register-success.png`、`rate-limited.png`、`mobile.png`。该目录沿用原忽略规则，只是本机验收产物，不提交截图或敏感正文。

针对新增代码实际执行：

- 先写 `mockAuth.test.ts`，因目标模块尚不存在加载失败；补实现后运行 `npm.cmd --prefix frontend test -- src/preview/auth/mockAuth.test.ts`，**1 个文件、11 项通过**。
- `npm.cmd --prefix frontend run typecheck`：通过，覆盖新增的预览 TypeScript/Vue 文件。
- 原登录/注册组件、认证状态、API client 均未改；本轮未机械重复完整 111 项测试、PB/T01 或正式生产构建。新预览已由现有 Vite 实际编译并在 Edge 运行；之前的整套验证记录仍是之前批次的结果，不冒充本轮重跑。

### 本地验收状态与给 A 的依赖

- **本地模拟交互人工复查：已于 2026 年 11 月 8 日由用户明确确认符合预期。** 具体仅限下文三项；不等于 T06 整体或真实联调已通过最终验收，不要求重做 PB/T01。
- **真实 T03/T05 联调：未执行。** A 仍按本文件现有接线说明接入正式入口；验证真实 Session Cookie、Origin/CSRF、过期/退出及认证限流，再接真实辩题与首轮链路。模拟器不替代这些验证，不另生成准备包。
- 本轮不改 T06 整体验收状态，不提交、不推送、不合并、不部署。

### 用户手动复查确认（2026-11-08）

用户在打开本地模拟预览并操作后，明确回复“符合预期了”。依据紧邻该确认之前列出的三项复查，记录如下；这是用户的手动确认，不是 Codex 代签，也不外推为其它未报告场景的人工验收。

| 本地模拟复查项 | 用户确认结果 |
| --- | --- |
| 模拟成功、1.5 秒延迟下双击登录：等待期间禁用，只增加一次登录，请求结束密码清空 | 符合预期 |
| 重置后注册：注册成功，登录次数仍为 0，状态仍未登录，不自动登录 | 符合预期 |
| 切换模拟凭据错误：显示用户名或密码错误，密码清空，按钮恢复，不自动重试 | 符合预期 |

附图可直接观察到第三项的 401 提示、空密码框、未登录状态和可用登录按钮；前两项以用户明确确认作为依据，不仅凭单张截图推断。未把包含工作区侧栏等无关内容的原始截图复制进仓库。

截图中的密码保存提示与表单密码清空属于不同范围：本次确认页面请求结束后的清理，不代表已检查或操作浏览器密码库。演示只使用公开示例输入，不使用真实密码。

**结论：本轮本地模拟预览交互已获用户人工确认，可收尾。T06 整体仍为进行中；真实 T03/T05 联调、A 的正式入口/首轮面板集成及 T06 整体最终验收仍待执行。** 本次仅更新本交接记录，不改源码，不重跑已通过测试，不提交、不推送、不合并、不部署。
