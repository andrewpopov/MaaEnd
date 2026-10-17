[CmdletBinding()]
param(
    [ValidateSet('check', 'prepare', 'validate', 'publish', 'build')]
    [string]$Action = 'check',
    [ValidateSet('maaend', 'mxu')]
    [string]$Profile = 'maaend',
    [string]$Repo = (Split-Path -Parent $PSScriptRoot),
    [string]$Run,
    [string]$ForkRemote = 'origin',
    [string]$UpstreamRemote = 'upstream'
)
$ErrorActionPreference = 'Stop'

# Use installed toolchains; do not download or install anything in the launcher.
$toolDirectories = @(
    (Join-Path $env:ProgramFiles 'Go\bin'),
    (Join-Path $env:USERPROFILE '.cache\codex-runtimes\codex-primary-runtime\dependencies\node\bin')
)
foreach ($directory in $toolDirectories) {
    if (Test-Path -LiteralPath $directory -PathType Container) {
        $env:PATH = $directory + [IO.Path]::PathSeparator + $env:PATH
    }
}
if ($Profile -eq 'mxu') {
    $cargoRoot = Join-Path $Repo '.build-tools\cargo'
    $rustupRoot = Join-Path $Repo '.build-tools\rustup'
    $mingwBin = Join-Path $Repo '.build-tools\w64devkit\bin'
    if (Test-Path -LiteralPath (Join-Path $cargoRoot 'bin\cargo.exe')) {
        $env:CARGO_HOME = $cargoRoot
        $env:PATH = (Join-Path $cargoRoot 'bin') + [IO.Path]::PathSeparator + $env:PATH
    }
    if (Test-Path -LiteralPath $rustupRoot -PathType Container) { $env:RUSTUP_HOME = $rustupRoot }
    if (Test-Path -LiteralPath $mingwBin -PathType Container) {
        $env:PATH = $mingwBin + [IO.Path]::PathSeparator + $env:PATH
    }
}
$python = Join-Path (Split-Path -Parent $PSScriptRoot) '.venv\Scripts\python.exe'
if (-not (Test-Path -LiteralPath $python)) { $python = (Get-Command python -ErrorAction Stop).Source }
$arguments = @('-X', 'utf8', (Join-Path $PSScriptRoot 'fork_sync.py'), $Action,
    '--repo', $Repo, '--profile', $Profile, '--fork-remote', $ForkRemote,
    '--upstream-remote', $UpstreamRemote)
if ($Run) { $arguments += @('--run', $Run) }
& $python @arguments
exit $LASTEXITCODE
