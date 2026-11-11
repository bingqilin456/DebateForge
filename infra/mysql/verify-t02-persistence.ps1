#requires -Version 7.2
$ErrorActionPreference = 'Stop'
$PSNativeCommandUseErrorActionPreference = $false
# 显式执行才会重启唯一的测试服务；不迁移、不播种、不改账户和卷。
$composeArgs = @('--context', 'desktop-linux', 'compose', '--project-name', 'siyige-t02',
    '--file', (Join-Path $PSScriptRoot 'compose.yaml'))

function Assert-That([bool]$Condition, [string]$Message) {
    if (-not $Condition) { throw $Message }
}

function Get-Container([string]$Environment) {
    $id = & docker @composeArgs ps --all --quiet "mysql-$Environment"
    Assert-That ($LASTEXITCODE -eq 0 -and @($id).Count -eq 1 -and "$id" -match '^[a-f0-9]{64}$') '容器身份不明确'
    $details = & docker --context desktop-linux inspect $id | ConvertFrom-Json
    Assert-That ($LASTEXITCODE -eq 0) '读取容器失败'
    $container = @($details)[0]
    Assert-That ($container.Config.Labels.'com.docker.compose.project' -eq 'siyige-t02') '非本项目容器'
    Assert-That ($container.Config.Labels.'com.docker.compose.service' -eq "mysql-$Environment") '服务身份不符'
    Assert-That ($container.Config.Image -eq 'mysql:8.4.13') '镜像不符'
    $volume = @($container.Mounts | Where-Object Destination -eq '/var/lib/mysql')
    Assert-That ($volume.Count -eq 1 -and $volume[0].Name -eq "siyige-t02_mysql-$Environment-data") '卷不符'
    $port = if ($Environment -eq 'test') { '13307' } else { '13306' }
    $bindings = @($container.NetworkSettings.Ports.'3306/tcp')
    Assert-That ($bindings.Count -eq 1 -and $bindings[0].HostIp -eq '127.0.0.1' -and $bindings[0].HostPort -eq $port) '回环端口不符'
    return $container
}

function Invoke-TestSql([string]$Sql) {
    $result = $Sql | & docker @composeArgs exec -T mysql-test bash /opt/siyige/mysql-client.sh migrator --default-character-set=utf8mb4 2>&1
    Assert-That ($LASTEXITCODE -eq 0) '测试库只读核对失败；停止且不输出原始 SQL/驱动错误'
    return (($result | ForEach-Object { "$_" }) -join "`n").Trim()
}

function Get-Snapshot {
    $identity = Invoke-TestSql 'SELECT DATABASE(),CURRENT_USER(),VERSION();'
    Assert-That ($identity -eq "siyige_test`tsiyige_test_migrator@%`t8.4.13") '实际库/账户/版本不符'
    $versions = Invoke-TestSql 'SELECT id,version_id,is_applied,tstamp FROM goose_db_version ORDER BY id;'
    $historyValid = Invoke-TestSql 'SELECT COUNT(*)=11 AND COUNT(DISTINCT version_id)=11 AND MIN(version_id)=0 AND MAX(version_id)=10 AND MIN(is_applied)=1 FROM goose_db_version;'
    Assert-That ($historyValid -eq '1') '正式历史不是完整的 0–10'
    $seedValid = Invoke-TestSql 'SELECT COUNT(*)=5 AND SUM(id BETWEEN 1 AND 5 AND enabled=1)=5 FROM topics;'
    Assert-That ($seedValid -eq '1') '不是五条启用种子；请先检查数据，不自动修复'
    # HEX 避免 Windows 输出编码影响字节级比较；保留完整标题、描述和微秒时间。
    $topics = Invoke-TestSql 'SELECT id,HEX(title),HEX(description),enabled,created_at FROM topics ORDER BY id;'
    $schema = @()
    $counts = @()
    foreach ($table in @('users', 'auth_sessions', 'topics', 'debates', 'debate_turns', 'messages')) {
        $schema += Invoke-TestSql "SHOW CREATE TABLE $table;"
        $counts += Invoke-TestSql "SELECT '$table',COUNT(*) FROM $table;"
    }
    return [ordered]@{ Identity=$identity; Versions=$versions; Topics=$topics; Schema=$schema; Counts=$counts }
}

$testBefore = Get-Container test
$devBefore = Get-Container dev
Assert-That ($testBefore.State.Health.Status -eq 'healthy') '测试库未健康，停止'
$snapshotBefore = Get-Snapshot
$evidenceDirectory = [IO.Path]::GetFullPath((Join-Path $PSScriptRoot '../../.cache/t02-mysql'))
Assert-That (Test-Path -LiteralPath $evidenceDirectory -PathType Container) '证据目录不存在'
$evidenceFile = Join-Path $evidenceDirectory ("persistence-{0}.json" -f (Get-Date -Format 'yyyyMMdd-HHmmss-fff'))
# 重启失败也保留前置证据；秘密与完整 inspect 不落盘。
$evidence = [ordered]@{ Before=$snapshotBefore; TestContainer=$testBefore.Id; DevContainer=$devBefore.Id; Verified=$false }
$evidence | ConvertTo-Json -Depth 8 | Set-Content -LiteralPath $evidenceFile -Encoding utf8
& docker @composeArgs restart --timeout 30 mysql-test
Assert-That ($LASTEXITCODE -eq 0) '测试库重启失败，停止'
$deadline = (Get-Date).AddSeconds(60)
do {
    Start-Sleep -Seconds 2
    $testAfter = Get-Container test
    if ($testAfter.State.Health.Status -eq 'healthy') { break }
} while ((Get-Date) -lt $deadline)
Assert-That ($testAfter.State.Health.Status -eq 'healthy') '重启后 60 秒内未恢复健康'
Assert-That ($testAfter.Id -eq $testBefore.Id) '测试容器被重建'
Assert-That ($testAfter.State.StartedAt -ne $testBefore.State.StartedAt) '未观察到真实重启'
$devAfter = Get-Container dev
Assert-That ($devAfter.Id -eq $devBefore.Id -and $devAfter.State.StartedAt -eq $devBefore.State.StartedAt) '开发容器发生变化'
$snapshotAfter = Get-Snapshot
Assert-That (($snapshotBefore | ConvertTo-Json -Depth 8 -Compress) -ceq ($snapshotAfter | ConvertTo-Json -Depth 8 -Compress)) '重启前后版本/结构/行数/种子内容不同'
$evidence.After = $snapshotAfter
$evidence.Verified = $true
$evidence.TestStartedBefore = $testBefore.State.StartedAt
$evidence.TestStartedAfter = $testAfter.State.StartedAt
$evidence | ConvertTo-Json -Depth 8 | Set-Content -LiteralPath $evidenceFile -Encoding utf8
Write-Output 'PASS 仅重启 mysql-test；容器和卷不变，开发容器未重启；0–10 历史、六表结构/行数和五条种子内容/时间完全一致'
Write-Output "本地证据：$evidenceFile"
