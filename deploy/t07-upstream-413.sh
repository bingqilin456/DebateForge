#!/usr/bin/env bash
# Run on the authorized ECS host through Windows SSH. Existing stopped lab only.
set -Eeuo pipefail
umask 077

[[ $# == 5 ]] || { echo 'usage: t07-upstream-413.sh LAB_ROOT CA_FILE APP_FULL_ID EDGE_FULL_ID MYSQL_FULL_ID' >&2; exit 2; }
root=$(realpath -e -- "$1")
ca=$(realpath -e -- "$2")
[[ -f $ca && $3 =~ ^sha256:[a-f0-9]{64}$ && $4 =~ ^sha256:[a-f0-9]{64}$ && $5 =~ ^sha256:[a-f0-9]{64}$ ]]
cd -- "$root"
[[ -f deploy/compose.yaml && -f deploy/.env ]]
[[ -z ${DOCKER_HOST:-} && -z ${COMPOSE_FILE:-} ]]
docker=(docker --context default)
[[ $(docker context inspect default --format '{{.Endpoints.docker.Host}}') == unix:///var/run/docker.sock ]]
project=siyige-t07-lab
dc=("${docker[@]}" compose --project-directory "$root/deploy" --env-file "$root/deploy/.env" -f "$root/deploy/compose.yaml" -p "$project")

tmp=$(mktemp -d)
active=0
cleanup() {
  local original=$?
  trap - EXIT
  if (( active )); then
    # Restore the ordinary edge configuration while stopping external access first.
    "${dc[@]}" stop edge app mysql >/dev/null 2>&1 || original=1
    "${dc[@]}" create --no-deps --no-build --pull never --force-recreate edge >/dev/null 2>&1 || original=1
    for svc in edge app mysql; do
      local id
      id=$("${dc[@]}" ps -aq "$svc") || { original=1; continue; }
      [[ -n $id && $("${docker[@]}" inspect -f '{{.State.Running}}' "$id") == false ]] || original=1
    done
  fi
  rm -f -- "$tmp"/{config.json,nginx.conf,override.yml,body,headers,response,logs,app.log,edge.log}
  rmdir -- "$tmp" || original=1
  if (( original == 0 )); then
    echo 'PASS_STOPPED base edge restored, original mysql volume retained'
  else
    echo 'BLOCKED verify lab stopped and base edge restored before any restart' >&2
  fi
  exit "$original"
}
trap cleanup EXIT
trap 'exit 130' INT TERM

"${dc[@]}" config --format json > "$tmp/config.json"
site=$(python3 -I - "$tmp/config.json" <<'PY'
import json, re, sys
p=json.load(open(sys.argv[1]))
s=p["services"]
assert p["name"] == "siyige-t07-lab"
assert p["volumes"]["mysql-data"]["name"] == "siyige-t07-lab_mysql-data"
assert s["app"]["environment"]["DB_NAME"] == "siyige_deploy"
assert s["app"]["environment"]["AI_MODE"] == "fake"
assert s["mysql"]["environment"]["MYSQL_DATABASE"] == "siyige_deploy"
assert "ports" not in s["app"] and "ports" not in s["mysql"]
assert all(x["host_ip"]=="127.0.0.1" for x in s["edge"]["ports"])
assert p["networks"]["proxy"]["internal"] and p["networks"]["database"]["internal"]
host=s["edge"]["environment"]["SITE_HOST"]
assert re.fullmatch(r"[a-z0-9.-]+",host)
print(host)
PY
)
for svc in mysql app edge; do
  id=$("${dc[@]}" ps -aq "$svc")
  [[ $id =~ ^[0-9a-f]+$ ]]
  [[ $("${docker[@]}" inspect -f '{{index .Config.Labels "com.docker.compose.project"}} {{index .Config.Labels "com.docker.compose.service"}} {{.State.Running}}' "$id") == "$project $svc false" ]]
  case "$svc" in
    mysql)
      [[ $("${docker[@]}" inspect -f '{{range .Mounts}}{{if eq .Destination "/var/lib/mysql"}}{{.Name}}{{end}}{{end}}' "$id") == siyige-t07-lab_mysql-data ]]
      "${docker[@]}" inspect "$id" | python3 -I -c 'import json,sys; c=json.load(sys.stdin)[0]; e=dict(x.split("=",1) for x in c["Config"]["Env"]); assert e.get("MYSQL_DATABASE")=="siyige_deploy"; assert not c["HostConfig"]["PortBindings"] and not c["HostConfig"]["PublishAllPorts"]; assert set(c["NetworkSettings"]["Networks"])=={"siyige-t07-lab_database"}'
      [[ $("${docker[@]}" inspect -f '{{.Image}}' "$id") == "$5" ]]
      mysql=$("${docker[@]}" inspect -f '{{.Id}}' "$id")
      ;;
    app)
      [[ $("${docker[@]}" inspect -f '{{.Image}}' "$id") == "$3" ]]
      app=$("${docker[@]}" inspect -f '{{.Id}}' "$id")
      "${docker[@]}" inspect "$id" | python3 -I -c 'import json,sys; c=json.load(sys.stdin)[0]; e=dict(x.split("=",1) for x in c["Config"]["Env"]); assert e.get("AI_MODE")=="fake" and e.get("DB_NAME")=="siyige_deploy" and e.get("DB_ADDR")=="mysql:3306"; assert not c["HostConfig"]["PortBindings"] and not c["HostConfig"]["PublishAllPorts"]; assert set(c["NetworkSettings"]["Networks"])=={"siyige-t07-lab_proxy","siyige-t07-lab_database"}'
      ;;
    edge) [[ $("${docker[@]}" inspect -f '{{.Image}}' "$id") == "$4" ]]; edge=$id ;;
  esac
done
# Stopped containers need not have a live endpoint.
verify_network() {
  local net=$1 exact=$2
  shift 2
  "${docker[@]}" network inspect "${project}_$net" | python3 -I -c '
import json, sys
net, exact, *allowed = sys.argv[1:]
network = json.load(sys.stdin)[0]
assert network["Labels"]["com.docker.compose.project"] == "siyige-t07-lab"
assert network["Labels"]["com.docker.compose.network"] == net
assert network["Internal"] and network["Driver"] == "bridge"
members = set(network.get("Containers") or {})
assert members <= set(allowed)
assert exact != "yes" or members == set(allowed)
' "$net" "$exact" "$@"
}
verify_network proxy no "$app" "$("${docker[@]}" inspect -f '{{.Id}}' "$edge")"
verify_network database no "$app" "$mysql"
"${docker[@]}" volume inspect siyige-t07-lab_mysql-data | python3 -I -c '
import json, sys
volume = json.load(sys.stdin)[0]
assert volume["Labels"]["com.docker.compose.project"] == "siyige-t07-lab"
assert volume["Driver"] == "local" and volume["Scope"] == "local" and not volume.get("Options")
'
for svc in app edge mysql; do
  image=$(python3 -I -c 'import json,sys; print(json.load(open(sys.argv[1]))["services"][sys.argv[2]]["image"])' "$tmp/config.json" "$svc")
  case "$svc" in app) expected=$3 ;; edge) expected=$4 ;; mysql) expected=$5 ;; esac
  [[ $("${docker[@]}" image inspect -f '{{.Id}}' "$image") == "$expected" ]]
done

"${docker[@]}" cp "$edge:/etc/nginx/nginx.conf" "$tmp/nginx.conf"
[[ $(grep -c '^[[:space:]]*client_max_body_size 32k;' "$tmp/nginx.conf") == 1 ]]
sed -i 's/client_max_body_size 32k;/client_max_body_size 64k;/' "$tmp/nginx.conf"
chmod 0444 "$tmp/nginx.conf"
cat > "$tmp/override.yml" <<EOF
services:
  edge:
    volumes:
      - type: bind
        source: $tmp/nginx.conf
        target: /etc/nginx/nginx.conf
        read_only: true
        bind:
          create_host_path: false
EOF
probe=("${dc[@]}" -f "$tmp/override.yml")
"${probe[@]}" config --quiet

active=1
"${dc[@]}" start --wait mysql app >/dev/null
"${probe[@]}" up -d --no-deps --no-build --pull never --wait --wait-timeout 90 edge >/dev/null
edge=$("${probe[@]}" ps -q edge)
[[ $("${docker[@]}" inspect -f '{{.Image}}' "$edge") == "$4" ]]
verify_network proxy yes "$app" "$("${docker[@]}" inspect -f '{{.Id}}' "$edge")"
verify_network database yes "$app" "$mysql"
"${probe[@]}" exec -T edge nginx -t >/dev/null 2>&1

curl_tls=(curl -q --noproxy '*' --silent --show-error --http1.1 --max-time 20 --cacert "$ca" --resolve "$site:443:127.0.0.1")
"${curl_tls[@]}" --fail "https://$site/api/health/ready" > /dev/null
since=$(date -u +%Y-%m-%dT%H:%M:%SZ)
mark=T07_413_SYNTHETIC_$(date +%s)_$$
python3 -I - "$tmp/body" "$mark" <<'PY'
import sys
from pathlib import Path
body=b'{"username":"'+sys.argv[2].encode()+b'","password":"'
Path(sys.argv[1]).write_bytes(body + b"x" * (32769-len(body)-2) + b'"}')
PY
for encoding in fixed chunked; do
  transfer=()
  [[ $encoding != chunked ]] || transfer=(-H 'Transfer-Encoding: chunked')
  code=$("${curl_tls[@]}" -D "$tmp/headers" -o "$tmp/response" -w '%{http_code}' "${transfer[@]}" \
    -H 'Content-Type: application/json' -H "Origin: https://$site" -H "Cookie: probe=$mark" \
    -H "Authorization: Bearer $mark" -H "X-Request-ID: $mark" -H 'Expect:' \
    --data-binary "@$tmp/body" "https://$site/api/v1/auth/login?probe=$mark")
  [[ $code == 413 ]]
  rid=$(python3 -I - "$tmp/headers" "$tmp/response" "$mark" <<'PY'
import json, re, sys
from pathlib import Path
headers=dict((k.lower(),v.strip()) for k,v in (line.split(":",1) for line in Path(sys.argv[1]).read_text().splitlines() if ":" in line))
body=json.loads(Path(sys.argv[2]).read_bytes())
rid=headers["x-request-id"]
assert re.fullmatch(r"[0-9a-zA-Z-]{16,64}",rid) and rid != sys.argv[3]
assert headers["content-type"].lower().startswith("application/json")
assert headers["cache-control"]=="no-store"
assert body["request_id"]==rid and body["error"]["code"]=="request_too_large"
print(rid)
PY
  )
  "${dc[@]}" logs --no-color --no-log-prefix --since "$since" app > "$tmp/app.log" 2>&1
  "${probe[@]}" logs --no-color --no-log-prefix --since "$since" edge > "$tmp/edge.log" 2>&1
  python3 -I - "$tmp/app.log" "$tmp/edge.log" "$rid" "$mark" <<'PY'
import json, sys
from pathlib import Path
app,edge=[Path(x).read_text() for x in sys.argv[1:3]]
rid,marker=sys.argv[3:]
assert marker not in app and marker not in edge
assert "request_id="+rid not in edge
def obj(line):
    try: return json.loads(line)
    except ValueError: return {}
assert any(x.get("request_id")==rid and x.get("status")==413 for x in map(obj,app.splitlines()))
PY
  printf 'PASS_GO_413 framing=%s request_id=%s\n' "$encoding" "$rid"
done
