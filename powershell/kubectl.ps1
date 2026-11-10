& (Join-Path $PSScriptRoot 'dispatch.ps1') -Command 'kubectl' -PipedInput $MyInvocation.ExpectingInput -PipedOutput ($MyInvocation.PipelinePosition -lt $MyInvocation.PipelineLength) -InputStream $input -Arguments $args
exit $LASTEXITCODE
