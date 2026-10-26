[CmdletBinding()]
param(
    [ValidateSet('Stage','Install')][string]$Action = 'Stage',
    [switch]$Elevated,
    [string]$Resume
)
$ErrorActionPreference = 'Stop'
$repo = Split-Path -Parent $PSScriptRoot
$goBin = Join-Path $env:ProgramFiles 'Go\bin'
if (Test-Path -LiteralPath $goBin) { $env:PATH = $goBin + [IO.Path]::PathSeparator + $env:PATH }
if ($Resume -and $Action -ne 'Install') { throw '-Resume requires -Action Install.' }
$python = Join-Path $repo '.venv\Scripts\python.exe'
if (-not (Test-Path -LiteralPath $python)) { throw 'Repository Python environment is missing.' }
$logDir = Join-Path $repo '.cache\local-update'
New-Item -ItemType Directory -Force -Path $logDir | Out-Null
$log = Join-Path $logDir ('launcher-' + (Get-Date -Format 'yyyyMMdd-HHmmss-fff') + '.log')
$admin = ([Security.Principal.WindowsPrincipal][Security.Principal.WindowsIdentity]::GetCurrent()).IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)
$requiresAdmin = $false
if ($Action -eq 'Install' -and -not $admin) {
    try {
        $status = Invoke-RestMethod 'http://127.0.0.1:12701/api/system/is-elevated' -TimeoutSec 3
        $requiresAdmin = [bool]$status.elevated
    } catch [System.Net.Http.HttpRequestException] {
        # Closed app: the installer independently checks that no agents remain.
    }
}
if ($requiresAdmin) {
    if ($Elevated) { throw 'Administrator elevation was not granted.' }
    Write-Host 'MaaEnd is elevated. Windows will request administrator approval for this install.'
    $arguments = @('-NoProfile', '-File', ('"' + $PSCommandPath + '"'), '-Action', $Action, '-Elevated')
    if ($Resume) { $arguments += @('-Resume', ('"' + $Resume + '"')) }
    $helper = Start-Process -FilePath (Get-Command pwsh).Source -Verb RunAs -WindowStyle Hidden -ArgumentList $arguments -PassThru -Wait
    if ($helper.ExitCode -ne 0) { throw "Elevated update failed ($($helper.ExitCode)). Logs: $logDir" }
    return
}
$updateArgs = @('-X','utf8',(Join-Path $repo 'tools\update_local.py'))
if ($Action -eq 'Install') { $updateArgs += @('--apply','--close-app') }
if ($Resume) { $updateArgs += @('--resume', (Resolve-Path -LiteralPath $Resume).Path) }
& $python @updateArgs *> $log
$updateExit = $LASTEXITCODE
Get-Content -LiteralPath $log -Tail 12 | Write-Host
if ($updateExit -ne 0) { throw "Update failed ($updateExit). Full log: $log" }
if ($Action -eq 'Install') {
    Start-Process -FilePath (Join-Path $repo 'install\MaaEnd.exe') -WorkingDirectory (Join-Path $repo 'install') -WindowStyle Hidden
}
Write-Host "Completed $Action. Log: $log"
