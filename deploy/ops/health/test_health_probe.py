#!/usr/bin/env python3
"""T16-B1 最小检查工具的 unittest（只用标准库，不联网、不碰 Docker）。

覆盖范围仅限本步交付：健康端点的状态码与 JSON 合同（含 HTML 200 必须失败），
以及备份文件缺失/过期/状态文件异常。续期、回退、告警传输由后续步骤单独测试。
所有临时文件写入 TemporaryDirectory，服务只绑定 127.0.0.1 的临时端口。
"""

from __future__ import annotations

import json
import os
import sys
import tempfile
import threading
import unittest
from datetime import datetime, timedelta, timezone
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path
from unittest import mock

sys.path.insert(0, str(Path(__file__).resolve().parent.parent))

import health_probe  # noqa: E402

# 合成标记：用于证明细节里不回显服务端可控内容或调用方文件名。
ECHO_MARKER = "marker-7f3a-should-not-appear"


class _Handler(BaseHTTPRequestHandler):
    """按路由返回合成的健康响应，用于验证判定逻辑而非真实应用。"""

    iat = 0

    def log_message(self, format, *args):  # noqa: A002 - 屏蔽测试访问日志
        return

    def do_GET(self):  # noqa: N802 - http.server 约定
        routes = self.server.routes  # type: ignore[attr-defined]
        status, content_type, raw = routes.get(
            self.path,
            (404, "text/plain; charset=utf-8", b"not found"),
        )
        if callable(raw):
            raw = raw(self)
        body = raw if isinstance(raw, bytes) else raw.encode("utf-8")
        self.send_response(status)
        if content_type:
            self.send_header("Content-Type", content_type)
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)


def _configured_status(code: int, value: str):
    return code, "application/json; charset=utf-8", json.dumps({"status": value})


class HealthProbeHttpTest(unittest.TestCase):
    """用本地临时端口验证 live/ready 的判定；不依赖 Deploy Compose。"""

    routes: dict[str, tuple[int, str, object]] = {}

    @classmethod
    def setUpClass(cls):
        cls.server = ThreadingHTTPServer(("127.0.0.1", 0), _Handler)
        cls.server.routes = cls.available_routes()
        cls.thread = threading.Thread(target=cls.server.serve_forever, daemon=True)
        cls.thread.start()
        cls.base_url = f"http://127.0.0.1:{cls.server.server_address[1]}"

    @classmethod
    def tearDownClass(cls):
        cls.server.shutdown()
        cls.server.server_close()
        cls.thread.join(timeout=5)

    @classmethod
    def available_routes(cls):
        return {
            "/api/health/live": _configured_status(200, "alive"),
            "/api/health/ready": _configured_status(200, "ready"),
            "/api/health/ready-down": _configured_status(503, "not_ready"),
            "/api/health/html": (200, "text/html; charset=utf-8", "<!doctype html><title>app</title>"),
            "/api/health/live-text": (200, "text/plain; charset=utf-8", "alive"),
            "/api/health/live-empty": (200, "application/json", ""),
            "/api/health/live-broken": (200, "application/json", '{"status":'),
            "/api/health/live-wrong": _configured_status(200, "ready"),
            "/api/health/live-500": _configured_status(500, "alive"),
            "/api/health/ready-proxy-502": (502, "text/html", "<html>bad gateway</html>"),
            "/api/health/ready-proxy-json-503": _configured_status(503, "not_ready"),
            "/api/health/huge": (200, "application/json", b"{" + b" " * (70 * 1024) + b"}"),
            # 服务端可控的异常取值与未知类型，用来确认不会被原样回显。
            "/api/health/live-injected": (200, "application/json", json.dumps({"status": ECHO_MARKER})),
            "/api/health/weird-type": (
                200,
                f"application/x-{ECHO_MARKER}",
                json.dumps({"status": "alive"}),
            ),
        }

    def test_live_and_ready_pass_with_json_contract(self):
        results = [
            health_probe.probe_health(self.base_url, "live", timeout=5),
            health_probe.probe_health(self.base_url, "ready", timeout=5),
        ]
        self.assertTrue(all(result.ok for result in results), [r.detail for r in results])
        self.assertEqual(results[0].detail, "200 application/json status=alive")
        self.assertEqual(results[1].detail, "200 application/json status=ready")

    def test_html_200_must_fail(self):
        result = health_probe.probe_path(
            self.base_url, "/api/health/html", 200, "alive"
        )
        self.assertFalse(result.ok)
        self.assertIn("text/html", result.detail)
        self.assertIn("HTML", result.detail)

    def test_non_json_and_malformed_bodies_must_fail(self):
        for path, reason in (
            ("/api/health/live-text", "text/plain"),
            ("/api/health/live-empty", "不是合法 JSON"),
            ("/api/health/live-broken", "不是合法 JSON"),
            ("/api/health/live-wrong", "status"),
            ("/api/health/huge", "上限"),
        ):
            with self.subTest(path=path):
                result = health_probe.probe_path(self.base_url, path, 200, "alive")
                self.assertFalse(result.ok)
                self.assertIn(reason, result.detail)

    def test_status_mismatch_must_fail_even_with_correct_json(self):
        result = health_probe.probe_path(self.base_url, "/api/health/live-500", 200, "alive")
        self.assertFalse(result.ok)
        self.assertIn("状态码 500", result.detail)

    def test_ready_503_contract_for_db_fault(self):
        result = health_probe.probe_path(
            self.base_url, "/api/health/ready-down", 503, "not_ready"
        )
        self.assertTrue(result.ok, result.detail)

    def test_proxy_html_failure_is_distinguishable_from_app_json(self):
        proxy = health_probe.probe_path(
            self.base_url, "/api/health/ready-proxy-502", 200, "ready"
        )
        self.assertFalse(proxy.ok)
        self.assertIn("应用或代理不可用", proxy.detail)
        # 代理透传的 JSON 503 仍是结构化 not_ready，与 HTML 故障页可区分。
        passthrough = health_probe.probe_path(
            self.base_url, "/api/health/ready-proxy-json-503", 503, "not_ready"
        )
        self.assertTrue(passthrough.ok, passthrough.detail)

    def test_connection_failure_is_reported_not_raised(self):
        # 未监听端口应返回失败结果而不是抛异常；Windows 上可能是拒绝连接或超时。
        result = health_probe.probe_health("http://127.0.0.1:1", "live", timeout=1)
        self.assertFalse(result.ok)
        self.assertTrue(
            any(token in result.detail for token in ("连接失败", "请求超时", "请求失败")),
            result.detail,
        )
        self.assertNotIn("Traceback", result.detail)

    def test_target_must_be_loopback(self):
        for base in (
            "http://192.0.2.10:8080",
            "http://example.com:8080",
            "ftp://127.0.0.1:8080",
            "http://user:pass@127.0.0.1:8080",
        ):
            with self.subTest(base=base):
                with self.assertRaises(ValueError):
                    health_probe.validate_base_url(base)
        self.assertEqual(
            health_probe.validate_base_url("http://127.0.0.1:8080/"),
            "http://127.0.0.1:8080",
        )
        self.assertEqual(
            health_probe.validate_base_url("https://localhost:8443"),
            "https://localhost:8443",
        )

    def test_unknown_status_value_is_not_echoed(self):
        result = health_probe.probe_path(
            self.base_url, "/api/health/live-injected", 200, "alive"
        )
        self.assertFalse(result.ok)
        self.assertNotIn(ECHO_MARKER, result.detail)
        self.assertNotIn(ECHO_MARKER, result.to_json())
        self.assertIn("status 字段与预期不符", result.detail)

    def test_unknown_content_type_is_not_echoed(self):
        result = health_probe.probe_path(
            self.base_url, "/api/health/weird-type", 200, "alive"
        )
        self.assertFalse(result.ok)
        self.assertNotIn(ECHO_MARKER, result.detail)
        self.assertIn("(非白名单类型)", result.detail)
        # 白名单内的类型仍按类别给出，便于区分 HTML 与纯文本。
        html = health_probe.probe_path(self.base_url, "/api/health/html", 200, "alive")
        self.assertIn("text/html", html.detail)

    def test_nan_and_inf_timeout_rejected(self):
        for bad in (float("nan"), float("inf"), float("-inf"), 0, -1):
            with self.subTest(timeout=bad):
                result = health_probe.probe_health(self.base_url, "live", timeout=bad)
                self.assertFalse(result.ok)
                self.assertIn("有限数值", result.detail)
        for bad in ("nan", "inf", "-inf", "0"):
            with self.subTest(cli=bad):
                # argparse 会把 nan/inf 解析成 float；必须判为参数错误而不是当真。
                # 负值需用 = 形式，否则 argparse 会把它当成选项而提前退出。
                self.assertEqual(
                    health_probe.main(
                        ["health", "--base-url", self.base_url, f"--timeout={bad}"]
                    ),
                    2,
                )

    def test_main_exit_codes(self):
        self.assertEqual(health_probe.main(["health", "--base-url", self.base_url]), 0)
        self.assertEqual(
            health_probe.main(["health", "--base-url", "http://192.0.2.10:8080"]), 2
        )
        self.assertEqual(
            health_probe.main(
                [
                    "health",
                    "--base-url",
                    self.base_url,
                    "--expect-ready",
                    "not_ready",
                ]
            ),
            1,
        )


class BackupCheckTest(unittest.TestCase):
    """备份存在性/新鲜度判定；状态文件为可独立注入的本地夹具。"""

    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.dir = Path(self.tmp.name)
        self.archive = self.dir / "synthetic-backup.tar.gz"
        self.archive.write_bytes(b"synthetic")
        self.now = datetime(2026, 11, 20, 9, 30, 0, tzinfo=timezone.utc)

    def tearDown(self):
        self.tmp.cleanup()

    def _status(self, payload) -> Path:
        path = self.dir / "backup.status.json"
        path.write_text(json.dumps(payload), encoding="utf-8")
        return path

    def test_missing_archive_fails(self):
        results = health_probe.judge_backup(
            self.dir / "absent.tar.gz", 24, now=self.now
        )
        self.assertEqual([result.check for result in results], ["backup.archive"])
        self.assertFalse(results[0].ok)
        self.assertIn("不存在", results[0].detail)

    def test_fresh_archive_by_mtime_passes(self):
        results = health_probe.judge_backup(self.archive, 24, now=self.now)
        self.assertTrue(all(result.ok for result in results), [r.detail for r in results])
        self.assertEqual([result.check for result in results], ["backup.archive", "backup.freshness"])

    def test_stale_archive_by_mtime_fails(self):
        old = (self.now - timedelta(hours=30)).timestamp()
        os.utime(self.archive, (old, old))
        results = health_probe.judge_backup(self.archive, 24, now=self.now)
        self.assertTrue(results[0].ok)
        self.assertFalse(results[1].ok)
        self.assertIn("备份过期", results[1].detail)
        self.assertIn("30.", results[1].detail)

    def test_status_file_last_success_is_consumed(self):
        status = self._status(
            {"ok": True, "last_success_utc": "2026-11-20T08:30:00+08:00"}
        )
        results = health_probe.judge_backup(self.archive, 24, status, now=self.now)
        self.assertTrue(all(result.ok for result in results), [r.detail for r in results])
        freshness = results[-1]
        self.assertIn("2026-11-20T00:30:00+00:00", freshness.detail)
        self.assertIn("状态文件", freshness.detail)

    def test_status_file_expired_fails(self):
        status = self._status(
            {"ok": True, "last_success_utc": "2026-11-18T00:00:00Z"}
        )
        results = health_probe.judge_backup(self.archive, 24, status, now=self.now)
        self.assertFalse(results[-1].ok)
        self.assertIn("备份过期", results[-1].detail)

    def test_failed_last_run_fails_even_if_archive_fresh(self):
        status = self._status({"ok": False, "last_success_utc": "2026-11-20T09:00:00Z"})
        results = health_probe.judge_backup(self.archive, 24, status, now=self.now)
        self.assertEqual(len(results), 2)
        self.assertTrue(results[0].ok)
        self.assertFalse(results[1].ok)
        self.assertIn("未成功", results[1].detail)
        self.assertIn("bool", results[1].detail)

    def test_status_file_value_is_not_echoed(self):
        # ok 字段取值属于状态文件内容，不得进入证据；只报类型。
        status = self._status({"ok": ECHO_MARKER, "last_success_utc": "2026-11-20T09:00:00Z"})
        results = health_probe.judge_backup(self.archive, 24, status, now=self.now)
        self.assertFalse(results[1].ok)
        self.assertNotIn(ECHO_MARKER, results[1].detail)
        self.assertNotIn(ECHO_MARKER, results[1].to_json())

    def test_archive_and_status_file_names_are_not_echoed(self):
        # 调用方路径可能含主机目录或业务信息，detail 只描述检查结论。
        named = self.dir / f"{ECHO_MARKER}.tar.gz"
        named.write_bytes(b"synthetic")
        missing = self.dir / f"{ECHO_MARKER}-absent.tar.gz"
        for path, expected in ((named, "备份未过期"), (missing, "备份文件不存在")):
            with self.subTest(path=path.name):
                results = health_probe.judge_backup(path, 24, now=self.now)
                for result in results:
                    self.assertNotIn(ECHO_MARKER, result.to_json())
                    self.assertTrue(result.detail, "detail 不应为空")
                first = results[0]
                if first.ok:
                    self.assertIn(expected, results[-1].detail)
                else:
                    self.assertIn(expected, first.detail)

    def test_empty_archive_fails(self):
        empty = self.dir / "empty.tar.gz"
        empty.write_bytes(b"")
        results = health_probe.judge_backup(empty, 24, now=self.now)
        self.assertEqual([result.check for result in results], ["backup.archive"])
        self.assertFalse(results[0].ok)
        self.assertIn("为空", results[0].detail)

    def test_directory_is_not_treated_as_archive(self):
        results = health_probe.judge_backup(self.dir, 24, now=self.now)
        self.assertFalse(results[0].ok)
        self.assertIn("普通文件", results[0].detail)

    def test_nan_and_inf_max_age_rejected(self):
        for bad in (float("nan"), float("inf"), 0, -1):
            with self.subTest(max_age=bad):
                results = health_probe.judge_backup(self.archive, bad, now=self.now)
                self.assertEqual([result.check for result in results], ["backup.max_age"])
                self.assertFalse(results[0].ok)
        for bad in ("nan", "inf", "0"):
            with self.subTest(cli=bad):
                self.assertEqual(
                    health_probe.main(
                        ["backup", "--path", str(self.archive), "--max-age-hours", bad]
                    ),
                    2,
                )

    def test_stat_race_is_reported_not_raised(self):
        # is_file/stat 与后续读取之间文件可能被替换或删除，必须判失败而不是抛栈。
        with mock.patch.object(
            Path, "stat", side_effect=FileNotFoundError("synthetic race")
        ):
            results = health_probe.judge_backup(self.archive, 24, now=self.now)
        self.assertEqual([result.check for result in results], ["backup.archive"])
        self.assertFalse(results[0].ok)
        self.assertIn("不存在", results[0].detail)

        with mock.patch.object(Path, "stat", side_effect=PermissionError("synthetic")):
            results = health_probe.judge_backup(self.archive, 24, now=self.now)
        self.assertFalse(results[0].ok)
        self.assertIn("无法访问", results[0].detail)

    def test_status_file_contract_violations_fail(self):
        cases = {
            "missing-file": None,
            "not-json": "not-json",
            "top-level-array": [],
            "missing-time": {"ok": True},
            "naive-time": {"ok": True, "last_success_utc": "2026-11-20T09:00:00"},
            "bad-time": {"ok": True, "last_success_utc": "yesterday"},
        }
        for name, payload in cases.items():
            with self.subTest(case=name):
                if payload is None:
                    status_path = self.dir / "no-such-status.json"
                elif isinstance(payload, str):
                    status_path = self.dir / "bad.json"
                    status_path.write_text(payload, encoding="utf-8")
                else:
                    status_path = self._status(payload)
                results = health_probe.judge_backup(
                    self.archive, 24, status_path, now=self.now
                )
                self.assertTrue(results[0].ok)
                self.assertFalse(results[1].ok, results[1].detail)

    def test_future_timestamp_is_reported(self):
        status = self._status(
            {"ok": True, "last_success_utc": "2026-11-21T00:00:00Z"}
        )
        results = health_probe.judge_backup(self.archive, 24, status, now=self.now)
        self.assertFalse(results[-1].ok)
        self.assertIn("晚于当前时间", results[-1].detail)

    def test_main_backup_exit_codes(self):
        self.assertEqual(
            health_probe.main(
                ["backup", "--path", str(self.archive), "--max-age-hours", "24"]
            ),
            0,
        )
        self.assertEqual(
            health_probe.main(
                ["backup", "--path", str(self.dir / "absent"), "--max-age-hours", "24"]
            ),
            1,
        )
        self.assertEqual(
            health_probe.main(
                ["backup", "--path", str(self.archive), "--max-age-hours", "0"]
            ),
            2,
        )


if __name__ == "__main__":
    unittest.main(verbosity=2)
