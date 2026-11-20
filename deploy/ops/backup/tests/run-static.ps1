#requires -Version 7.2
# T16A 静态/离线测试：不启动数据库、不写仓库、不创建或删除任何容器资源。覆盖 Bash 语法、--help、参数与
# 密钥形态拒绝、以及 age 加解密往返/截断/错误密钥。使用本机 Docker 镜像运行
# 容器内检查，不安装任何主机软件。
$ErrorActionPreference = 'Stop'
$PSNativeCommandUseErrorActionPreference = $false

$testsDir = $PSScriptRoot
$backupDir = [IO.Path]::GetFullPath((Join-Path $testsDir '..'))
$bash = 'D:\Program Files\Git\bin\bash.exe'
if (-not (Test-Path -LiteralPath $bash)) { $bash = (Get-Command bash -ErrorAction SilentlyContinue).Source }
if (-not $bash) { throw '未找到 bash（需要 Git for Windows 或同类 POSIX Shell）' }
$bashDir = Split-Path -Parent (Split-Path -Parent $bash)
$env:PATH = "$bashDir\bin;$bashDir\usr\bin;$env:PATH"

$script:results = [System.Collections.Generic.List[object]]::new()
function Add-Check {
    param([string]$Name, [bool]$Passed, [string]$Detail = '')
    $script:results.Add([pscustomobject]@{ name = $Name; passed = $Passed; detail = $Detail })
    $mark = if ($Passed) { 'PASS' } else { 'FAIL' }
    Write-Host ("[{0}] {1}{2}" -f $mark, $Name, $(if ($Detail) { " — $Detail" } else { '' })) `
        -ForegroundColor $(if ($Passed) { 'Green' } else { 'Red' })
}

function Invoke-Bash {
    param([string]$Script, [string[]]$Arguments = @())
    $output = & $bash -n -- "$Script" 2>&1
    return @{ code = $LASTEXITCODE; output = ($output -join ' ') }
}

function ConvertTo-PosixPath {
    param([string]$Path)
    $converted = & $bash -lc "cygpath -u '$($Path.Replace('\', '/'))'" 2>$null
    if ($LASTEXITCODE -ne 0 -or -not $converted) { return ($Path -replace '\\', '/') }
    return ($converted | Select-Object -First 1).Trim()
}

function Invoke-BashScript {
    param([string]$Script, [string[]]$Arguments = @())
    $output = & $bash "$Script" @Arguments 2>&1
    return @{ code = $LASTEXITCODE; text = ($output -join "`n") }
}

# 1. 文件存在与 LF
$required = @(
    'Dockerfile', 'compose.yaml', '.gitignore', 'README.md',
    'bin/t16a-lib.sh', 'bin/t16a-backup.sh', 'bin/t16a-restore.sh',
    'sql/client.sh', 'sql/dump.sh', 'sql/import.sh', 'sql/init-accounts.sh',
    'tests/age-roundtrip.sh', 'tests/run-static.ps1'
)
foreach ($rel in $required) {
    Add-Check "文件存在：$rel" (Test-Path -LiteralPath (Join-Path $backupDir $rel))
}

$shellFiles = @(Get-ChildItem -LiteralPath $backupDir -Recurse -File -Filter '*.sh' |
    Where-Object { $_.FullName -notmatch '[\\/]\.local[\\/]' })
foreach ($file in $shellFiles) {
    $rel = [IO.Path]::GetRelativePath($backupDir, $file.FullName).Replace('\', '/')
    $text = [IO.File]::ReadAllText($file.FullName)
    Add-Check "无 CRLF：$rel" (-not $text.Contains("`r`n"))
    Add-Check "严格模式：$rel" ($text -match '(?m)^set -euo pipefail$')
    Add-Check "可执行位：$rel" ($text -match '(?m)^#!/usr/bin/env bash')
    $syntax = Invoke-Bash -Script $file.FullName
    Add-Check "bash -n：$rel" ($syntax.code -eq 0) $syntax.output
}

# 2. --help 退出码为 0 且含用法
foreach ($name in @('t16a-backup.sh', 't16a-restore.sh')) {
    $result = Invoke-BashScript -Script (Join-Path $backupDir "bin/$name") -Arguments @('--help')
    Add-Check "--help 成功：$name" ($result.code -eq 0) $result.text
    Add-Check "--help 含用法：$name" ($result.text -match '用法')
}

# 3. 参数拒绝（不依赖 Docker 运行）
$tmp = Join-Path ([IO.Path]::GetTempPath()) ("t16a-offline-" + [guid]::NewGuid().ToString('N'))
New-Item -ItemType Directory -Path $tmp | Out-Null
try {
    $recipientPub = Join-Path $tmp 'recipient.pub'
    $recipientPriv = Join-Path $tmp 'identity.txt'
    # 用固定长度假值只验证形态判断，不产生真实密钥。
    Set-Content -LiteralPath $recipientPub -Value ('age1' + ('q' * 58)) -NoNewline
    $fakeSecretPrefix = 'AGE-SECRET-KEY-1'
    Set-Content -LiteralPath $recipientPriv -Value ($fakeSecretPrefix + 'FAKE') -NoNewline

    # 同目录拒绝：密文与私钥放在同一目录，脚本必须在解密前拒绝。
    $sameDirArtifact = Join-Path $tmp 'same.sql.gz.age'
    Set-Content -LiteralPath $sameDirArtifact -Value 'age-encryption.org/v1 dummy' -NoNewline
    # 摘要不匹配：产物齐全但 sha256 与密文不符，必须在连接数据库前拒绝。
    $artifactsDir = Join-Path $tmp 'artifacts'
    $keysDir = Join-Path $tmp 'keys'
    New-Item -ItemType Directory -Path $artifactsDir, $keysDir | Out-Null
    $artifact = Join-Path $artifactsDir 't16a-test.sql.gz.age'
    Set-Content -LiteralPath $artifact -Value 'age-encryption.org/v1 dummy payload' -NoNewline
    Set-Content -LiteralPath (Join-Path $artifactsDir 't16a-test.sql.gz.sha256') -Value (('0' * 64) + '  t16a-test.sql.gz.age') -NoNewline
    Set-Content -LiteralPath (Join-Path $artifactsDir 't16a-test.manifest.json') -Value '{"schema":"t16a-backup/v1","artifact":"t16a-test.sql.gz.age","source_project":"t16a-backup-source","source_db":"siyige_t16a_source"}' -NoNewline
    Set-Content -LiteralPath (Join-Path $keysDir 'identity.txt') -Value ($fakeSecretPrefix + 'FAKE') -NoNewline

    foreach ($name in @('recipientPub', 'recipientPriv', 'sameDirArtifact', 'artifact', 'keysDir')) {
        Set-Variable -Name $name -Value (ConvertTo-PosixPath (Get-Variable -Name $name -ValueOnly))
    }
    $artifactDirPosix = ConvertTo-PosixPath $artifactsDir
    $keysIdentityPosix = ConvertTo-PosixPath (Join-Path $keysDir 'identity.txt')
    $keysDirPosix = ConvertTo-PosixPath $keysDir

    # 固定 label 的旧产物必须在 Docker 检查前拒绝，且原内容保持不变。
    $collisionDir = Join-Path $tmp 'collision'
    New-Item -ItemType Directory -Path $collisionDir | Out-Null
    $collisionArtifact = Join-Path $collisionDir 't16a-fixed.sql.gz.age'
    Set-Content -LiteralPath $collisionArtifact -Value 'keep-existing' -NoNewline
    $collisionDirPosix = ConvertTo-PosixPath $collisionDir
    $collisionResult = Invoke-BashScript -Script (Join-Path $backupDir 'bin/t16a-backup.sh') -Arguments @(
        '--artifacts-dir', $collisionDirPosix, '--recipient-file', $recipientPub,
        '--label', 't16a-fixed'
    )
    $collisionKept = (Get-Content -LiteralPath $collisionArtifact -Raw) -eq 'keep-existing'
    Add-Check 'backup 拒绝覆盖既有产物' (
        $collisionResult.code -eq 1 -and $collisionResult.text -match '产物已存在' -and $collisionKept
    ) '旧产物内容保持不变'

    $cases = @(
        @{ name = 'backup 缺参数'; script = 'bin/t16a-backup.sh'; args = @(); expect = 2 },
        @{ name = 'backup 未知参数'; script = 'bin/t16a-backup.sh'; args = @('--bogus'); expect = 2 },
        @{ name = 'backup 相对路径拒绝'; script = 'bin/t16a-backup.sh'; args = @('--artifacts-dir', 'relative/dir', '--recipient-file', $recipientPub); expect = 1; pattern = '绝对路径' },
        @{ name = 'backup 拒绝私钥当 recipient'; script = 'bin/t16a-backup.sh'; args = @('--artifacts-dir', $artifactDirPosix, '--recipient-file', $recipientPriv); expect = 1; pattern = '含私钥' },
        @{ name = 'restore 缺参数'; script = 'bin/t16a-restore.sh'; args = @(); expect = 2 },
        @{ name = 'restore 拒绝非白名单项目'; script = 'bin/t16a-restore.sh'; args = @('--artifact', $artifact, '--identity-file', $recipientPriv, '--project', 'siyige-t02'); expect = 1; pattern = '项目名被拒绝' },
        @{ name = 'restore 拒绝源项目当目标'; script = 'bin/t16a-restore.sh'; args = @('--artifact', $artifact, '--identity-file', $recipientPriv, '--project', 't16a-backup-source'); expect = 1; pattern = '项目名被拒绝' },
        @{ name = 'restore 拒绝不存在的密文'; script = 'bin/t16a-restore.sh'; args = @('--artifact', ($artifactDirPosix + '/missing.sql.gz.age'), '--identity-file', $recipientPriv); expect = 1; pattern = '密文不存在' },
        @{ name = 'restore 拒绝私钥与密文同目录'; script = 'bin/t16a-restore.sh'; args = @('--artifact', $sameDirArtifact, '--identity-file', $recipientPriv); expect = 1; pattern = '密钥与密文分离' },
        @{ name = 'restore 拒绝摘要不匹配（副本损坏）'; script = 'bin/t16a-restore.sh'; args = @('--artifact', $artifact, '--identity-file', $keysIdentityPosix); expect = 1; pattern = '摘要不匹配' }
    )
    foreach ($case in $cases) {
        $result = Invoke-BashScript -Script (Join-Path $backupDir $case.script) -Arguments $case.args
        $patternOk = if ($case.ContainsKey('pattern')) { $result.text -match [regex]::Escape($case.pattern) } else { $true }
        $ok = ($result.code -eq $case.expect) -and ($result.text -notmatch 'BACKUP_OK|RESTORE_OK') -and $patternOk
        Add-Check $case.name $ok "exit=$($result.code) 期望=$($case.expect)；命中预期拒绝原因=$patternOk"
    }
}
finally {
    Remove-Item -LiteralPath $tmp -Recurse -Force -ErrorAction SilentlyContinue
}

# 4. age 往返/截断/错误密钥：在一次性 golang 容器内构建 age v1.2.1 后运行本目录脚本。
$mount = "$($backupDir):/src:ro"
$ageTest = & docker run --rm -v $mount --entrypoint bash golang:1.27.1 -c @'
set -e
export CGO_ENABLED=0 GOTOOLCHAIN=local GOBIN=/usr/local/bin
go install filippo.io/age/cmd/age@v1.2.1 filippo.io/age/cmd/age-keygen@v1.2.1 >/dev/null 2>&1
bash /src/tests/age-roundtrip.sh
'@ 2>&1
Add-Check 'age 加解密往返与拒绝用例' ($LASTEXITCODE -eq 0) ($ageTest -join ' ')

$failed = @($script:results | Where-Object { -not $_.passed })
Write-Host ''
Write-Host ("静态/离线测试：{0} 项通过，{1} 项失败" -f ($script:results.Count - $failed.Count), $failed.Count) `
    -ForegroundColor $(if ($failed.Count) { 'Red' } else { 'Green' })
if ($failed.Count -gt 0) {
    $failed | ForEach-Object { Write-Host ("  FAIL  {0} {1}" -f $_.name, $_.detail) -ForegroundColor Red }
    exit 1
}
exit 0
