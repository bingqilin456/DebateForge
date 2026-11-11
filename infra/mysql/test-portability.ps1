#requires -Version 7.2
$ErrorActionPreference = 'Stop'
$PSNativeCommandUseErrorActionPreference = $false
$repoRoot = [IO.Path]::GetFullPath((Join-Path $PSScriptRoot '../..'))
$pwshPath = (Get-Process -Id $PID).Path
$failures = 0

function Test-That([bool]$Condition, [string]$Message) {
    if (-not $Condition) { throw $Message }
}

function Invoke-Case([string]$CaseName, [scriptblock]$Body) {
    try {
        & $Body
        Write-Output "PASS $CaseName"
    } catch {
        $script:failures++
        Write-Output "FAIL $CaseName : $($_.Exception.Message)"
    }
}

# 只在忽略目录的新仓库建立临时索引，保留检出样本；不改项目索引或已有文件。
foreach ($name in @('GIT_DIR', 'GIT_WORK_TREE', 'GIT_INDEX_FILE')) {
    Test-That (-not [Environment]::GetEnvironmentVariable($name)) "请先在干净终端清除 $name"
}
$fixture = Join-Path $repoRoot ('.cache/t02-mysql/p1-portability/' + [Guid]::NewGuid().ToString('N'))
$null = New-Item -ItemType Directory -Path $fixture -Force
& git -C $fixture init --quiet
Test-That ($LASTEXITCODE -eq 0) '临时 Git 初始化失败'
& git -C $fixture config core.autocrlf true
Test-That ($LASTEXITCODE -eq 0) '临时 Git 配置失败'
$shellFiles = @('infra/mysql/init-accounts.sh', 'infra/mysql/mysql-client.sh')
$paths = @($shellFiles)
if (Test-Path -LiteralPath (Join-Path $repoRoot '.gitattributes')) {
    Copy-Item -LiteralPath (Join-Path $repoRoot '.gitattributes') -Destination $fixture
    $paths = @('.gitattributes') + $paths
}
foreach ($path in $paths) {
    $blob = & git -C $fixture hash-object -w --no-filters -- (Join-Path $repoRoot $path)
    Test-That ($LASTEXITCODE -eq 0) '临时 Git 对象写入失败'
    & git -C $fixture update-index --add --cacheinfo "100644,$blob,$path"
    Test-That ($LASTEXITCODE -eq 0) '临时 Git 索引写入失败'
}
& git -C $fixture checkout-index --all --force
Test-That ($LASTEXITCODE -eq 0) '临时 Git 检出失败'
$gitExecutable = Get-Command git.exe -CommandType Application | Select-Object -First 1
$gitRoot = Split-Path (Split-Path $gitExecutable.Source)
$bashPath = Join-Path $gitRoot 'bin/bash.exe'
Test-That (Test-Path -LiteralPath $bashPath) '需要 Git for Windows 自带的 Bash'
Write-Output "CHECKOUT $fixture (core.autocrlf=true)"
foreach ($path in $shellFiles) {
    Invoke-Case "LF checkout and bash syntax: $path" {
        $file = Join-Path $fixture $path
        $bytes = [IO.File]::ReadAllBytes($file)
        $cr = @($bytes | Where-Object { $_ -eq 13 }).Count
        $lf = @($bytes | Where-Object { $_ -eq 10 }).Count
        $syntax = & $bashPath -n $file.Replace('\', '/') 2>&1
        $status = $LASTEXITCODE
        Write-Output "BYTES $path : CR=$cr LF=$lf bash_exit=$status"
        Test-That ($cr -eq 0 -and $lf -gt 0 -and $status -eq 0) "检出文件不是可执行的 LF Bash：$syntax"
    }
}

$verifyPath = Join-Path $PSScriptRoot 'verify-isolation.ps1'
$tokens = $null; $parseErrors = $null
$ast = [Management.Automation.Language.Parser]::ParseFile($verifyPath, [ref]$tokens, [ref]$parseErrors)
Test-That ($parseErrors.Count -eq 0) '验证脚本解析失败'
# 仅载入实际脚本的错误处理设置和 SQL 包装函数，不执行顶层 Docker/SQL 流程。
$header = @()
foreach ($statement in $ast.EndBlock.Statements) {
    if (($statement -is [Management.Automation.Language.AssignmentStatementAst] -and
            $statement.Left.VariablePath.UserPath -in @('ErrorActionPreference', 'PSNativeCommandUseErrorActionPreference')) -or
        ($statement -is [Management.Automation.Language.FunctionDefinitionAst] -and
            $statement.Name -in @('Assert-That', 'Invoke-DbSql'))) {
        $header += $statement.Extent.Text
    }
}
$setup = [scriptblock]::Create($header -join "`n")
$composeArgs = @()
foreach ($incoming in @($false, $true)) {
    Invoke-Case "native exit handling, caller preference=$incoming" {
        $PSNativeCommandUseErrorActionPreference = $incoming
        & {
            . $setup
            $nativeCode = '[Console]::Error.WriteLine("ERROR 1045 test fixture"); exit 1'
            # Docker 是外部依赖；替身只转发到真实 pwsh 原生进程，测试生产函数的处理结果。
            function docker {
                $encoded = [Convert]::ToBase64String([Text.Encoding]::Unicode.GetBytes($nativeCode))
                $input | & $pwshPath -NoProfile -NonInteractive -EncodedCommand $encoded
            }
            $null = Invoke-DbSql test app 'SELECT 1' 'ERROR 1045'
            foreach ($expected in @('', 'ERROR 1044')) {
                $rejected = $false
                try { $null = Invoke-DbSql test app 'SELECT 1' $expected } catch {
                    Test-That ($_.FullyQualifiedErrorId -notlike '*NativeCommand*') '原生命令异常绕过了业务断言'
                    $rejected = $true
                }
                Test-That $rejected '非预期错误必须被拒绝'
            }
            $nativeCode = 'Write-Output "query-ok"; exit 0'
            Test-That ((Invoke-DbSql test app 'SELECT 1') -eq 'query-ok') '成功查询未正常返回'
            $rejected = $false
            try { $null = Invoke-DbSql test app 'SELECT 1' 'ERROR 1045' } catch { $rejected = $true }
            Test-That $rejected '预期拒绝时不能把成功退出当作通过'
        }
        Test-That ($PSNativeCommandUseErrorActionPreference -eq $incoming) '脚本设置泄漏到调用方'
    }
}

foreach ($name in @('init-secrets.ps1', 'verify-isolation.ps1')) {
    Invoke-Case "PowerShell 5.1 stops before script body: $name" {
        $file = Join-Path $PSScriptRoot $name
        $scriptAst = [Management.Automation.Language.Parser]::ParseFile($file, [ref]$tokens, [ref]$parseErrors)
        # 先确认版本门槛，避免测试在未修复的脚本中触发真实初始化或数据库操作。
        Test-That ($scriptAst.ScriptRequirements.RequiredPSVersion -ge [version]'7.2') '缺少 PowerShell 7.2+ 版本门槛'
        $legacy = & powershell.exe -NoProfile -NonInteractive -File $file 2>&1
        Test-That ($LASTEXITCODE -ne 0 -and ($legacy -join "`n") -match '7\.2') '旧引擎没有明确拒绝版本'
    }
}
if ($failures -gt 0) { throw "$failures portability case(s) failed" }
Write-Output 'PASS all 6 portability cases; no Docker command or database operation executed'
