& (Join-Path $PSScriptRoot 'dispatch.ps1') -Command 'mariadb-server' -PipedInput $MyInvocation.ExpectingInput -PipedOutput ($MyInvocation.PipelinePosition -lt $MyInvocation.PipelineLength) -InputStream $input -Arguments $args
exit $LASTEXITCODE
