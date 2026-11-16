#requires -Version 7.2
[CmdletBinding()]param()
$ErrorActionPreference='Stop'
$repoRoot=[IO.Path]::GetFullPath((Join-Path $PSScriptRoot '../..'))
$outputRoot=Join-Path $repoRoot '.cache/t15-local/nginx'
New-Item -ItemType Directory -Path $outputRoot -Force | Out-Null
$template=Get-Content -LiteralPath (Join-Path $repoRoot 'deploy/nginx/site.conf.template') -Raw
$start=$template.IndexOf('server {', $template.IndexOf('server {')+1)
$end=$template.IndexOf('# 仅供容器内检查 Nginx。')
if ($start -lt 0 -or $end -lt $start) {throw '无法识别受测站点模板'}
$site=$template.Substring($start,$end-$start).Replace('listen 8443 ssl;','listen 8080;').Replace('${SITE_HOST}','127.0.0.1')
$site=[regex]::Replace($site,'(?m)^\s*ssl_(certificate|certificate_key|protocols) .*\r?\n','')
$site=$site.Replace('include /etc/nginx/siyige/proxy-sse.conf;','include /etc/nginx/siyige/proxy-sse.conf;'+"`n        add_header X-T15-SSE-Route matched always;")
$config="pid /tmp/nginx.pid;`nerror_log stderr warn;`nevents {}`nhttp {`naccess_log off;`nlog_format api_413 'status=413';`nupstream siyige_api { server host.docker.internal:8080; }`n$site`n}`n"
[IO.File]::WriteAllText((Join-Path $outputRoot 'nginx.conf'),$config.Replace("`r`n","`n"),[Text.UTF8Encoding]::new($false))
$existing=docker --context desktop-linux ps -a --filter 'name=^/siyige-t15-local-nginx$' --format '{{.Names}}'
if ($LASTEXITCODE -ne 0) {throw 'Docker Desktop Linux context 不可用'}
if ($existing) {
    $owned=docker --context desktop-linux inspect siyige-t15-local-nginx --format '{{index .Config.Labels "siyige.scope"}}'
    $mountSource=docker --context desktop-linux inspect siyige-t15-local-nginx --format '{{range .Mounts}}{{if eq .Destination "/etc/nginx/nginx.conf"}}{{.Source}}{{end}}{{end}}'
    if ($owned -ne 't15-local' -or $mountSource.Replace('\','/') -ne "$outputRoot/nginx.conf".Replace('\','/')) {throw '同名容器标签或配置挂载不属于本次 worktree'}
    docker --context desktop-linux restart siyige-t15-local-nginx
} else {
    docker --context desktop-linux run --detach --name siyige-t15-local-nginx --label siyige.scope=t15-local --publish 127.0.0.1:18080:8080 --mount "type=bind,source=$outputRoot/nginx.conf,target=/etc/nginx/nginx.conf,readonly" --mount "type=bind,source=$repoRoot/deploy/nginx,target=/etc/nginx/siyige,readonly" --entrypoint nginx nginx:1.30.6-alpine-slim -g 'daemon off;'
}
if ($LASTEXITCODE -ne 0) {throw '本地 Nginx 启动失败'}
docker --context desktop-linux exec siyige-t15-local-nginx nginx -t
if ($LASTEXITCODE -ne 0) {throw 'Nginx 配置检查失败'}
Write-Output '本地代理：http://127.0.0.1:18080；停止：docker --context desktop-linux stop siyige-t15-local-nginx'
