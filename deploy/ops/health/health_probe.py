#!/usr/bin/env python3
"""T16-B1 最小健康与备份检查工具（只读）。

本步只验证两件事：
1. 应用健康端点：GET /api/health/live 与 GET /api/health/ready 的状态码、
   Content-Type 和 JSON 字段。HTML 200、非 JSON、缺 status、状态码不符
   一律判失败，禁止把 SPA 页面或代理错误页当成健康。
2. 备份文件：必须存在；最后成功/存档时间不得超过允许的最大年龄。
   时间来源可显式指定状态文件，默认取备份文件自身 mtime。

约束：只用标准库；只允许回环目标；只发 GET；不输出凭据、Cookie、
请求头或业务正文。容器编排、证书续期、告警投递与应用回退不在本步范围。
"""

from __future__ import annotations

import argparse
import http.client
import ipaddress
import json
import math
import socket
import ssl
import stat as statmod
import sys
import urllib.error
import urllib.parse
import urllib.request
from dataclasses import dataclass
from datetime import datetime, timezone
from pathlib import Path

# 响应体上限：健康端点只该返回极短 JSON，超限视为异常，避免被大页面拖住。
MAX_BODY_BYTES = 64 * 1024
DEFAULT_TIMEOUT = 5.0
LIVE_PATH = "/api/health/live"
READY_PATH = "/api/health/ready"

# 只白名单已知类型；其他 Content-Type 一律不原样回显，避免把服务端可控内容
# （错误页、注入串）当成证据写进报告。
KNOWN_CONTENT_TYPES = {
    "application/json": "application/json",
    "text/html": "text/html",
    "text/plain": "text/plain",
    "application/octet-stream": "application/octet-stream",
}


def _content_type_label(raw: str) -> str:
    """把 Content-Type 归一成白名单标签；未知类型只报类别，不回显原值。"""

    base = raw.split(";")[0].strip().lower()
    if not base:
        return "(缺失)"
    return KNOWN_CONTENT_TYPES.get(base, "(非白名单类型)")


def _require_positive_finite(value: float, name: str) -> float:
    """拒绝 NaN/Inf/非正数；NaN 参与比较恒为 False，不显式拦截会被当成合法值。"""

    if not isinstance(value, (int, float)) or not math.isfinite(value) or value <= 0:
        raise ValueError(f"{name} 必须为大于 0 的有限数值")
    return float(value)


@dataclass(frozen=True)
class Result:
    """单条检查结果；detail 只放状态码、Content-Type、时间等安全信息。"""

    check: str
    ok: bool
    detail: str

    def to_json(self) -> str:
        return json.dumps(
            {"check": self.check, "ok": self.ok, "detail": self.detail},
            ensure_ascii=False,
        )


@dataclass(frozen=True)
class HttpObservation:
    code: int | None
    content_type: str
    body: str
    error: str | None


class _NoRedirect(urllib.request.HTTPRedirectHandler):
    """健康检查按原始状态码判定，不跟随重定向。"""

    def redirect_request(self, req, fp, code, msg, headers, newurl):
        return None


def _build_opener() -> urllib.request.OpenerDirector:
    # 显式空 ProxyHandler 忽略环境代理，避免本地检查被 HTTP(S)_PROXY 改道。
    return urllib.request.build_opener(urllib.request.ProxyHandler({}), _NoRedirect())


def validate_base_url(base_url: str) -> str:
    """只接受回环 http/https 目标，返回去掉尾斜杠的基地址。"""

    parsed = urllib.parse.urlsplit(base_url)
    if parsed.scheme not in ("http", "https"):
        raise ValueError("--base-url 只支持 http 或 https")
    if parsed.username or parsed.password:
        raise ValueError("--base-url 不得包含凭据")
    if parsed.query or parsed.fragment:
        raise ValueError("--base-url 不得包含查询串或片段")
    host = parsed.hostname
    if not host:
        raise ValueError("--base-url 缺少主机名")
    if parsed.path not in ("", "/"):
        raise ValueError("--base-url 不应包含路径，健康路径由工具拼接")
    if host != "localhost":
        try:
            address = ipaddress.ip_address(host)
        except ValueError:
            raise ValueError(f"仅允许回环目标，拒绝主机名 {host!r}") from None
        if not address.is_loopback:
            raise ValueError(f"仅允许回环目标，拒绝 {host}")
    return f"{parsed.scheme}://{parsed.netloc}"


def _describe_network_error(exc: object) -> str:
    text = str(exc).lower()
    if isinstance(exc, urllib.error.URLError):
        return _describe_network_error(exc.reason)
    if isinstance(exc, ConnectionRefusedError) or "refused" in text or "10061" in text:
        return "连接失败（拒绝连接）：应用或代理不可达"
    if "10013" in text or "permission" in text:
        return "连接失败（本机拒绝连接）"
    if isinstance(exc, TimeoutError) or "timed out" in text or "timeout" in text:
        return "请求超时"
    if isinstance(exc, ssl.SSLCertVerificationError) or "certificate verify failed" in text:
        return "TLS 证书校验失败"
    if isinstance(exc, ssl.SSLError) or "ssl" in text:
        return "TLS 握手失败"
    if isinstance(exc, socket.gaierror) or "getaddrinfo" in text:
        return "域名解析失败"
    if isinstance(exc, str):
        return "请求失败"
    return f"请求失败（{type(exc).__name__}）"


def _observation(code: int, content_type: str, raw: bytes) -> HttpObservation:
    if len(raw) > MAX_BODY_BYTES:
        return HttpObservation(code, content_type, "", f"响应体超过上限 {MAX_BODY_BYTES} 字节")
    return HttpObservation(code, content_type, raw.decode("utf-8", errors="replace"), None)


def _observe(opener: urllib.request.OpenerDirector, url: str, timeout: float) -> HttpObservation:
    try:
        with opener.open(url, timeout=timeout) as response:
            return _observation(
                response.status,
                response.headers.get("Content-Type", ""),
                response.read(MAX_BODY_BYTES + 1),
            )
    except urllib.error.HTTPError as exc:
        try:
            try:
                raw = exc.read(MAX_BODY_BYTES + 1) or b""
            except (OSError, http.client.HTTPException):
                raw = b""
            headers = exc.headers
            content_type = headers.get("Content-Type", "") if headers else ""
            return _observation(exc.code, content_type, raw)
        finally:
            # HTTPError 自身持有连接，必须关闭，避免测试与多次探针泄漏 socket。
            exc.close()
    except (urllib.error.URLError, OSError, http.client.HTTPException, ValueError) as exc:
        return HttpObservation(None, "", "", _describe_network_error(exc))


def _judge(
    check: str,
    observation: HttpObservation,
    expected_status: int,
    expected_value: str,
) -> Result:
    if observation.error is not None:
        return Result(check, False, observation.error)
    content_type = observation.content_type.split(";")[0].strip().lower()
    if observation.code != expected_status:
        hint = ""
        if observation.code in (502, 503, 504) and content_type != "application/json":
            hint = "（应用或代理不可用）"
        return Result(
            check,
            False,
            f"状态码 {observation.code}，预期 {expected_status}{hint}",
        )
    if content_type != "application/json":
        return Result(
            check,
            False,
            f"状态码 {observation.code}，Content-Type 为 {_content_type_label(observation.content_type)}；"
            "非 application/json，HTML/代理页面不算健康",
        )
    try:
        payload = json.loads(observation.body)
    except ValueError:
        return Result(check, False, "状态码与 Content-Type 正确，但响应体不是合法 JSON")
    if not isinstance(payload, dict):
        return Result(check, False, "JSON 顶层不是对象")
    value = payload.get("status")
    if value != expected_value:
        # 不回显实际取值：服务端可控内容不得进入证据。
        return Result(
            check,
            False,
            f"status 字段与预期不符（实际类型 {type(value).__name__}），预期 {expected_value!r}",
        )
    return Result(check, True, f"{observation.code} application/json status={expected_value}")


def probe_path(
    base_url: str,
    path: str,
    expected_status: int,
    expected_value: str,
    *,
    timeout: float = DEFAULT_TIMEOUT,
    check: str | None = None,
) -> Result:
    """对固定路径做一次严格判定，供 live/ready 与负向用例复用。"""

    name = check or f"health{path}"
    try:
        base = validate_base_url(base_url)
        timeout = _require_positive_finite(timeout, "--timeout")
    except ValueError as exc:
        return Result(name, False, str(exc))
    observation = _observe(_build_opener(), base + path, timeout)
    return _judge(name, observation, expected_status, expected_value)


def probe_health(
    base_url: str,
    kind: str,
    *,
    timeout: float = DEFAULT_TIMEOUT,
    expect: str = "ready",
) -> Result:
    if kind == "live":
        return probe_path(
            base_url, LIVE_PATH, 200, "alive", timeout=timeout, check="health live"
        )
    if kind == "ready":
        if expect not in ("ready", "not_ready"):
            raise ValueError("ready 期望值只接受 ready 或 not_ready")
        status = 200 if expect == "ready" else 503
        return probe_path(
            base_url, READY_PATH, status, expect, timeout=timeout, check="health ready"
        )
    raise ValueError("kind 只接受 live 或 ready")

def _parse_rfc3339(value: str) -> datetime:
    text = value.strip()
    if text.endswith(("Z", "z")):
        text = text[:-1] + "+00:00"
    parsed = datetime.fromisoformat(text)
    if parsed.tzinfo is None:
        raise ValueError("时间缺少时区")
    return parsed.astimezone(timezone.utc)


def _read_backup_status(status_path: Path) -> tuple[Result, datetime | None]:
    name = "backup.status"
    # 不回显文件名：路径由调用方提供，可能含主机目录或业务信息。
    try:
        present = status_path.is_file()
    except OSError:
        return Result(name, False, "备份状态文件无法访问"), None
    if not present:
        return Result(name, False, "备份状态文件不存在"), None
    try:
        raw = status_path.read_text(encoding="utf-8")
    except (OSError, UnicodeDecodeError):
        return Result(name, False, "备份状态文件无法按 UTF-8 读取"), None
    if len(raw.encode("utf-8")) > MAX_BODY_BYTES:
        return Result(name, False, "备份状态文件超过大小上限"), None
    try:
        payload = json.loads(raw)
    except ValueError:
        return Result(name, False, "备份状态文件不是合法 JSON"), None
    if not isinstance(payload, dict):
        return Result(name, False, "备份状态 JSON 顶层不是对象"), None
    if payload.get("ok") is not True:
        # 只报类型，不回显实际值，避免把状态文件内容写进证据。
        return (
            Result(
                name,
                False,
                f"上次备份未成功（ok 字段类型 {type(payload.get('ok')).__name__}，要求布尔 true）",
            ),
            None,
        )
    value = payload.get("last_success_utc")
    if not isinstance(value, str):
        return Result(name, False, "状态文件缺少字符串字段 last_success_utc"), None
    try:
        last = _parse_rfc3339(value)
    except ValueError:
        return Result(name, False, "last_success_utc 不是带时区的 RFC3339 时间"), None
    return Result(name, True, f"状态文件 ok，最后成功时间 {last.isoformat()}"), last


def judge_backup(
    backup_path: str | Path,
    max_age_hours: float,
    status_path: str | Path | None = None,
    *,
    now: datetime | None = None,
) -> list[Result]:
    """检查备份存在性与新鲜度；返回全部检查项，CLI 按任一失败判失败。"""

    try:
        max_age = _require_positive_finite(max_age_hours, "--max-age-hours")
    except ValueError as exc:
        return [Result("backup.max_age", False, str(exc))]
    current = now or datetime.now(timezone.utc)
    archive = Path(backup_path)
    # is_file 与 stat 之间文件可能被替换或删除；此处统一按失败处理而不是抛异常。
    try:
        info = archive.stat()
        if not statmod.S_ISREG(info.st_mode):
            return [Result("backup.archive", False, "备份路径不是普通文件")]
    except FileNotFoundError:
        return [Result("backup.archive", False, "备份文件不存在")]
    except OSError:
        return [Result("backup.archive", False, "备份文件无法访问")]
    if info.st_size == 0:
        return [Result("backup.archive", False, "备份文件为空（0 字节），不算有效备份")]
    results: list[Result] = [
        Result("backup.archive", True, f"备份文件存在（{info.st_size} 字节）")
    ]
    last: datetime | None = None
    source = "备份文件 mtime"
    if status_path is not None:
        status_result, last = _read_backup_status(Path(status_path))
        results.append(status_result)
        if not status_result.ok:
            return results
        source = "状态文件 last_success_utc"
    if last is None:
        last = datetime.fromtimestamp(info.st_mtime, tz=timezone.utc)
    age_hours = (current - last).total_seconds() / 3600
    if age_hours < 0:
        results.append(
            Result(
                "backup.freshness",
                False,
                f"最后成功时间 {last.isoformat()} 晚于当前时间，数据可疑（来源：{source}）",
            )
        )
    elif age_hours > max_age:
        results.append(
            Result(
                "backup.freshness",
                False,
                f"备份过期：最后成功 {last.isoformat()}，{age_hours:.2f} 小时前，"
                f"超过上限 {max_age:g} 小时（来源：{source}）",
            )
        )
    else:
        results.append(
            Result(
                "backup.freshness",
                True,
                f"备份未过期：最后成功 {last.isoformat()}，{age_hours:.2f} 小时前，"
                f"上限 {max_age:g} 小时（来源：{source}）",
            )
        )
    return results


def _emit(results: list[Result]) -> int:
    for result in results:
        print(result.to_json())
    failed = sum(1 for result in results if not result.ok)
    print(json.dumps({"summary": "failed" if failed else "ok", "failed": failed}, ensure_ascii=False))
    return 1 if failed else 0


def main(argv: list[str] | None = None) -> int:
    # 重定向到文件时固定 UTF-8，避免中文 detail 受控制台代码页影响。
    if hasattr(sys.stdout, "reconfigure"):
        try:
            sys.stdout.reconfigure(encoding="utf-8")
        except (OSError, ValueError):
            pass
    parser = argparse.ArgumentParser(
        prog="health_probe.py",
        description="T16-B1 最小健康与备份检查（只读、仅回环）",
    )
    subparsers = parser.add_subparsers(dest="command", required=True)
    health = subparsers.add_parser("health", help="检查 live/ready 状态码与 JSON")
    health.add_argument("--base-url", required=True, help="回环基地址，如 http://127.0.0.1:8080")
    health.add_argument("--timeout", type=float, default=DEFAULT_TIMEOUT)
    health.add_argument(
        "--expect-ready",
        choices=("ready", "not_ready"),
        default="ready",
        help="预期 ready 结果；not_ready 用于隔离故障演练核对 503 合同",
    )
    backup = subparsers.add_parser("backup", help="检查备份存在性与新鲜度")
    backup.add_argument("--path", required=True, help="备份归档文件路径")
    backup.add_argument("--max-age-hours", type=float, required=True, help="允许的最大年龄（小时）")
    backup.add_argument(
        "--status-file",
        help="可选状态 JSON：{ok: true, last_success_utc: RFC3339}，用于取最后成功时间",
    )
    args = parser.parse_args(argv)

    if args.command == "health":
        try:
            validate_base_url(args.base_url)
        except ValueError as exc:
            _emit([Result("health.target", False, str(exc))])
            return 2
        try:
            _require_positive_finite(args.timeout, "--timeout")
        except ValueError as exc:
            _emit([Result("health.timeout", False, str(exc))])
            return 2
        results = [
            probe_health(args.base_url, "live", timeout=args.timeout),
            probe_health(args.base_url, "ready", timeout=args.timeout, expect=args.expect_ready),
        ]
        return _emit(results)

    try:
        _require_positive_finite(args.max_age_hours, "--max-age-hours")
    except ValueError as exc:
        _emit([Result("backup.max_age", False, str(exc))])
        return 2
    return _emit(judge_backup(args.path, args.max_age_hours, args.status_file))


if __name__ == "__main__":
    raise SystemExit(main())
