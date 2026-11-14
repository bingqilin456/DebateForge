#requires -Version 7.2
[CmdletBinding()]
param(
    [Parameter(Mandatory)][string]$PasswordFile,
    [ValidateSet('slow','failure','timeout','heartbeat')][string]$Scenario='slow'
)
$ErrorActionPreference='Stop'
$repoRoot=[IO.Path]::GetFullPath((Join-Path $PSScriptRoot '../..'))
if (-not [IO.Path]::IsPathFullyQualified($PasswordFile) -or -not (Test-Path -LiteralPath $PasswordFile -PathType Leaf)) { throw '请提供固定测试库 app 密码文件的绝对路径' }
if (Get-NetTCPConnection -State Listen -LocalPort 8080 -ErrorAction SilentlyContinue) { throw '8080 已占用，请先确认既有服务，脚本不会替换它' }
$outputRoot=Join-Path $repoRoot '.cache/t15-local'
New-Item -ItemType Directory -Path $outputRoot -Force | Out-Null
Push-Location (Join-Path $repoRoot 'backend')
try {
    $env:AI_MODE='disabled'
    Remove-Item Env:SIYIGE_INTEGRATION,Env:SIYIGE_MIGRATION_TEST -ErrorAction SilentlyContinue
    $executable=Join-Path $outputRoot 'server.exe'
    go build -o $executable ./cmd/server
    if ($LASTEXITCODE -ne 0) {throw '后端构建失败'}
    $env:APP_ENV='test';$env:AI_MODE='fake';$env:T15_FAKE_SCENARIO=$Scenario
    $env:HTTP_ADDR='127.0.0.1:8080';$env:AUTH_ORIGIN='http://127.0.0.1:5173'
    $env:DB_ADDR='127.0.0.1:13307';$env:DB_NAME='siyige_test';$env:DB_USER='siyige_test_app';$env:DB_PASSWORD_FILE=$PasswordFile
    $serverProcess=Start-Process -FilePath $executable -WorkingDirectory (Get-Location).Path -WindowStyle Hidden -PassThru -RedirectStandardOutput (Join-Path $outputRoot 'server.log') -RedirectStandardError (Join-Path $outputRoot 'server-error.log')
    $serverProcess.Id | Set-Content -LiteralPath (Join-Path $outputRoot 'server.pid')
    for ($attempt=0;$attempt -lt 30;$attempt++) {
        if ($serverProcess.HasExited) {throw '后端已退出，请查看本地日志'}
        try { $ready=Invoke-WebRequest 'http://127.0.0.1:8080/api/health/ready' -TimeoutSec 1; if ($ready.StatusCode -eq 200) {break} } catch {}
        Start-Sleep -Milliseconds 200
    }
    if ($attempt -eq 30) {throw '后端未在期限内 ready，请检查固定隔离库'}
    Write-Output "Fake=$Scenario PID=$($serverProcess.Id) API=http://127.0.0.1:8080；停止：Stop-Process -Id $($serverProcess.Id)"
} finally {Pop-Location}
