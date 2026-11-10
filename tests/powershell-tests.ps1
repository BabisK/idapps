$ErrorActionPreference = 'Stop'
$root = Split-Path $PSScriptRoot -Parent
$wrappers = Join-Path $root 'powershell'
$temp = Join-Path ([IO.Path]::GetTempPath()) ('idapps-test-' + [guid]::NewGuid())
New-Item -ItemType Directory -Path $temp | Out-Null
$oldPath = $env:PATH
$oldHome = $HOME
try {
    $source = @'
using System;
using System.IO;
class Docker {
    static int Main(string[] args) {
        File.AppendAllText(Environment.GetEnvironmentVariable("IDAPPS_LOG"), String.Join("|", Array.ConvertAll(args, x => Convert.ToBase64String(System.Text.Encoding.UTF8.GetBytes(x)))) + "\n");
        if (args.Length > 0 && (args[0] == "stop" || args[0] == "volume") && Environment.GetEnvironmentVariable("IDAPPS_FAIL_ACTION") == "1") { Console.Error.WriteLine("mock failure"); return 27; }
        if (args.Length > 0 && args[0] == "info") return Environment.GetEnvironmentVariable("IDAPPS_UNREACHABLE") == "1" ? 1 : 0;
        if (args.Length > 0 && args[0] == "ps" && Environment.GetEnvironmentVariable("IDAPPS_RUNNING") == "1") Console.WriteLine("container-id");
        if (args.Length > 0 && args[0] == "run" && Environment.GetEnvironmentVariable("IDAPPS_STDIN") == "1") { Console.InputEncoding = System.Text.Encoding.UTF8; Console.Write(Convert.ToBase64String(System.Text.Encoding.UTF8.GetBytes(Console.In.ReadToEnd().TrimEnd('\r', '\n')))); }
        if (args.Length > 0 && args[0] == "run" && Environment.GetEnvironmentVariable("IDAPPS_EXIT") == "1") return 42;
        return 0;
    }
}
'@
    $mock = Join-Path $temp 'docker.exe'
    if ($PSVersionTable.PSVersion.Major -ge 7) {
        # PowerShell 7's Add-Type cannot emit executables; Windows includes 5.1.
        $sourceFile = Join-Path $temp 'Docker.cs'
        Set-Content -LiteralPath $sourceFile -Value $source -Encoding UTF8
        $compiler = Join-Path $temp 'compile.ps1'
        Set-Content -LiteralPath $compiler -Value 'param($SourceFile, $OutputFile) Add-Type -Path $SourceFile -OutputAssembly $OutputFile -OutputType ConsoleApplication' -Encoding UTF8
        & "$env:SystemRoot\System32\WindowsPowerShell\v1.0\powershell.exe" -NoProfile -ExecutionPolicy Bypass -File $compiler $sourceFile $mock
        if ($LASTEXITCODE -ne 0 -or !(Test-Path $mock)) { throw 'Failed to build Docker mock' }
    } else {
        Add-Type -TypeDefinition $source -OutputAssembly $mock -OutputType ConsoleApplication
    }
    $env:PATH = "$temp;$oldPath;$wrappers"
    $env:IDAPPS_LOG = Join-Path $temp 'calls'
    $fakeHome = Join-Path $temp 'home with spaces ü'
    New-Item -ItemType Directory -Path $fakeHome | Out-Null
    Set-Variable HOME -Value $fakeHome -Force
    function Assert($condition, $message) { if (!$condition) { throw $message } }
    function Calls {
        @(Get-Content $env:IDAPPS_LOG | ForEach-Object { ,@($_.Split('|') | ForEach-Object { [Text.Encoding]::UTF8.GetString([Convert]::FromBase64String($_)) }) })
    }
    Assert ((Get-Command etcdctl).Source -eq (Join-Path $wrappers 'etcdctl.ps1')) 'PATH script discovery failed'
    $roots = @(Get-ChildItem $root -File | Where-Object { (Get-Content -LiteralPath $_.FullName -TotalCount 1) -eq '#!/bin/sh' })
    foreach ($wrapper in $roots) {
        $entry = Join-Path $wrappers ($wrapper.Name + '.ps1')
        Assert (Test-Path $entry) "Missing $entry"
        & $entry 'test argument' | Out-Null
        Assert ($LASTEXITCODE -eq 0) "Failed $entry"
    }
    function ImageFor($name) {
        $contents = Get-Content -LiteralPath (Join-Path $root $name) -Raw
        $imageName = [regex]::Match($contents, '(?m)^IMAGE_NAME=([^\r\n]+)').Groups[1].Value
        $version = [regex]::Match($contents, '(?m)^IMAGE_VERSION=([^\r\n]+)').Groups[1].Value
        return "${imageName}:${version}"
    }
    $serviceArgs = @{
        etcd = @('--name','etcd','-p','2379:2379','-p','2380:2380','-v','etcd:/etcd-data','--entrypoint','/usr/local/bin/etcd','IMAGE','--listen-client-urls','http://0.0.0.0:2379','--advertise-client-urls','http://0.0.0.0:2379')
        'mariadb-server' = @('--name','mariadb','-e','MARIADB_ROOT_PASSWORD=root','-p','3306:3306','-v','mariadb:/var/lib/mysql','IMAGE')
        memcached = @('--name','memcached','-p','11211:11211','IMAGE')
        'mysql-server' = @('--name','mysql','-e','MYSQL_ROOT_PASSWORD=root','-p','3306:3306','-v','mysql:/var/lib/mysql','IMAGE')
        'nats-server' = @('--name','nats-server','-p','4222:4222','-p','6222:6222','-p','8222:8222','IMAGE')
        pgadmin4 = @('--name','pgadmin4','-e','PGADMIN_DEFAULT_EMAIL=user@pgadmin.org','-e','PGADMIN_DEFAULT_PASSWORD=pgadmin','-p','8080:80','-v','pgadmin4:/var/lib/pgadmin','IMAGE')
        phpmyadmin = @('--name','phpmyadmin','-e','PMA_ARBITRARY=1','-p','8082:80','IMAGE')
        postgres = @('--name','postgres','-e','POSTGRES_USER=postgres','-e','POSTGRES_PASSWORD=postgres','-p','5432:5432','-v','postgres:/var/lib/postgresql/data','IMAGE')
        redis = @('--name','redis','-p','6379:6379','-v','redis:/data','IMAGE','--save','60','1')
        'redis-stack' = @('--name','redis-stack','-p','6379:6379','-p','8083:8001','-v','redis-stack:/data','IMAGE')
    }
    $clients = @{
        aws = @('',''); etcdctl = @('/usr/local/bin/etcdctl',''); go = @('','go'); helm = @('','')
        helmfile = @('/usr/local/bin/helmfile',''); kubectl = @('',''); mysql = @('','mysql')
        mysqldump = @('','mysqldump'); nats = @('/usr/local/bin/nats',''); pg_dump = @('','pg_dump')
        psql = @('','psql'); 'redis-cli' = @('','redis-cli')
    }
    foreach ($client in $clients.Keys) {
        Remove-Item $env:IDAPPS_LOG
        & (Join-Path $wrappers ($client + '.ps1')) 'client-argument' | Out-Null
        $call = @(Calls)[-1]
        $imageIndex = [array]::IndexOf($call, (ImageFor $client))
        Assert ($imageIndex -ge 0) "$client image pin missing"
        if ($clients[$client][0]) {
            Assert ($imageIndex -ge 2 -and $call[$imageIndex - 2] -eq '--entrypoint' -and $call[$imageIndex - 1] -eq $clients[$client][0]) "$client entrypoint changed"
        } else { Assert (-not ($call -contains '--entrypoint')) "$client unexpected entrypoint" }
        $expectedTail = if ($clients[$client][1]) { @($clients[$client][1], 'client-argument') } else { @('client-argument') }
        Assert ((@($call[($imageIndex + 1)..($call.Count - 1)]) -join "`n") -ceq ($expectedTail -join "`n")) "$client executable or arguments changed"
    }
    $services = @('etcd','mariadb-server','memcached','mysql-server','nats-server','pgadmin4','phpmyadmin','postgres','redis','redis-stack')
    foreach ($service in $services) {
        $entry = Join-Path $wrappers ($service + '.ps1')
        foreach ($operation in @('default','start','stop')) {
            Remove-Item $env:IDAPPS_LOG
            if ($operation -eq 'default') { & $entry | Out-Null } else { & $entry $operation | Out-Null }
            Assert ($LASTEXITCODE -eq 0) "$service $operation failed"
            $call = @(Calls)[-1]
            $expected = if ($operation -eq 'stop') { 'stop' } else { 'run' }
            Assert ($call[0] -eq $expected) "$service $operation did not invoke Docker $expected"
            if ($operation -ne 'stop') {
                $expectedArgs = @('run','--rm','-d') + @($serviceArgs[$service] | ForEach-Object { if ($_ -eq 'IMAGE') { ImageFor $service } else { $_ } })
                Assert (($call -join "`n") -ceq ($expectedArgs -join "`n")) "$service startup options or image changed"
            }
        }
    }
    $expectedMounts = @{
        aws = ,@('.aws','/tmp/.aws')
        helm = @(@('.kube','/.kube'),@('.helm','/.helm'),@('.config/helm','/.config/helm'),@('.cache/helm','/.cache/helm'))
        helmfile = @(@('.kube','/.kube'),@('.helm','/.helm'),@('.config/helm','/.config/helm'),@('.cache/helm','/.cache/helm'))
        kubectl = ,@('.kube','/.kube')
    }
    foreach ($tool in $expectedMounts.Keys) {
        & (Join-Path $wrappers ($tool + '.ps1')) | Out-Null
        $call = @(Calls)[-1]
        foreach ($mount in $expectedMounts[$tool]) {
            $hostPath = Join-Path $fakeHome $mount[0]
            Assert ($call -contains "${hostPath}:$($mount[1])") "$tool configuration mount missing"
        }
    }
    Remove-Item $env:IDAPPS_LOG
    & (Join-Path $wrappers 'psql.ps1') 'one two' '' 'a"b' 'last' | Out-Null
    $call = @(Calls)[-1]
    Assert ($call[-4] -eq 'one two' -and $call[-3] -ceq '' -and $call[-2] -ceq 'a"b' -and $call[-1] -eq 'last') 'Argument values or order changed'
    $spacePath = Join-Path $temp 'folder with spaces ü'
    New-Item -ItemType Directory -Path $spacePath | Out-Null
    Push-Location $spacePath
    try {
        & (Join-Path $wrappers 'psql.ps1') | Out-Null
        $call = @(Calls)[-1]
        Assert ($call -contains "${spacePath}:/postgres") 'Current directory mount changed'
    } finally { Pop-Location }
    $env:IDAPPS_RUNNING = '1' 
    Remove-Item $env:IDAPPS_LOG
    & (Join-Path $wrappers 'postgres.ps1') clean | Out-Null
    Assert ($LASTEXITCODE -ne 0) 'Clean allowed while running'
    Assert (-not (@(Calls) | Where-Object { $_[0] -eq 'volume' })) 'Clean removed running volume'
    Remove-Item $env:IDAPPS_LOG
    & (Join-Path $wrappers 'postgres.ps1') logs -f -t 12 | Out-Null
    $call = @(Calls)[-1]
    Assert (($call -join ',') -eq 'logs,-f,-n,12,postgres') 'Logs arguments changed'
    $env:IDAPPS_RUNNING = '0'
    & (Join-Path $wrappers 'postgres.ps1') clean | Out-Null
    Assert ($LASTEXITCODE -eq 0) 'Clean failed when stopped'
    $env:IDAPPS_FAIL_ACTION = '1'
    & (Join-Path $wrappers 'postgres.ps1') stop 2>$null | Out-Null
    Assert ($LASTEXITCODE -eq 27) 'Stop failure exit code not preserved'
    & (Join-Path $wrappers 'postgres.ps1') clean 2>$null | Out-Null
    Assert ($LASTEXITCODE -eq 27) 'Clean failure exit code not preserved'
    $env:IDAPPS_FAIL_ACTION = '0'
    $env:IDAPPS_STDIN = '1'
    $stream = 'piped text Καλημέρα ü' | & (Join-Path $wrappers 'psql.ps1')
    Assert ($stream -eq [Convert]::ToBase64String([Text.Encoding]::UTF8.GetBytes('piped text Καλημέρα ü'))) "Piped input or output not preserved: $stream"
    $env:IDAPPS_STDIN = '0'
    $env:IDAPPS_EXIT = '1'
    & (Join-Path $wrappers 'psql.ps1') | Out-Null
    Assert ($LASTEXITCODE -eq 42) 'Exit status not preserved'
    $env:IDAPPS_EXIT = '0'; $env:IDAPPS_UNREACHABLE = '1'
    & (Join-Path $wrappers 'psql.ps1') 2>$null | Out-Null
    Assert ($LASTEXITCODE -ne 0) 'Unreachable Docker succeeded'
    $env:IDAPPS_UNREACHABLE = '0'
    $env:PATH = $wrappers
    & (Join-Path $wrappers 'psql.ps1') 2>$null | Out-Null
    Assert ($LASTEXITCODE -ne 0) 'Missing Docker succeeded'
    $env:PATH = "$temp;$oldPath;$wrappers"
    function psql { 'function wins' }
    Assert ((psql) -eq 'function wins') 'Function precedence changed'
    & (Join-Path $wrappers 'psql.ps1') | Out-Null
    Assert ($LASTEXITCODE -eq 0) 'Explicit script failed with function conflict'
    Remove-Item Function:psql
    Set-Alias psql Write-Output
    Assert ((psql 'alias wins') -eq 'alias wins') 'Alias precedence changed'
    & (Join-Path $wrappers 'psql.ps1') | Out-Null
    Assert ($LASTEXITCODE -eq 0) 'Explicit script failed with alias conflict'
    Remove-Item Alias:psql
    Copy-Item (Join-Path $temp 'docker.exe') (Join-Path $temp 'psql.exe')
    Assert ((Get-Command psql).Source -eq (Join-Path $temp 'psql.exe')) 'Executable precedence changed'
    & (Join-Path $wrappers 'psql.ps1') | Out-Null
    Assert ($LASTEXITCODE -eq 0) 'Explicit script failed with executable conflict'
    Write-Output "PowerShell $($PSVersionTable.PSVersion): $($roots.Count) wrappers and mock checks passed"
} finally {
    $env:PATH = $oldPath
    Set-Variable HOME -Value $oldHome -Force
    Remove-Item $temp -Recurse -Force -ErrorAction SilentlyContinue
    Remove-Item Env:IDAPPS_LOG, Env:IDAPPS_RUNNING, Env:IDAPPS_EXIT, Env:IDAPPS_UNREACHABLE, Env:IDAPPS_STDIN, Env:IDAPPS_FAIL_ACTION -ErrorAction SilentlyContinue
}
