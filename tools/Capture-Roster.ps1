[CmdletBinding()]
param(
    [string]$Resume,
    [string]$Output,
    [string]$ApiUrl = 'http://127.0.0.1:12702/api',
    [string]$Instance,
    [ValidateSet('Overview', 'Gear', 'Weapons')][string]$StartPage = 'Overview',
    [ValidateRange(1, 200)][int]$MaxOperators = 80
)
$ErrorActionPreference = 'Stop'
$repoRoot = Split-Path -Parent $PSScriptRoot
$python = Join-Path $repoRoot '.venv\Scripts\python.exe'
if (-not (Test-Path -LiteralPath $python)) {
    throw 'Repository Python environment missing. Run uv sync from the repository first.'
}
$scriptArgs = @((Join-Path $PSScriptRoot 'capture_roster.py'), '--api-url', $ApiUrl, '--start-page', $StartPage, '--max-operators', $MaxOperators)
if ($Resume) { $scriptArgs += @('--resume', $Resume) }
if ($Output) { $scriptArgs += @('--output', $Output) }
if ($Instance) { $scriptArgs += @('--instance', $Instance) }
& $python @scriptArgs
exit $LASTEXITCODE
