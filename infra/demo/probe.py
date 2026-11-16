"""仅在专用容器明确为 Fake 时创建合成记录；不停止、重启、迁移或清理数据库。"""
import http.cookiejar, json, secrets, time, urllib.request, urllib.error, uuid, sys, subprocess
from pathlib import Path
sys.dont_write_bytecode = True
sys.path.insert(0, str(Path(__file__).resolve().parents[2] / 'infra' / 'sse'))
from probe import Decoder
env = json.loads(subprocess.check_output(['docker', '--context', 'desktop-linux', 'inspect', 'siyige-p3-demo-app-1', '--format', '{{json .Config.Env}}']))
if 'AI_MODE=fake' not in env or 'APP_ENV=demo' not in env:
    raise SystemExit('拒绝：固定 Demo 容器必须明确为 demo + fake，未发送任何 API 请求')
base = 'http://127.0.0.1:18081'
opener = urllib.request.build_opener(urllib.request.HTTPCookieProcessor(http.cookiejar.CookieJar()))
csrf = ''

def request(path, body=None, stream=False):
    headers = {'Origin': base}
    if body is not None:
        headers.update({'Content-Type': 'application/json', 'X-CSRF-Token': csrf})
    if stream:
        headers['Accept'] = 'text/event-stream'
    return opener.open(urllib.request.Request(base + path, None if body is None else json.dumps(body).encode(), headers), timeout=70)

def api(path, body=None):
    with request(path, body) as r:
        return json.load(r)['data']
username = 'demo' + secrets.token_hex(5)
password = secrets.token_hex(16)
api('/api/v1/auth/register', {'username': username, 'password': password})
csrf = api('/api/v1/auth/login', {'username': username, 'password': password})['csrf_token']
assert api('/api/v1/debates')['items'] == []
topics = api('/api/v1/topics')['items']
debates = []
for style in ['logical', 'aggressive', 'empathic', 'data']:
    d = api('/api/v1/debates', {'topic_id': topics[0]['id'], 'user_stance': 'pro', 'ai_style': style})
    assert d['ai_style'] == style and d['mode'] == 'standard_4'
    debates.append(d)
for page in [1, 2]:
    assert len(api(f'/api/v1/debates?page={page}&page_size=2')['items']) == 2
assert api('/api/v1/debates?page=3&page_size=2')['items'] == []
for url, body, code in [('/api/v1/debates?page=0', None, 400), ('/api/v1/debates', {'topic_id': topics[0]['id'], 'user_stance': 'pro', 'ai_style': 'LOGICAL'}, 400)]:
    try:
        request(url, body)
        raise AssertionError('invalid accepted')
    except urllib.error.HTTPError as e:
        assert e.code == code
d = debates[0]
path = '/api/v1/debates/' + d['id']
assert api(path + '/strategy')['status'] == 'not_started'
assert api(path + '/strategy', {})['status'] == 'completed'
timings = []
for round_no in range(1, 5):
    key = str(uuid.uuid4())
    decoder = Decoder()
    frames = []
    delta_times = []
    started = time.monotonic()
    with request(path + '/turns/stream', {'round_no': round_no, 'content': '合成中文论点🌱', 'client_request_id': key}, True) as r:
        assert r.headers.get_content_type() == 'text/event-stream'
        while (chunk := r.read(1)):
            for name, data in decoder.feed(chunk):
                frames.append((name, data))
                if name == 'delta':
                    delta_times.append(time.monotonic() - started)
    names = [n for n, _ in frames]
    assert names[0] == 'start' and names[-1] == 'done' and ('error' not in names)
    assert len(delta_times) > 1 and delta_times[-1] - delta_times[0] > 0.1
    turn = api(path)['turns'][-1]
    assert turn['client_request_id'] == key and turn['status'] == 'completed'
    assert ''.join((v['text'] for n, v in frames if n == 'delta')) == turn['messages'][1]['content']
    timings.append([round(delta_times[0], 3), round(delta_times[-1], 3)])
assert api(path + '/summary', {})['status'] == 'completed'
with request('/api/v1/auth/logout', {}) as r:
    assert r.status == 204
csrf = api('/api/v1/auth/login', {'username': username, 'password': password})['csrf_token']
detail = api(path)
assert len(detail['turns']) == 4 and detail['summary_status']['status'] == 'completed'
assert api(path + '/strategy')['status'] == 'completed'
assert api('/api/v1/debates')['total'] == 4
with request('/api/v1/auth/logout', {}) as r:
    assert r.status == 204
print(json.dumps({'result': 'PASS', 'mode': 'fake', 'target': 'siyige_demo', 'checks': ['four_styles', 'empty_history', 'pagination', 'invalid_page_style', 'strategy', 'four_round_sse_incremental', 'summary', 'relogin_history'], 'delta_seconds': timings}))
