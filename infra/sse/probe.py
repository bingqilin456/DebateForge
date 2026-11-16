"""固定本地 Fake 联调入口；标准库增量解析，绝不输出 Cookie、密码或 CSRF。"""
import argparse
import codecs
import http.cookiejar
import json
import secrets
import time
import urllib.error
import urllib.parse
import urllib.request
import uuid


class Decoder:
    def __init__(self):
        self.utf8 = codecs.getincrementaldecoder("utf-8")("strict")
        self.pending = ""
        self.event = ""
        self.data = []

    def feed(self, chunk, final=False):
        self.pending += self.utf8.decode(chunk, final)
        frames = []
        while "\n" in self.pending:
            line, self.pending = self.pending.split("\n", 1)
            line = line.removesuffix("\r")
            if not line:
                if self.data:
                    frames.append((self.event or "message", json.loads("\n".join(self.data))))
                self.event, self.data = "", []
            elif line.startswith(":"):
                frames.append(("heartbeat", None))
            else:
                key, sep, value = line.partition(":")
                if sep:
                    value = value.removeprefix(" ")
                    if key == "event":
                        self.event = value
                    elif key == "data":
                        self.data.append(value)
        # EOF 的半事件不发布；调用方在没有 done 时必须查询正式状态。
        return frames


def self_test():
    raw = ('event: delta\r\ndata: {"text":"中文🌱"}\r\n\r\n'
           ': keepalive\r\n\r\nevent: error\ndata: {"error":\n'
           'data: {"code":"model_timeout"}}\n\nevent: done\ndata: {"partial":').encode()
    for size in (1, 2, 3, 7, len(raw)):
        decoder, frames = Decoder(), []
        for i in range(0, len(raw), size):
            frames += decoder.feed(raw[i:i + size])
        frames += decoder.feed(b"", final=True)
        assert frames == [("delta", {"text": "中文🌱"}), ("heartbeat", None),
                          ("error", {"error": {"code": "model_timeout"}})], frames
    print("PASS UTF-8 / LF+CRLF / multiline data / heartbeat / half event")


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--base", default="http://127.0.0.1:8080")
    parser.add_argument("--origin", default="http://127.0.0.1:5173")
    parser.add_argument("--scenario", choices=("success", "failure", "timeout", "disconnect", "lost-done"), default="success")
    parser.add_argument("--require-incremental", action="store_true")
    parser.add_argument("--nginx", action="store_true")
    parser.add_argument("--require-heartbeat", action="store_true")
    parser.add_argument("--self-test", action="store_true")
    args = parser.parse_args()
    if args.self_test:
        self_test()
        return
    url = urllib.parse.urlsplit(args.base)
    if url.scheme != "http" or url.hostname != "127.0.0.1" or url.port not in (8080, 5173, 18080) or url.username or url.password or url.path not in ("", "/") or url.query or url.fragment:
        parser.error("只允许固定本地入口 127.0.0.1:8080/5173/18080，且后端须按说明使用 siyige_test + Fake")
    opener = urllib.request.build_opener(urllib.request.HTTPCookieProcessor(http.cookiejar.CookieJar()))
    csrf = ""

    def request(path, body=None, stream=False):
        headers = {"Origin": args.origin}
        if body is not None:
            headers.update({"Content-Type": "application/json", "X-CSRF-Token": csrf})
        if stream:
            headers["Accept"] = "text/event-stream"
        req = urllib.request.Request(args.base.rstrip("/") + path,
                                     None if body is None else json.dumps(body, ensure_ascii=False).encode(), headers)
        return opener.open(req, timeout=70)

    def api(path, body=None):
        with request(path, body) as response:
            return json.load(response)["data"]

    username, password = "t15" + secrets.token_hex(5), secrets.token_hex(16)
    api("/api/v1/auth/register", {"username": username, "password": password})
    login = api("/api/v1/auth/login", {"username": username, "password": password})
    csrf = login["csrf_token"]
    topics = api("/api/v1/topics")["items"]
    debate = api("/api/v1/debates", {"topic_id": topics[0]["id"], "user_stance": "pro"})
    path = "/api/v1/debates/" + debate["id"]
    key = str(uuid.uuid4())
    start = time.monotonic()
    frames, decoder, delta_times = [], Decoder(), []
    with request(path + "/turns/stream", {"round_no": 1, "content": "开放式考核需要统一评价标准。中文🌱", "client_request_id": key}, stream=True) as response:
        assert response.status == 200 and response.headers.get_content_type() == "text/event-stream"
        if args.nginx:
            assert response.headers.get("X-T15-SSE-Route") == "matched", "未命中实际模板的 SSE location"
        while chunk := response.read(1):
            for name, data in decoder.feed(chunk):
                frames.append((name, data))
                if name == "start":
                    assert set(data) == {"turn_id", "request_id"}
                    assert data["request_id"] == response.headers["X-Request-ID"]
                elif name == "delta":
                    delta_times.append(time.monotonic() - start)
                if args.scenario == "disconnect" and name == "delta":
                    break
            else:
                continue
            break
        decoder.feed(b"", final=True)
    names = [name for name, _ in frames]
    assert names and names[0] == "start", names
    if args.scenario in ("success", "lost-done"):
        assert names[-1] == "done" and "error" not in names, names
        if args.require_incremental:
            assert len(delta_times) >= 2 and delta_times[-1] - delta_times[0] > 0.1, delta_times
    elif args.scenario in ("failure", "timeout"):
        assert names[-1] == "error" and "done" not in names, names
        expected = "model_timeout" if args.scenario == "timeout" else "model_unavailable"
        assert frames[-1][1]["error"]["code"] == expected, frames[-1]
    if args.require_heartbeat:
        assert "heartbeat" in names, names
    # lost-done 模拟客户端丢弃已收到的最终事件，恢复只依赖详情查询。
    for _ in range(30):
        detail = api(path)
        turns = [turn for turn in detail["turns"] if turn["client_request_id"] == key]
        if turns and turns[0]["status"] != "generating":
            break
        time.sleep(0.1)
    assert len(turns) == 1, turns
    turn = turns[0]
    want = "completed" if args.scenario in ("success", "lost-done") else "failed"
    assert turn["status"] == want, turn["status"]
    assert len(turn["messages"]) == (2 if want == "completed" else 1)
    if want == "completed":
        text = "".join(data["text"] for name, data in frames if name == "delta")
        assert text == turn["messages"][1]["content"]
    with request("/api/v1/auth/logout", {}) as response:
        assert response.status == 204
    print(json.dumps({"result": "PASS", "base": args.base, "scenario": args.scenario,
                      "events": names, "first_delta_seconds": round(delta_times[0], 3) if delta_times else None,
                      "last_delta_seconds": round(delta_times[-1], 3) if delta_times else None,
                      "stored_status": turn["status"], "attempt_version": turn["attempt_version"]}, ensure_ascii=False))


if __name__ == "__main__":
    main()
