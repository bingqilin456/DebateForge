#requires -Version 7.2
param([switch]$Restart)
$ErrorActionPreference = 'Stop'
# 预期的 MySQL 拒绝也会退出非零；统一在脚本作用域捕获输出并显式检查退出码。
$PSNativeCommandUseErrorActionPreference = $false
# 固定 context/project，先核对 Docker 元数据，再允许向本次专用库写探针。
$composeArgs = @('--context', 'desktop-linux', 'compose', '--project-name', 'siyige-t02',
    '--file', (Join-Path $PSScriptRoot 'compose.yaml'))
$secretDirectory = [IO.Path]::GetFullPath((Join-Path $PSScriptRoot '../../.cache/t02-mysql/secrets'))
$environments = @('dev', 'test')
$containers = @{}

function Assert-That([bool]$Condition, [string]$Message) {
    if (-not $Condition) { throw $Message }
}

function Invoke-DbSql([string]$Environment, [string]$Role, [string]$Sql, [string]$ExpectedError = '') {
    $result = $Sql | & docker @composeArgs exec -T "mysql-$Environment" bash /opt/siyige/mysql-client.sh $Role 2>&1
    $status = $LASTEXITCODE
    $resultText = ($result | ForEach-Object { "$_" }) -join "`n"
    if ($ExpectedError) {
        Assert-That ($status -ne 0 -and $resultText -match $ExpectedError) "预期拒绝未成立：$Environment/$Role/$ExpectedError"
        return
    }
    Assert-That ($status -eq 0) "数据库验证失败：$Environment/$Role；$resultText"
    return $resultText.Trim()
}

foreach ($environment in $environments) {
    $id = & docker @composeArgs ps --all --quiet "mysql-$environment"
    Assert-That ($LASTEXITCODE -eq 0 -and @($id).Count -eq 1 -and "$id" -match '^[a-f0-9]{64}$') '容器身份不明确'
    $details = & docker --context desktop-linux inspect $id | ConvertFrom-Json
    Assert-That ($LASTEXITCODE -eq 0) '读取容器元数据失败'
    $container = @($details)[0]
    Assert-That ($container.Config.Labels.'com.docker.compose.project' -eq 'siyige-t02') '非本项目容器'
    Assert-That ($container.Config.Labels.'com.docker.compose.service' -eq "mysql-$environment") '服务名不符'
    Assert-That ($container.Config.Image -eq 'mysql:8.4.13') '镜像版本不符'
    Assert-That ($container.State.Health.Status -eq 'healthy') '容器尚未健康'
    $volume = @($container.Mounts | Where-Object { $_.Destination -eq '/var/lib/mysql' })
    Assert-That ($volume.Count -eq 1 -and $volume[0].Name -eq "siyige-t02_mysql-$environment-data") '数据卷不符'
    $networks = @($container.NetworkSettings.Networks.PSObject.Properties.Name)
    Assert-That ($networks.Count -eq 1 -and $networks[0] -eq "siyige-t02_$environment-db") '网络不符'
    $port = if ($environment -eq 'dev') { 13306 } else { 13307 }
    $bindings = @($container.NetworkSettings.Ports.'3306/tcp')
    Assert-That ($bindings.Count -eq 1 -and $bindings[0].HostIp -eq '127.0.0.1' -and $bindings[0].HostPort -eq "$port") '回环端口不符'
    $published = @($container.NetworkSettings.Ports.PSObject.Properties | Where-Object { $null -ne $_.Value })
    Assert-That ($published.Count -eq 1) '存在未预期的发布端口'
    $containers[$environment] = $container

    # 从 Windows 实际连接发布端口并读取 MySQL greeting，不能只检查 Compose 文本。
    $tcp = [Net.Sockets.TcpClient]::new()
    try {
        $connection = $tcp.ConnectAsync('127.0.0.1', $port)
        Assert-That ($connection.Wait(5000)) '回环 TCP 连接超时'
        $stream = $tcp.GetStream()
        $stream.ReadTimeout = 5000
        $buffer = New-Object byte[] 256
        $count = $stream.Read($buffer, 0, $buffer.Length)
        Assert-That ([Text.Encoding]::ASCII.GetString($buffer, 0, $count).Contains('8.4.13')) 'MySQL greeting 版本不符'
    } finally { $tcp.Dispose() }
    $metadata = Invoke-DbSql $environment app 'SELECT VERSION(), @@character_set_server, @@global.time_zone, @@default_storage_engine;'
    Assert-That ($metadata -eq "8.4.13`tutf8mb4`t+00:00`tInnoDB") '数据库运行参数不符'
    $schema = Invoke-DbSql $environment app 'SELECT DATABASE();'
    Assert-That ($schema -eq "siyige_$environment") '默认库不符'
    $rootHosts = Invoke-DbSql $environment root "SELECT Host FROM mysql.user WHERE User='root';"
    Assert-That ($rootHosts -eq 'localhost') 'root 意外允许远程登录'
    $accounts = Invoke-DbSql $environment root "SELECT User FROM mysql.user WHERE User LIKE 'siyige%' ORDER BY User;"
    Assert-That ($accounts -eq "siyige_${environment}_app`nsiyige_${environment}_migrator") '存在未预期的项目账户'
    Write-Output "PASS $environment : 容器/卷/网络/回环 TCP/版本/字符集/UTC/账户身份"
}
Assert-That ($containers.dev.Id -ne $containers.test.Id) '容器未隔离'
$devUuid = Invoke-DbSql dev app 'SELECT @@server_uuid;'
$testUuid = Invoke-DbSql test app 'SELECT @@server_uuid;'
Assert-That ($devUuid -ne $testUuid) 'MySQL 实例 UUID 未隔离'
$passwords = @($environments | ForEach-Object {
    $environment = $_
    'root', 'app', 'migrator' | ForEach-Object {
        [IO.File]::ReadAllText((Join-Path $secretDirectory "$environment-$_.txt"))
    }
})
Assert-That (@($passwords | Select-Object -Unique).Count -eq 6) '口令未独立'
$passwords = $null

# 实际使用另一环境的账号及口令登录，凭据仅通过 stdin 进入临时 option file。
foreach ($environment in $environments) {
    $peer = if ($environment -eq 'dev') { 'test' } else { 'dev' }
    foreach ($role in @('app', 'migrator')) {
        $password = [IO.File]::ReadAllText((Join-Path $secretDirectory "$peer-$role.txt"))
        $configText = "[client]`nuser=siyige_${peer}_$role`npassword=$password`nhost=127.0.0.1`n"
        $shell = 'umask 077; f=$(mktemp); trap ''rm -f -- "$f"'' EXIT; cat > "$f"; mysql --defaults-extra-file="$f" --connect-timeout=5 --execute="SELECT 1"'
        $denied = $configText | & docker @composeArgs exec -T "mysql-$environment" bash -c $shell 2>&1
        Assert-That ($LASTEXITCODE -ne 0 -and (($denied -join "`n") -match 'ERROR 1045')) '交叉凭据未被拒绝'
        $password = $null; $configText = $null
        Invoke-DbSql $environment $role "USE siyige_$peer;" 'ERROR 1044'
        Invoke-DbSql $environment $role "USE siyigex$environment;" 'ERROR 1044'
        Invoke-DbSql $environment $role 'SELECT User FROM mysql.user;' 'ERROR 1142'
    }
    Write-Output "PASS $environment : 交叉凭据 1045/其他库与下划线通配库 1044/系统账户表 1142"
}

$probe = 't02_probe_' + [Guid]::NewGuid().ToString('N')
$created = @()
try {
    foreach ($environment in $environments) {
        $null = Invoke-DbSql $environment migrator "CREATE TABLE $probe (id INT PRIMARY KEY, marker VARCHAR(32) NOT NULL) ENGINE=InnoDB;"
        $created += $environment
        $null = Invoke-DbSql $environment app "INSERT INTO $probe VALUES (1, '$environment'), (2, 'discard'); UPDATE $probe SET marker='updated' WHERE id=2; DELETE FROM $probe WHERE id=2;"
        $marker = Invoke-DbSql $environment app "SELECT marker FROM $probe;"
        Assert-That ($marker -eq $environment) '数据标记串库'
        Invoke-DbSql $environment app "CREATE TABLE ${probe}_denied (id INT);" 'ERROR 1142'
        Invoke-DbSql $environment app "DROP TABLE $probe;" 'ERROR 1142'
        Write-Output "PASS $environment : 迁移 DDL/应用 CRUD/应用 DDL 拒绝/数据独立"
    }
    if ($Restart) {
        & docker @composeArgs restart mysql-dev mysql-test
        Assert-That ($LASTEXITCODE -eq 0) '重启失败'
        & docker @composeArgs up --detach --wait --wait-timeout 180 mysql-dev mysql-test
        Assert-That ($LASTEXITCODE -eq 0) '重启后未健康'
        foreach ($environment in $environments) {
            $marker = Invoke-DbSql $environment app "SELECT marker FROM $probe;"
            Assert-That ($marker -eq $environment) '重启后数据丢失或串库'
        }
        Write-Output 'PASS 重启两个数据库后，各自数据标记仍可读'
    }
} finally {
    # 只清理由本次执行生成且成功创建的随机探针表，不删除业务表或卷。
    foreach ($environment in $created) {
        $null = Invoke-DbSql $environment migrator "DROP TABLE $probe;"
    }
}
foreach ($environment in $environments) {
    $remaining = Invoke-DbSql $environment migrator "SELECT COUNT(*) FROM information_schema.tables WHERE table_schema='siyige_$environment';"
    Write-Output "PASS $environment : 探针已清理，当前库表数=$remaining"
}
Write-Output 'PASS 隔离验证完成；未执行业务迁移、种子或后端就绪接口验证。'
