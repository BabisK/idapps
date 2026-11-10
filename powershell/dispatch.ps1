param(
    [Parameter(Mandatory=$true)][string]$Command,
    [bool]$PipedInput,
    [bool]$PipedOutput,
    $InputStream,
    [Parameter(ValueFromRemainingArguments=$true)][AllowEmptyString()][string[]]$Arguments
)

# Read pins as data, never execute the shell scripts.
$source = Join-Path (Split-Path $PSScriptRoot -Parent) $Command
$content = Get-Content -LiteralPath $source -Raw
$name = [regex]::Match($content, '(?m)^IMAGE_NAME=([^\r\n]+)').Groups[1].Value
$version = [regex]::Match($content, '(?m)^IMAGE_VERSION=([^\r\n]+)').Groups[1].Value
if (!$name -or !$version) { [Console]::Error.WriteLine("Cannot read image pin for $Command"); exit 1 }
$image = "${name}:${version}"
$docker = Get-Command docker -CommandType Application -ErrorAction SilentlyContinue | Select-Object -First 1
if (!$docker) { [Console]::Error.WriteLine('Docker CLI not found. Install Docker and add it to PATH.'); exit 1 }
$dockerPath = $docker.Source
$previousPreference = $ErrorActionPreference
$ErrorActionPreference = 'Continue'
& $dockerPath info *> $null
$ErrorActionPreference = $previousPreference
if ($LASTEXITCODE -ne 0) { [Console]::Error.WriteLine('Docker daemon is not running or is unreachable.'); exit 1 }

# Windows PowerShell and PowerShell 7 legacy native argument handling do not
# preserve empty strings or embedded quotes. Encode for CommandLineToArgvW.
function Invoke-Docker([string[]]$DockerArgs, [bool]$Pipe = $false) {
    $nativeArgs = $DockerArgs
    if ($env:OS -eq 'Windows_NT' -and ($PSVersionTable.PSVersion.Major -lt 7 -or
        !(Get-Variable PSNativeCommandArgumentPassing -ErrorAction SilentlyContinue) -or
        $PSNativeCommandArgumentPassing -eq 'Legacy')) {
        $nativeArgs = @($DockerArgs | ForEach-Object {
            if ($_ -eq '') { '""' }
            else { '"' + ([regex]::Replace($_, '(\\*)"', '$1$1\"') -replace '(\\+)$', '$1$1') + '"' }
        })
    }
    if ($Pipe) {
        $previousEncoding = $global:OutputEncoding
        try {
            $global:OutputEncoding = New-Object System.Text.UTF8Encoding $false
            $InputStream | & $dockerPath @nativeArgs
        } finally { $global:OutputEncoding = $previousEncoding }
    }
    else { & $dockerPath @nativeArgs }
}

$services = @{
    'etcd' = @{ Name='etcd'; Volume='etcd'; Options=@('-p','2379:2379','-p','2380:2380','-v','etcd:/etcd-data','--entrypoint','/usr/local/bin/etcd'); Tail=@('--listen-client-urls','http://0.0.0.0:2379','--advertise-client-urls','http://0.0.0.0:2379'); Message='localhost on client port 2379 and peer port 2380' }
    'mariadb-server' = @{ Name='mariadb'; Volume='mariadb'; Options=@('-e','MARIADB_ROOT_PASSWORD=root','-p','3306:3306','-v','mariadb:/var/lib/mysql'); Message='localhost port 3306, username: root, password: root' }
    'mysql-server' = @{ Name='mysql'; Volume='mysql'; Options=@('-e','MYSQL_ROOT_PASSWORD=root','-p','3306:3306','-v','mysql:/var/lib/mysql'); Message='localhost port 3306, username: root, password: root' }
    'postgres' = @{ Name='postgres'; Volume='postgres'; Options=@('-e','POSTGRES_USER=postgres','-e','POSTGRES_PASSWORD=postgres','-p','5432:5432','-v','postgres:/var/lib/postgresql/data'); Message='localhost port 5432, username: postgres, password: postgres' }
    'redis' = @{ Name='redis'; Volume='redis'; Options=@('-p','6379:6379','-v','redis:/data'); Tail=@('--save','60','1'); Message='localhost port 6379' }
    'redis-stack' = @{ Name='redis-stack'; Volume='redis-stack'; Options=@('-p','6379:6379','-p','8083:8001','-v','redis-stack:/data'); Message='localhost port 6379, RedisInsights port 8083' }
    'pgadmin4' = @{ Name='pgadmin4'; Volume='pgadmin4'; Options=@('-e','PGADMIN_DEFAULT_EMAIL=user@pgadmin.org','-e','PGADMIN_DEFAULT_PASSWORD=pgadmin','-p','8080:80','-v','pgadmin4:/var/lib/pgadmin'); Message='http://localhost:8080, username: user@pgadmin.org, password: pgadmin' }
    'phpmyadmin' = @{ Name='phpmyadmin'; Options=@('-e','PMA_ARBITRARY=1','-p','8082:80'); Message='http://localhost:8082' }
    'memcached' = @{ Name='memcached'; Options=@('-p','11211:11211'); Message='localhost port 11211' }
    'nats-server' = @{ Name='nats-server'; Options=@('-p','4222:4222','-p','6222:6222','-p','8222:8222'); Message='localhost ports 4222, 6222 and 8222' }
}
if ($services.ContainsKey($Command)) {
    $service = $services[$Command]; $container = $service.Name
    $operation = if ($Arguments.Count) { $Arguments[0] } else { 'start' }
    switch ($operation) {
        { $_ -eq 'start' -or $_ -eq 'clean' -or $_ -eq 'logs' } {
            if ($operation -eq 'clean' -and !$service.Volume) { exit 0 }
            $running = @(Invoke-Docker @('ps','-q','-f',"name=$container"))
            if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }
            if ($operation -eq 'start') {
                if ($running.Count -gt 0) { Write-Output "$container is already running"; exit 1 }
                $startArgs = @('run','--rm','-d','--name',$container) + $service.Options + @($image)
                if ($service.Tail) { $startArgs += $service.Tail }
                Invoke-Docker $startArgs | Out-Null
                if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }
                Write-Output "$container started at $($service.Message)"; exit 0
            }
            if ($operation -eq 'clean') {
                if ($running.Count -gt 0) { Write-Output "Cannot clean when container $container is running. Please stop the container first."; exit 1 }
                $oldPreference = $ErrorActionPreference
                try { $ErrorActionPreference = 'Continue'; Invoke-Docker @('volume','rm',$service.Volume) *> $null }
                finally { $ErrorActionPreference = $oldPreference }
                if ($LASTEXITCODE -ne 0) { [Console]::Error.WriteLine("Docker failed to remove volume $($service.Volume) (exit $LASTEXITCODE)."); exit $LASTEXITCODE }
                Write-Output "Cleaned up data for $container"; exit 0
            }
            $options = @()
            for ($i = 1; $i -lt $Arguments.Count; $i++) {
                if ($Arguments[$i] -eq '--') { break }
                if ($Arguments[$i] -eq '-f') { $options += '-f'; continue }
                if ($Arguments[$i] -eq '-t') {
                    $i++; if ($i -ge $Arguments.Count -or !$Arguments[$i]) { [Console]::Error.WriteLine('Error: "-t" requires a non empty argument'); exit 1 }
                    $options += @('-n',$Arguments[$i]); continue
                }
                if ($Arguments[$i].StartsWith('-')) { [Console]::Error.WriteLine("WARN: Unknown option (ignored): $($Arguments[$i])"); continue }
                break
            }
            if ($running.Count -eq 0) { Write-Output "$container is not running. No logs to display"; exit 1 }
            Invoke-Docker (@('logs') + $options + @($container)); exit $LASTEXITCODE
        }
        'stop' {
            $oldPreference = $ErrorActionPreference
            try { $ErrorActionPreference = 'Continue'; Invoke-Docker @('stop',$container) *> $null }
            finally { $ErrorActionPreference = $oldPreference }
            if ($LASTEXITCODE -ne 0) { [Console]::Error.WriteLine("Docker failed to stop container $container (exit $LASTEXITCODE)."); exit $LASTEXITCODE }
            Write-Output "$container has stopped"; exit 0
        }
        default { exit 0 }
    }
}

$run = @('run','--rm','-i')
if (![Console]::IsInputRedirected -and ![Console]::IsOutputRedirected -and !$PipedInput -and !$PipedOutput) { $run += '-t' }
$run += '--net=host'
$cwd = (Get-Location).ProviderPath
$client = @{
    'etcdctl' = @('/etcd','/usr/local/bin/etcdctl','')
    'go' = @('/src','','go')
    'mysql' = @('/mysql','','mysql')
    'mysqldump' = @('/mysql','','mysqldump')
    'nats' = @('/nats','/usr/local/bin/nats','')
    'pg_dump' = @('/postgres','','pg_dump')
    'psql' = @('/postgres','','psql')
    'redis-cli' = @('/redis','','redis-cli')
    'helm' = @('/helm','','')
    'helmfile' = @('/helmfile','/usr/local/bin/helmfile','')
}
if ($client.ContainsKey($Command)) {
    $cfg = $client[$Command]
    $run += @('-v',"${cwd}:$($cfg[0])",'-w',$cfg[0])
}
if ($Command -eq 'aws') {
    $run += @('-v',"$(Join-Path $HOME '.aws'):/tmp/.aws",'-v',"${cwd}:/aws",'-e','AWS_CONFIG_FILE=/tmp/.aws/config','-e','AWS_SHARED_CREDENTIALS_FILE=/tmp/.aws/credentials')
}
if ($Command -in @('helm','helmfile','kubectl')) {
    $mounts = @(@('.kube','/.kube'),@('.helm','/.helm'),@('.config/helm','/.config/helm'),@('.cache/helm','/.cache/helm'))
    if ($Command -eq 'kubectl') { $mounts = ,@('.kube','/.kube') }
    foreach ($mount in $mounts) {
        $path = Join-Path $HOME $mount[0]
        if (!(Test-Path -LiteralPath $path)) { New-Item -ItemType Directory -Force -Path $path | Out-Null }
        $run += @('-v',"${path}:$($mount[1])")
    }
}
if ($client.ContainsKey($Command) -and $cfg[1]) { $run += @('--entrypoint',$cfg[1]) }
$run += $image
if ($client.ContainsKey($Command) -and $cfg[2]) { $run += $cfg[2] }
$run += $Arguments
Invoke-Docker $run $PipedInput
exit $LASTEXITCODE
