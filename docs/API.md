# Project Bootstrap API 契约

日期：2026 年 11 月 1 日。仅冻结用户批准的工程初始化接口；不代表 T01 全部业务 DTO 已冻结。产品范围仍以 [PRD](PRD.md) 为准。

## 存活检查

- 方法与路径：`GET /health/live`，不带 `/api/v1` 前缀。
- 输入：无请求体，无查询参数要求，无认证要求；只读，不调用数据库或模型。
- 输出：HTTP 200，`Content-Type: application/json; charset=utf-8`，JSON `{"status":"alive"}`，不套业务包装。
- 响应头：`X-Request-ID` 为服务器生成的关联标识，`Cache-Control: no-store`。
- 语义：仅表示 Go HTTP 进程可以处理请求，不代表数据库、模型或整个产品就绪。

## 未实现路径和失败

`/health/ready` 本阶段未注册，返回 404；后续 T02 接入 MySQL 后再实现配置与 DB 连通检查。`/api/` 目前也没有业务路由。

未知路由返回 404，已知路径不支持的方法返回 405，意外 panic 返回受控 500。错误 JSON：

```json
{"error":{"code":"not_found","message":"接口不存在"},"request_id":"服务端生成的标识"}
```

错误码分别为 `not_found`、`method_not_allowed`、`internal_error`；不包含堆栈、请求正文或内部错误细节。当前没有业务接口，不借此声称认证、权限或限流已实现。

## 本地请求链路

浏览器访问 `http://127.0.0.1:5173`，用相对路径 `/health/live` 请求 Vite；Vite 将 `/health/`、`/api/` 前缀原样代理至 `http://127.0.0.1:8080`，不去掉前缀，不使用 SPA HTML 作为 API 成功结果。

前端检查 HTTP 状态、JSON Content-Type 和 `status=alive`，总超时 3 秒（包括读取响应体）。失败只显示受控中文提示，不自动重试；用户点击后重新检查。
