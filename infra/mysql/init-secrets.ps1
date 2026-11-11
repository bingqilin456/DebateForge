#requires -Version 7.2
$ErrorActionPreference = 'Stop'
$secretDirectory = [IO.Path]::GetFullPath((Join-Path $PSScriptRoot '../../.cache/t02-mysql/secrets'))
$names = @('dev-root', 'dev-app', 'dev-migrator', 'test-root', 'test-app', 'test-migrator')
$existing = @($names | Where-Object { Test-Path -LiteralPath (Join-Path $secretDirectory "$_.txt") })
if ($existing.Count -gt 0 -and $existing.Count -ne $names.Count) {
    throw '发现不完整的秘密文件集合；停止以免与已有数据库口令不一致，请人工检查。'
}
if ($existing.Count -eq $names.Count) {
    foreach ($name in $names) {
        if ([IO.File]::ReadAllText((Join-Path $secretDirectory "$name.txt")) -cnotmatch '^[a-f0-9]{64}$') {
            throw '已有秘密文件格式不符，未覆盖任何口令。'
        }
    }
    Write-Output '秘密文件已存在，未覆盖；修改文件不会更新已有数据库账户。'
    return
}

$null = New-Item -ItemType Directory -Path $secretDirectory -Force
# 本地明文文件只授予当前 Windows 用户，Compose 按服务分别挂载。
$identity = [Security.Principal.WindowsIdentity]::GetCurrent().User
$acl = [Security.AccessControl.DirectorySecurity]::new()
$acl.SetOwner($identity)
$acl.SetAccessRuleProtection($true, $false)
$acl.AddAccessRule([Security.AccessControl.FileSystemAccessRule]::new(
    $identity, 'FullControl', 'ContainerInherit,ObjectInherit', 'None', 'Allow'))
Set-Acl -LiteralPath $secretDirectory -AclObject $acl

$generator = [Security.Cryptography.RandomNumberGenerator]::Create()
try {
    foreach ($name in $names) {
        $bytes = New-Object byte[] 32
        $generator.GetBytes($bytes)
        $passwordText = [BitConverter]::ToString($bytes).Replace('-', '').ToLowerInvariant()
        $path = Join-Path $secretDirectory "$name.txt"
        $stream = [IO.File]::Open($path, [IO.FileMode]::CreateNew, [IO.FileAccess]::Write)
        try {
            $encoded = [Text.Encoding]::ASCII.GetBytes($passwordText)
            $stream.Write($encoded, 0, $encoded.Length)
        } finally { $stream.Dispose() }
    }
} finally { $generator.Dispose(); $passwordText = $null }
Write-Output '已生成六份独立本地秘密文件；口令未输出，目录已限制为当前 Windows 用户访问。'
