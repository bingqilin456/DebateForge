"""仅对本任务独占 Fake 容器执行 HTTP；合成记录保留，不直写数据库。"""
import concurrent.futures
import http.client
import http.cookiejar
import json
from pathlib import Path
import secrets
import socket
import subprocess
import sys
import time
import urllib.error
import urllib.request

BASE = "http://127.0.0.1:18081"
PROJECT = "siyige-p3-a-strategy-fake"
STATE = Path(__file__).resolve().parents[2] / ".cache/p3-demo/secrets/strategy-probe.json"


def inspect(service):
    return json.loads(subprocess.check_output([
        "docker", "--context", "desktop-linux", "inspect", f"{PROJECT}-{service}-1"
    ]))[0]


def guard():
    app, db = inspect("app"), inspect("mysql")
    for info in (app, db):
        assert info["Config"]["Labels"]["com.docker.compose.project"] == PROJECT
    env = app["Config"]["Env"]
    assert all(x in env for x in ("APP_ENV=demo", "AI_MODE=fake", "DB_ADDR=mysql:3306",
                                 "DB_NAME=siyige_demo", "DB_USER=siyige_demo_app"))
    assert app["NetworkSettings"]["Ports"]["8080/tcp"] == [
        {"HostIp": "127.0.0.1", "HostPort": "18081"}]
    assert any(m.get("Name") == PROJECT + "_demo-data" and m["Destination"] == "/var/lib/mysql"
               for m in db["Mounts"])
    assert not any(db["NetworkSettings"]["Ports"].values()), "DB must not publish ports"
    return env


class Client:
    def __init__(self, credentials):
        self.jar = http.cookiejar.CookieJar()
        self.opener = urllib.request.build_opener(urllib.request.HTTPCookieProcessor(self.jar))
        self.csrf = ""
        code, data = self.call("/api/v1/auth/login", credentials)
        assert code == 200, (code, data)
        self.csrf = data["data"]["csrf_token"]
        self.uid = data["data"]["user"]["id"]

    def call(self, path, body=None):
        headers = {"Origin": BASE}
        if body is not None:
            headers.update({"Content-Type": "application/json", "X-CSRF-Token": self.csrf})
        request = urllib.request.Request(BASE + path, None if body is None else json.dumps(body).encode(), headers)
        try:
            response = self.opener.open(request, timeout=20)
        except urllib.error.HTTPError as error:
            response = error
        with response:
            return response.status, json.load(response)

    def create(self):
        code, data = self.call("/api/v1/debates", {"topic_id": "1", "user_stance": "pro", "ai_style": "logical"})
        assert code == 201, (code, data)
        return "/api/v1/debates/" + data["data"]["id"] + "/strategy"


def status(client, path, expected, version, body=None, http=200):
    code, data = client.call(path, body)
    assert code == http and data["data"]["status"] == expected and data["data"]["attempt_version"] == version, (code, data)
    return data["data"]


def wait_status(client, path, expected):
    deadline = time.monotonic() + 5
    while time.monotonic() < deadline:
        code, data = client.call(path)
        if code == 200 and data["data"]["status"] == expected:
            return data["data"]
        time.sleep(0.05)
    raise AssertionError(f"did not observe {expected}")


env = guard()
phase = sys.argv[1] if len(sys.argv) == 2 else ""
if phase == "prepare":
    assert not STATE.exists(), "已有账号状态；复用其 ID，不覆盖凭据或重置数据库"
    assert not any(x.startswith(("P3_STRATEGY_FAKE_FAILURE_USER_ID=", "P3_STRATEGY_FAKE_SLOW_USER_ID="))
                   and x.split("=", 1)[1] for x in env), "prepare requires ordinary Fake"
    accounts = {}
    for role in ("failure", "slow", "normal"):
        credentials = {"username": "sf" + secrets.token_hex(6), "password": secrets.token_hex(16)}
        request = urllib.request.Request(BASE + "/api/v1/auth/register", json.dumps(credentials).encode(),
                                         {"Origin": BASE, "Content-Type": "application/json"})
        with urllib.request.urlopen(request) as response:
            assert response.status == 201
        client = Client(credentials)
        accounts[role] = dict(credentials, id=client.uid)
    STATE.write_text(json.dumps(accounts), encoding="utf-8")
    print(json.dumps({"failure_id": accounts["failure"]["id"], "slow_id": accounts["slow"]["id"]}))
elif phase == "run":
    accounts = json.loads(STATE.read_text(encoding="utf-8"))
    assert f'P3_STRATEGY_FAKE_FAILURE_USER_ID={accounts["failure"]["id"]}' in env
    assert f'P3_STRATEGY_FAKE_SLOW_USER_ID={accounts["slow"]["id"]}' in env
    clients = {role: Client({k: v for k, v in account.items() if k != "id"})
               for role, account in accounts.items()}
    failure, slow, normal = (clients[x] for x in ("failure", "slow", "normal"))
    paths = []
    # 另一账号先成功不能消耗目标账号的失败；同一账号新会话可重现。
    status(normal, normal.create(), "completed", 1, {}, 201)
    for repeat in range(2):
        path = failure.create()
        paths.append(path)
        code, data = failure.call(path, {})
        assert code == 503 and data["error"]["code"] == "model_unavailable", (code, data)
        current = status(failure, path, "failed", 1)
        if repeat == 0:
            status(failure, path, "failed", 1, {})
            code, data = failure.call(path, {"retry": True, "expected_attempt_version": 2})
            assert code == 409 and data["error"]["code"] == "attempt_version_conflict"
        status(failure, path, "completed", 2,
               {"retry": True, "expected_attempt_version": current["attempt_version"]})
    path = slow.create()
    paths.append(path)
    with concurrent.futures.ThreadPoolExecutor(max_workers=1) as pool:
        start = time.monotonic()
        pending = pool.submit(slow.call, path, {})
        wait_status(slow, path, "generating")
        status(slow, path, "generating", 1, {}, 202)
        status(slow, path, "generating", 1, {"retry": True, "expected_attempt_version": 1}, 202)
        code, data = pending.result(timeout=15)
        assert code == 201 and data["data"]["status"] == "completed", (code, data)
        delay = time.monotonic() - start
        assert delay >= 8
    status(slow, path, "completed", 1)
    # 关闭真实 HTTP 连接；服务用 WithoutCancel cleanup 保存失败，然后显式重试。
    path = slow.create()
    paths.append(path)
    connection = http.client.HTTPConnection("127.0.0.1", 18081, timeout=20)
    cookies = "; ".join(f"{c.name}={c.value}" for c in slow.jar)
    connection.request("POST", path, b"{}", {"Origin": BASE, "Content-Type": "application/json",
                                           "X-CSRF-Token": slow.csrf, "Cookie": cookies})
    wait_status(slow, path, "generating")
    connection.sock.shutdown(socket.SHUT_RDWR)
    connection.close()
    current = wait_status(slow, path, "failed")
    assert current["error_code"] == "generation_cancelled", current
    status(slow, path, "completed", 2, {"retry": True, "expected_attempt_version": current["attempt_version"]})
    print(json.dumps({"result": "PASS", "target": PROJECT + "/mysql:3306/siyige_demo",
                      "checks": ["account_isolation", "per_debate_repeat", "persisted_failed", "explicit_retry",
                                 "stale_version", "generating", "duplicate_202", "completed_query", "http_cancel_retry"],
                      "slow_seconds": round(delay, 3), "paths": paths}))
else:
    raise SystemExit("usage: python infra/strategy-fake/probe.py prepare|run")
