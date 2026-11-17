#requires -Version 7.2
$ErrorActionPreference = 'Stop'
$PSNativeCommandUseErrorActionPreference = $false

$repoRoot = [IO.Path]::GetFullPath((Join-Path $PSScriptRoot '../..'))
$demoUrl = 'http://127.0.0.1:18081'
$composeFile = Join-Path $PSScriptRoot 'compose.yaml'
$secretDirectory = Join-Path $repoRoot '.cache/p3-demo/secrets'
$env:P3_DEMO_SECRETS_DIR = [IO.Path]::GetFullPath($secretDirectory)
Set-Location -LiteralPath $repoRoot

function Invoke-Checked {
    param([Parameter(Mandatory)][string]$Program, [Parameter(Mandatory)][string[]]$Arguments)
    & $Program @Arguments
    if ($LASTEXITCODE -ne 0) {
        throw "命令失败（退出码 $LASTEXITCODE）：$Program $($Arguments -join ' ')"
    }
}

function Get-RunningServices {
    $lines = @(& docker --context desktop-linux ps --filter 'label=com.docker.compose.project=siyige-p3-demo' --filter status=running --format '{{.Label "com.docker.compose.service"}}' 2>$null)
    if ($LASTEXITCODE -ne 0) { throw '无法查询 Demo 容器状态，已停止启动。请检查 Docker Desktop。' }
    return @($lines | Where-Object { -not [string]::IsNullOrWhiteSpace($_) })
}

function Test-DemoPage {
    try {
        $response = Invoke-WebRequest -Uri "$demoUrl/api/health/live" -TimeoutSec 3
        return $response.StatusCode -eq 200
    } catch { return $false }
}

try {
    Write-Host '思弈阁本地 Demo 快捷启动' -ForegroundColor Cyan
    if (-not (Get-Command docker -ErrorAction SilentlyContinue)) { throw '未找到 Docker CLI。请安装并启动 Docker Desktop（Linux 容器模式）。' }
    $dockerOs = & docker --context desktop-linux info --format '{{.OSType}}'
    if ($LASTEXITCODE -ne 0) { throw 'Docker Desktop 尚未就绪。请启动 Docker Desktop 并切换到 Linux 容器模式。' }
    if ($dockerOs -ne 'linux') { throw "当前 Docker Engine 类型为 '$dockerOs'，Demo 需要 Linux 容器模式。" }

    $runningServices = Get-RunningServices
    if ($runningServices -contains 'app' -and $runningServices -contains 'edge') {
        if (Test-DemoPage) {
            Write-Host 'Demo 已在运行，直接打开页面。'
            Start-Process $demoUrl
            exit 0
        }
        throw '发现已有 Demo 容器正在运行，但页面健康检查未通过。为避免打断正在使用的人，本脚本没有重启或替换容器。请查看 infra/demo/README.md。'
    }
    if ($runningServices.Count -gt 0) {
        throw "检测到 Demo 环境部分服务正在运行（$($runningServices -join ', ')）。为避免打断现有体验，本脚本未执行构建、迁移或重启；请先由当前使用者确认环境状态。"
    }

    $secretFiles = @(@('root.txt', 'app.txt', 'migrator.txt') | Where-Object { Test-Path -LiteralPath (Join-Path $secretDirectory $_) })
    if ($secretFiles.Count -eq 0) {
        & docker --context desktop-linux volume inspect siyige-p3-demo_demo-data *> $null
        if ($LASTEXITCODE -eq 0) {
            throw '发现已有 Demo 数据卷，但本地秘密文件缺失。为避免与数据库账户口令不匹配，本脚本不会重新生成；请从安全备份恢复秘密文件后再启动。'
        }
    }

    Invoke-Checked pwsh @('-NoProfile', '-File', (Join-Path $PSScriptRoot 'init-secrets.ps1'))
    Invoke-Checked docker @('--context', 'desktop-linux', 'compose', '-f', $composeFile, 'config', '--quiet')

    Push-Location (Join-Path $repoRoot 'frontend')
    try {
        if (-not (Test-Path -LiteralPath 'node_modules')) {
            Invoke-Checked npm @('ci')
        }
        Invoke-Checked npm @('run', 'build')
    } finally { Pop-Location }

    Invoke-Checked docker @('--context', 'desktop-linux', 'compose', '-f', $composeFile, 'build', 'app', 'migrate')
    Invoke-Checked docker @('--context', 'desktop-linux', 'compose', '-f', $composeFile, 'up', '-d', '--wait', '--wait-timeout', '180', 'mysql')
    Invoke-Checked docker @('--context', 'desktop-linux', 'compose', '-f', $composeFile, 'run', '--rm', 'migrate', 'up')
    Invoke-Checked docker @('--context', 'desktop-linux', 'compose', '-f', $composeFile, 'exec', '-T', 'mysql', 'bash', '/opt/siyige/mysql-client.sh', 'root', '--execute=source /opt/siyige/grant-app.sql')
    Invoke-Checked docker @('--context', 'desktop-linux', 'compose', '-f', $composeFile, 'up', '-d', '--wait', '--wait-timeout', '90', 'app', 'edge')
    Invoke-Checked docker @('--context', 'desktop-linux', 'compose', '-f', $composeFile, 'exec', '-T', 'edge', 'nginx', '-t')

    if (-not (Test-DemoPage)) { throw "容器已启动，但页面健康检查失败。请检查 Demo 日志；页面地址：$demoUrl" }
    Start-Process $demoUrl
    Write-Host "页面已就绪：$demoUrl" -ForegroundColor Green
} catch {
    Write-Host "启动失败：$($_.Exception.Message)" -ForegroundColor Red
    exit 1
}
