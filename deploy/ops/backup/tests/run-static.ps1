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
    # 摘要文件名必须与备份脚本的发布名一致（<label>.sha256）；这里刻意造出摘要不匹配。
    Set-Content -LiteralPath (Join-Path $artifactsDir 't16a-test.sha256') -Value (('0' * 64) + '  t16a-test.sql.gz.age') -NoNewline
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

    # age-keygen 默认输出 3 行（# created、# public key、AGE-SECRET-KEY-1）。
    # 私钥校验必须只取私钥行；若把注释行也拼进来，真实生成的密钥文件会被误拒。
    $multilineKeysDir = Join-Path $tmp 'keys-multiline'
    New-Item -ItemType Directory -Path $multilineKeysDir | Out-Null
    $multilineIdentity = Join-Path $multilineKeysDir 'identity.txt'
    Set-Content -LiteralPath $multilineIdentity -Value @(
        '# created: 2026-01-01T00:00:00Z',
        ('# public key: age1' + ('q' * 58)),
        ($fakeSecretPrefix + 'FAKE')
    )
    $multilineIdentityPosix = ConvertTo-PosixPath $multilineIdentity

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
        @{ name = 'restore 拒绝摘要不匹配（副本损坏）'; script = 'bin/t16a-restore.sh'; args = @('--artifact', $artifact, '--identity-file', $keysIdentityPosix); expect = 1; pattern = '摘要不匹配' },
        # 多行私钥文件（含 age-keygen 注释行）必须通过私钥形态校验，只允许在后面的摘要校验处失败。
        @{ name = 'restore 接受 age-keygen 多行私钥文件'; script = 'bin/t16a-restore.sh'; args = @('--artifact', $artifact, '--identity-file', $multilineIdentityPosix); expect = 1; pattern = '摘要不匹配' }
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
if (-not (Get-Command docker -ErrorAction SilentlyContinue)) {
    Add-Check 'age 加解密往返与拒绝用例' $false '未找到 Docker CLI，未运行容器内 age 测试'
} else {
    $ageTest = & docker run --rm -v $mount --entrypoint bash golang:1.27.1 -c @'
set -e
export CGO_ENABLED=0 GOTOOLCHAIN=local GOBIN=/usr/local/bin
go install filippo.io/age/cmd/age@v1.2.1 filippo.io/age/cmd/age-keygen@v1.2.1 >/dev/null 2>&1
bash /src/tests/age-roundtrip.sh
'@ 2>&1
    Add-Check 'age 加解密往返与拒绝用例' ($LASTEXITCODE -eq 0) ($ageTest -join ' ')
}

# 5. 暂存目录必须带进程号（$$）：同一 label 的并发备份不能共用暂存目录，
#    否则两者会互相删除明文载荷或错发产物。断言源码形态 + 实际展开值。
$libText = [IO.File]::ReadAllText((Join-Path $backupDir 'bin/t16a-lib.sh'))
$backupText = [IO.File]::ReadAllText((Join-Path $backupDir 'bin/t16a-backup.sh'))
Add-Check 'lib 声明 staging_dir 且用 $$ 取进程号' (
    $libText -match '(?m)^staging_dir\(\) \{' -and $libText.Contains('"$$"')
) '缺少 staging_dir 或 $$'
Add-Check 'backup 用 staging_dir 生成暂存目录' (
    $backupText.Contains('staging="$(staging_dir "$artifacts_dir" "$label")"')
) '未改用 staging_dir'
Add-Check 'backup 不再使用单 $ 暂存目录' (
    -not $backupText.Contains('.staging.$label.$"')
) '仍存在 .staging.$label.$ 形态'
$libPosix = ConvertTo-PosixPath (Join-Path $backupDir 'bin/t16a-lib.sh')
$probeA = (& $bash -c '. "$1"; staging_dir /abs t16a-probe' _ $libPosix 2>&1) -join ''
$probeB = (& $bash -c '. "$1"; staging_dir /abs t16a-probe' _ $libPosix 2>&1) -join ''
Add-Check '暂存目录含数字进程号' ($probeA -match '^/abs/\.staging\.t16a-probe\.\d+$') $probeA
Add-Check '两个备份进程暂存目录不同' ($probeA -ne $probeB) "$probeA / $probeB"

# 6. “未发布宿主端口”判定必须看 HostConfig.PortBindings。docker ps 的 {{.Ports}} 对仅
#    EXPOSE 的端口（标准 mysql 镜像的 3306/33060）也会打印 "3306/tcp"，据此判空会让真实
#    备份在未发布任何端口的容器上被误拒，因此这里断言源码口径 + 归一化函数的行为。
Add-Check 'lib 声明 normalize_published_ports' (
    $libText -match '(?m)^normalize_published_ports\(\) \{'
) '缺少归一化函数'
Add-Check '端口断言使用 HostConfig.PortBindings' (
    $libText.Contains('HostConfig.PortBindings') -and -not $libText.Contains("--format '{{.Ports}}'")
) '仍按 docker ps {{.Ports}} 判定'
$normEmpty = (& $bash -c '. "$1"; printf "  \n\n" | normalize_published_ports' _ $libPosix 2>&1) -join ''
$normList = (& $bash -c '. "$1"; printf "3306/tcp\n 33060/tcp " | normalize_published_ports' _ $libPosix 2>&1) -join ''
Add-Check '端口归一化折叠空白为空' ($normEmpty -eq '') "得到 [$normEmpty]"
Add-Check '端口归一化保留端口列表' ($normList -eq '3306/tcp 33060/tcp') "得到 [$normList]"

# 7. 恢复端查找的摘要/清单文件名必须与备份端发布的名字一致，否则真实恢复必然在
#    “缺少摘要文件”处被拒（备份发布 <label>.sha256，恢复曾按 <label>.sql.gz.sha256 查找）。
$restoreText = [IO.File]::ReadAllText((Join-Path $backupDir 'bin/t16a-restore.sh'))
Add-Check '恢复端摘要文件名与备份发布名一致' (
    $restoreText.Contains('sha_file="${artifact%.sql.gz.age}.sha256"') -and
    $backupText.Contains('"$artifacts_dir/$label.sha256"')
) '摘要文件名口径不一致'

# 8. 端口断言必须 fail-closed：docker ps / docker inspect 查询失败时，不能把失败当成
#    “没有已发布端口”而放行，否则 Docker 不可用时备份会照跑，隔离保证建立在错误状态上。
#    这里用 shell 函数覆盖 docker 命令，四种模式都不需要真实 Docker。
$failClosedProbe = @'
set -u
lib="$1"; mode="$2"
. "$lib"
docker() {
    case "$mode:$1" in
        psfail:ps)           return 3 ;;
        inspectfail:ps)      printf 'probe-container\n'; return 0 ;;
        inspectfail:inspect) return 4 ;;
        ok:ps)               printf 'probe-container\n'; return 0 ;;
        ok:inspect)          return 0 ;;
        bound:ps)            printf 'probe-container\n'; return 0 ;;
        bound:inspect)       printf '3306/tcp\n'; return 0 ;;
        *)                   return 1 ;;
    esac
}
assert_no_published_ports t16a-backup-source
printf 'ALLOWED\n'
'@
$probeFile = Join-Path ([IO.Path]::GetTempPath()) ("t16a-failclosed-" + [guid]::NewGuid().ToString('N') + '.sh')
try {
    [IO.File]::WriteAllText($probeFile, (($failClosedProbe -replace "`r`n", "`n") + "`n"))
    $probeFilePosix = ConvertTo-PosixPath $probeFile
    function Invoke-PortProbe {
        param([string]$Mode)
        $text = (& $bash $probeFilePosix $libPosix $Mode 2>&1) -join "`n"
        return @{ code = $LASTEXITCODE; text = $text }
    }
    $psFail = Invoke-PortProbe 'psfail'
    Add-Check '端口断言在 docker ps 失败时拒绝' (
        $psFail.code -eq 1 -and $psFail.text -match 'docker ps 失败' -and $psFail.text -notmatch 'ALLOWED'
    ) "exit=$($psFail.code) 期望=1"
    $inspectFail = Invoke-PortProbe 'inspectfail'
    Add-Check '端口断言在 docker inspect 失败时拒绝' (
        $inspectFail.code -eq 1 -and $inspectFail.text -match 'docker inspect 失败' -and $inspectFail.text -notmatch 'ALLOWED'
    ) "exit=$($inspectFail.code) 期望=1"
    $okProbe = Invoke-PortProbe 'ok'
    Add-Check '端口断言在无绑定端口时放行' (
        $okProbe.code -eq 0 -and $okProbe.text -match 'ALLOWED'
    ) "exit=$($okProbe.code) 期望=0"
    $boundProbe = Invoke-PortProbe 'bound'
    Add-Check '端口断言在存在绑定端口时拒绝' (
        $boundProbe.code -eq 1 -and $boundProbe.text -match '已发布宿主端口' -and $boundProbe.text -notmatch 'ALLOWED'
    ) "exit=$($boundProbe.code) 期望=1"
}
finally {
    Remove-Item -LiteralPath $probeFile -Force -ErrorAction SilentlyContinue
}

$failed = @($script:results | Where-Object { -not $_.passed })
Write-Host ''
Write-Host ("静态/离线测试：{0} 项通过，{1} 项失败" -f ($script:results.Count - $failed.Count), $failed.Count) `
    -ForegroundColor $(if ($failed.Count) { 'Red' } else { 'Green' })
if ($failed.Count -gt 0) {
    $failed | ForEach-Object { Write-Host ("  FAIL  {0} {1}" -f $_.name, $_.detail) -ForegroundColor Red }
    exit 1
}
exit 0
