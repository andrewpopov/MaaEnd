[CmdletBinding()]
param(
    [string]$TargetRoot = 'I:\MaaEndData\Images',
    [string]$RepositoryRoot = (Split-Path -Parent $PSScriptRoot)
)
$ErrorActionPreference = 'Stop'
$repoPath = (Resolve-Path -LiteralPath $RepositoryRoot).Path.TrimEnd('\')
$targetPath = [IO.Path]::GetFullPath($TargetRoot).TrimEnd('\')
if ([IO.Path]::GetPathRoot($targetPath) -eq [IO.Path]::GetPathRoot($repoPath)) {
    throw 'Image storage must be on a different drive from the repository.'
}
if ($targetPath -eq [IO.Path]::GetPathRoot($targetPath).TrimEnd('\')) {
    throw 'Choose a dedicated image-storage folder, not a drive root.'
}
New-Item -ItemType Directory -Path $targetPath -Force | Out-Null
$stamp = Get-Date -Format 'yyyyMMdd-HHmmss'
$mappings = @(
    @{ Source = 'install\data\AccountScans\screenshots'; Target = 'AccountScans\screenshots' },
    @{ Source = 'install\data\AccountScans\inbox'; Target = 'AccountScans\inbox' },
    @{ Source = 'install\data\WorkflowRecordings'; Target = 'WorkflowRecordings' },
    @{ Source = 'install\data\RosterScans'; Target = 'RosterScans' },
    @{ Source = 'install\data\CombatTeams\captures'; Target = 'CombatTeams\captures' },
    @{ Source = 'install\debug\on_error'; Target = 'debug\on_error' }
)
$results = @()
foreach ($mapping in $mappings) {
    $source = [IO.Path]::GetFullPath((Join-Path $repoPath $mapping.Source))
    $destination = [IO.Path]::GetFullPath((Join-Path $targetPath $mapping.Target))
    if (!$source.StartsWith($repoPath + '\', [StringComparison]::OrdinalIgnoreCase) -or
        !$destination.StartsWith($targetPath + '\', [StringComparison]::OrdinalIgnoreCase)) {
        throw 'Resolved capture paths escaped their designated roots.'
    }
    New-Item -ItemType Directory -Path $destination -Force | Out-Null
    New-Item -ItemType Directory -Path (Split-Path -Parent $source) -Force | Out-Null
    if (!(Test-Path -LiteralPath $source)) {
        New-Item -ItemType Junction -Path $source -Target $destination | Out-Null
        $results += @{ source = $source; destination = $destination; files = 0; bytes = 0 }
        continue
    }
    $item = Get-Item -LiteralPath $source -Force
    if ($item.Attributes -band [IO.FileAttributes]::ReparsePoint) {
        if ([IO.Path]::GetFullPath([string]$item.Target).TrimEnd('\') -ne $destination) {
            throw "Existing link points somewhere else: $source"
        }
        Write-Output "Already linked: $source"
        continue
    }
    $hold = [IO.Path]::GetFullPath($source + '.image-migration-' + $stamp)
    # Both rename and cleanup targets must remain inside this checkout.
    if (!$hold.StartsWith($repoPath + '\', [StringComparison]::OrdinalIgnoreCase) -or
        (Test-Path -LiteralPath $hold)) { throw "Unsafe migration backup path: $hold" }
    & robocopy $source $destination /E /COPY:DAT /DCOPY:DAT /XJ /R:2 /W:1 /NFL /NDL /NJH /NJS /NP | Out-Null
    if ($LASTEXITCODE -ge 8) { throw "Initial copy failed: $source" }
    Move-Item -LiteralPath $source -Destination $hold
    try {
        New-Item -ItemType Junction -Path $source -Target $destination | Out-Null
    } catch {
        Move-Item -LiteralPath $hold -Destination $source
        throw
    }
    # Copy again after linking to include files written during the initial copy.
    & robocopy $hold $destination /E /COPY:DAT /DCOPY:DAT /XJ /R:2 /W:1 /NFL /NDL /NJH /NJS /NP | Out-Null
    if ($LASTEXITCODE -ge 8) { throw "Final copy failed; original files retained at $hold" }
    [long]$bytes = 0
    $files = @(Get-ChildItem -LiteralPath $hold -File -Recurse -Force)
    foreach ($file in $files) {
        $relative = $file.FullName.Substring($hold.Length).TrimStart('\')
        $copied = Join-Path $destination $relative
        if (!(Test-Path -LiteralPath $copied) -or
            (Get-FileHash -LiteralPath $file.FullName -Algorithm SHA256).Hash -ne
            (Get-FileHash -LiteralPath $copied -Algorithm SHA256).Hash) {
            throw "Verification failed; original files retained at $hold"
        }
        $bytes += $file.Length
    }
    $holdItem = Get-Item -LiteralPath $hold -Force
    if ($holdItem.Attributes -band [IO.FileAttributes]::ReparsePoint) { throw 'Refusing to delete a link.' }
    if ([IO.Path]::GetFullPath($holdItem.FullName) -ne $hold -or
        !$hold.StartsWith($repoPath + '\', [StringComparison]::OrdinalIgnoreCase)) {
        throw 'Migration cleanup path validation failed.'
    }
    Remove-Item -LiteralPath $hold -Recurse -Force
    $results += @{ source = $source; destination = $destination; files = $files.Count; bytes = $bytes }
    Write-Output ("Moved {0} files ({1:N1} MiB): {2}" -f $files.Count, ($bytes / 1MB), $mapping.Target)
}
$manifest = @{ created = (Get-Date).ToString('o'); repository = $repoPath; target = $targetPath; directories = $results }
$manifestPath = Join-Path $targetPath ('migration-' + $stamp + '.json')
$manifest | ConvertTo-Json -Depth 6 | Set-Content -LiteralPath $manifestPath -Encoding utf8
Write-Output "Migration manifest: $manifestPath"
