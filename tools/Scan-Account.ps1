[CmdletBinding()]
param([Parameter(ValueFromRemainingArguments = $true)][string[]]$ScanArguments)
$ErrorActionPreference = 'Stop'
$repoRoot = Split-Path -Parent $PSScriptRoot
$python = Join-Path $repoRoot '.venv\Scripts\python.exe'
if (-not (Test-Path -LiteralPath $python)) {
    throw 'Repository Python environment missing. Run uv sync first.'
}
& $python (Join-Path $PSScriptRoot 'account_scan.py') @ScanArguments
exit $LASTEXITCODE
