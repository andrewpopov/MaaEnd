[CmdletBinding()]
param(
    [Parameter(Mandatory)][string]$PackagePath,
    [Parameter(Mandatory)][string]$GuiPath,
    [string]$InstallPath = (Join-Path (Split-Path -Parent $PSScriptRoot) 'install'),
    [Parameter(Mandatory)][string]$SourceCommit,
    [switch]$Apply
)
$ErrorActionPreference = 'Stop'
$package = (Resolve-Path -LiteralPath $PackagePath).Path
$install = (Resolve-Path -LiteralPath $InstallPath).Path
$gui = (Resolve-Path -LiteralPath $GuiPath).Path
function Assert-Within([string]$Root, [string]$Path) {
    $absolute = [IO.Path]::GetFullPath($Path)
    $prefix = $Root.TrimEnd('\', '/') + [IO.Path]::DirectorySeparatorChar
    if (-not $absolute.StartsWith($prefix, [StringComparison]::OrdinalIgnoreCase)) {
        throw "Path is outside its intended directory: $absolute"
    }
}
if (Get-Process MaaEnd -ErrorAction SilentlyContinue) {
    throw 'Close MaaEnd before installation. Running task and agent processes must finish first.'
}
# A leftover child agent can still hold DLLs and write history after the GUI closes.
foreach ($process in Get-CimInstance Win32_Process -Filter "Name='go-service.exe' OR Name='cpp-algo.exe'") {
    if (-not $process.ExecutablePath) { throw 'Cannot verify the path of a running agent. Close it first.' }
    if ($process.ExecutablePath.StartsWith($install + '\', [StringComparison]::OrdinalIgnoreCase)) {
        throw "An installed agent is still running: $($process.ProcessId)"
    }
}
$allowed = @('agent','locales','maafw','resource','resource_adb','resource_cloud_adb',
    'resource_linux','resource_macos','resource_playcover','tasks','interface.json',
    'LICENSE','README.md','MaaEnd.exe','data')
foreach ($item in Get-ChildItem -LiteralPath $package) {
    if ($item.Name -notin $allowed) { throw "Unexpected package entry: $($item.Name)" }
}
foreach ($required in @('MaaEnd.exe','interface.json','agent\go-service.exe','agent\cpp-algo.exe','maafw\MaaFramework.dll')) {
    if (-not (Test-Path -LiteralPath (Join-Path $package $required) -PathType Leaf)) {
        throw "Incomplete package: $required"
    }
}
$backup = Join-Path $install ('backups\layered-' + (Get-Date -Format 'yyyyMMdd-HHmmss-fff'))
Assert-Within $install $backup
$operations = @()
foreach ($item in Get-ChildItem -LiteralPath $package) {
    if ($item.Name -eq 'data') {
        # Overlay shipped static data files; preserve account history and other runtime files.
        foreach ($file in Get-ChildItem -LiteralPath $item.FullName -File -Recurse) {
            $relative = $file.FullName.Substring($package.Length + 1)
            if ($relative -match '^data[\\/]AutoCollect[\\/]') { throw 'Package attempts to replace collection history.' }
            $operations += [pscustomobject]@{source=$file.FullName; relative=$relative; oldMoved=$false; newMoved=$false}
        }
    } else {
        $operations += [pscustomobject]@{source=$item.FullName; relative=$item.Name; oldMoved=$false; newMoved=$false}
    }
}
foreach ($op in $operations) {
    Assert-Within $package $op.source
    Assert-Within $install (Join-Path $install $op.relative)
    Assert-Within $backup (Join-Path $backup $op.relative)
}
$preserved = @{}
foreach ($relative in @('config\mxu-MaaEnd.json','data\AutoCollect\history.json')) {
    $path = Join-Path $install $relative
    if (Test-Path -LiteralPath $path -PathType Leaf) { $preserved[$relative] = (Get-FileHash -LiteralPath $path).Hash }
}
$manifest = [ordered]@{
    source_commit=$SourceCommit; package=$package; custom_gui=$gui
    custom_gui_sha256=(Get-FileHash -LiteralPath $gui).Hash
    go_agent_sha256=(Get-FileHash -LiteralPath (Join-Path $package 'agent\go-service.exe')).Hash
    cpp_agent_sha256=(Get-FileHash -LiteralPath (Join-Path $package 'agent\cpp-algo.exe')).Hash
    framework_sha256=(Get-FileHash -LiteralPath (Join-Path $package 'maafw\MaaFramework.dll')).Hash
    preserved=$preserved; backup=$backup; installed=$false
}
Write-Output "Layered installation: $($operations.Count) package entries; custom GUI $($manifest.custom_gui_sha256)"
Write-Output "Backup: $backup"
if (-not $Apply) { Write-Output 'Preview only. Add -Apply to install while MaaEnd is closed.'; return }
New-Item -ItemType Directory -Path $backup | Out-Null
$manifestPath = Join-Path $backup 'layers.json'
$manifest | ConvertTo-Json -Depth 5 | Set-Content -LiteralPath $manifestPath -Encoding utf8
try {
    Copy-Item -LiteralPath $gui -Destination (Join-Path $package 'MaaEnd.exe') -Force
    foreach ($op in $operations) {
        $target = Join-Path $install $op.relative
        $old = Join-Path $backup $op.relative
        New-Item -ItemType Directory -Force -Path (Split-Path -Parent $old) | Out-Null
        New-Item -ItemType Directory -Force -Path (Split-Path -Parent $target) | Out-Null
        if (Test-Path -LiteralPath $target) {
            Move-Item -LiteralPath $target -Destination $old
            $op.oldMoved = $true
        }
        Move-Item -LiteralPath $op.source -Destination $target
        $op.newMoved = $true
    }
    foreach ($relative in $preserved.Keys) {
        if ((Get-FileHash -LiteralPath (Join-Path $install $relative)).Hash -ne $preserved[$relative]) {
            throw "Preserved user file changed: $relative"
        }
    }
    if ((Get-FileHash -LiteralPath (Join-Path $install 'MaaEnd.exe')).Hash -ne $manifest.custom_gui_sha256) {
        throw 'Installed GUI does not match our custom layer.'
    }
    $manifest.installed = $true
    $manifest | ConvertTo-Json -Depth 5 | Set-Content -LiteralPath $manifestPath -Encoding utf8
    Write-Output "Installed. Layer manifest: $manifestPath"
} catch {
    $failure = $_
    [array]::Reverse($operations)
    foreach ($op in $operations) {
        $target = Join-Path $install $op.relative
        if ($op.newMoved) { Move-Item -LiteralPath $target -Destination $op.source }
        if ($op.oldMoved) { Move-Item -LiteralPath (Join-Path $backup $op.relative) -Destination $target }
    }
    throw $failure
}
