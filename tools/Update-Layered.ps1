[CmdletBinding()]
param(
    [ValidateSet('Check','Stage','Install')][string]$Action = 'Check',
    [string]$MxuRepo = (Join-Path (Split-Path -Parent (Split-Path -Parent $PSScriptRoot)) 'MXU'),
    [string]$InstallPath = (Join-Path (Split-Path -Parent $PSScriptRoot) 'install'),
    [string]$Resume,
    [long]$BuildRun,
    [string]$ArchivePath,
    [string]$ShortcutPath = (Join-Path $env:USERPROFILE 'OneDrive\Desktop\MaaEnd.lnk')
)
$ErrorActionPreference = 'Stop'
$repo = Split-Path -Parent $PSScriptRoot
$mxu = (Resolve-Path -LiteralPath $MxuRepo).Path
$toolRoot=$mxu
if (-not (Test-Path (Join-Path $toolRoot '.build-tools'))) { $toolRoot=Join-Path (Split-Path -Parent $repo) 'MXU' }
foreach ($directory in @((Join-Path $env:ProgramFiles 'Go\bin'),
    (Join-Path $env:USERPROFILE '.cache\codex-runtimes\codex-primary-runtime\dependencies\node\bin'),
    (Join-Path $toolRoot '.build-tools\cargo\bin'), (Join-Path $toolRoot '.build-tools\w64devkit\bin'))) {
    if (Test-Path -LiteralPath $directory -PathType Container) {
        $env:PATH = $directory + [IO.Path]::PathSeparator + $env:PATH
    }
}
if (Test-Path (Join-Path $toolRoot '.build-tools\cargo')) { $env:CARGO_HOME=Join-Path $toolRoot '.build-tools\cargo' }
if (Test-Path (Join-Path $toolRoot '.build-tools\rustup')) { $env:RUSTUP_HOME=Join-Path $toolRoot '.build-tools\rustup' }
$python=Join-Path $repo '.venv\Scripts\python.exe'
if (-not (Test-Path -LiteralPath $python)) { $python=(Get-Command python -ErrorAction Stop).Source }
function Invoke-Checked([string]$Directory,[string]$Program,[string[]]$Arguments) {
    Push-Location -LiteralPath $Directory
    try {
        Write-Host ('+ '+$Program+' '+($Arguments -join ' '))
        $result = & $Program @Arguments
        if ($LASTEXITCODE -ne 0) { $result | Out-Host; throw "$Program failed with exit code $LASTEXITCODE" }
        return $result
    } finally { Pop-Location }
}
function Get-GitOutput([string]$Directory,[string[]]$Arguments) {
    return ((Invoke-Checked $Directory 'git' $Arguments) -join "`n").Trim()
}
function New-GuiSnapshot([string]$SourceRepo,[string]$IndexFile) {
$oldIndex=$env:GIT_INDEX_FILE
try {
    $env:GIT_INDEX_FILE=$IndexFile
    Get-GitOutput $SourceRepo @('read-tree','HEAD') | Out-Host
    Get-GitOutput $SourceRepo @('add','-A','--','src','src-tauri','package.json','pnpm-lock.yaml','index.html','tsconfig.json','tsconfig.node.json','vite.config.ts','.gitignore') | Out-Host
    $tree=Get-GitOutput $SourceRepo @('write-tree')
    $commit=Get-GitOutput $SourceRepo @('commit-tree',$tree,'-p','HEAD','-m','chore: snapshot custom GUI layer for isolated update')
} finally {
    if ($null -eq $oldIndex) { Remove-Item Env:GIT_INDEX_FILE -ErrorAction SilentlyContinue } else { $env:GIT_INDEX_FILE=$oldIndex }
}
    return $commit
}
$forkUrl=Get-GitOutput $repo @('remote','get-url','origin')
if ($forkUrl -notmatch '^(?:https://github.com/|git@github.com:)([^/]+/[^/]+?)(?:\.git)?$') { throw 'origin must be our GitHub fork.' }
$fork=$Matches[1]
if ($fork -ieq 'MaaEnd/MaaEnd') { throw 'origin points to upstream, not our fork.' }
Get-GitOutput $repo @('fetch','--no-recurse-submodules','origin','refs/heads/v2:refs/remotes/origin/v2') | Out-Host
Get-GitOutput $repo @('fetch','--no-recurse-submodules','upstream','refs/heads/v2:refs/remotes/upstream/v2') | Out-Host
$missing=[int](Get-GitOutput $repo @('rev-list','--count','origin/v2..upstream/v2'))
$base=Get-GitOutput $repo @('rev-parse','origin/v2')
Write-Host "MaaEnd: $missing missing upstream commits; fork $base"
$mxuRemote='origin'
$mxuRemotes=(Get-GitOutput $mxu @('remote')) -split "`n"
if ('upstream' -in $mxuRemotes) { $mxuRemote='upstream' }
$mxuUrl=Get-GitOutput $mxu @('remote','get-url',$mxuRemote)
if ($mxuUrl -notin @('https://github.com/MistEO/MXU.git','https://github.com/MistEO/MXU','git@github.com:MistEO/MXU.git')) {
    throw 'MXU upstream remote is not MistEO/MXU. Configure its upstream remote first.'
}
Get-GitOutput $mxu @('fetch','--no-recurse-submodules',$mxuRemote,"refs/heads/main:refs/remotes/$mxuRemote/main") | Out-Host
$mxuMissing=Get-GitOutput $mxu @('rev-list','--count',"HEAD..$mxuRemote/main")
Write-Host "MXU: $mxuMissing upstream commits to merge with our local GUI layer."
if ($Action -eq 'Check') { return }
if ($missing -gt 0) {
    $openPrs=(((Invoke-Checked $repo 'gh' @('pr','list','--repo',$fork,'--base','v2','--json','headRefName,url')) -join "`n") | ConvertFrom-Json)
    $existing=@($openPrs | Where-Object { $_.headRefName -like 'codex/sync-maaend-*' })
    if ($existing.Count -gt 0) { throw "An upstream sync PR is already open: $($existing[0].url). Merge it after CI, then rerun." }
    Invoke-Checked $repo $python @('-X','utf8','tools/fork_sync.py','prepare') | Out-Host
    $latest=Get-ChildItem (Join-Path $repo '.cache\fork-sync') -Filter run.json -Recurse | Sort-Object LastWriteTime -Descending | Select-Object -First 1
    if (-not $latest) { throw 'Sync did not produce a manifest.' }
    Invoke-Checked $repo $python @('-X','utf8','tools/fork_sync.py','publish','--run',$latest.FullName) | Out-Host
    throw "Upstream sync PR prepared. Merge it after CI, then rerun this command. Manifest: $($latest.FullName)"
}
$inputs=@('assets','agent','tools/setup','tools/schema','tools/maadeps-download.py',
    'tools/check_maautils_sync.py','.github/workflows/install.yml','package.json','pnpm-lock.yaml','pyproject.toml','uv.lock')
if ($Resume) {
    $manifestPath=(Resolve-Path -LiteralPath $Resume).Path
    $manifest=Get-Content -LiteralPath $manifestPath -Raw | ConvertFrom-Json -AsHashtable
    $stage=Split-Path -Parent $manifestPath
    if ($manifest.installed) { throw 'This update was already installed.' }
    if ($manifest.fork_sha -ne $base) {
        & git -C $repo merge-base --is-ancestor $manifest.fork_sha $base
        if ($LASTEXITCODE -ne 0) { throw 'Fork history changed since staging. Start a fresh update.' }
        & git -C $repo diff --quiet $manifest.fork_sha $base -- @inputs
        if ($LASTEXITCODE -ne 0) { throw 'App build inputs changed since staging. Start a fresh update.' }
    }
    $package=Join-Path $stage 'package'
    $guiWork=$manifest.mxu_worktree
    if (-not $guiWork -or -not (Test-Path -LiteralPath $guiWork)) { throw 'No GUI worktree was prepared. Start a fresh stage.' }
    if ((Get-GitOutput $guiWork @('status','--porcelain','--untracked-files=no'))) { throw 'Commit GUI conflict resolutions before resuming.' }
    Get-GitOutput $guiWork @('merge-base','--is-ancestor',"$mxuRemote/main",'HEAD') | Out-Host
    $selected=[pscustomobject]@{headSha=$manifest.source_commit}
    function Save-Manifest { $manifest | ConvertTo-Json -Depth 6 | Set-Content -LiteralPath $manifestPath -Encoding utf8 }
} else {
# Accept an ancestor's successful package only when every build input is identical.
$runs=(((Invoke-Checked $repo 'gh' @('run','list','--repo',$fork,'--workflow','install.yml','--limit','30',
    '--json','databaseId,status,conclusion,headSha')) -join "`n") | ConvertFrom-Json)
if ($BuildRun) { $runs=@($runs | Where-Object { $_.databaseId -eq $BuildRun }) }
if ($ArchivePath -and -not $BuildRun) { throw '-ArchivePath requires the exact -BuildRun that produced it.' }
$selected=$null
foreach ($run in $runs) {
    if ($run.status -ne 'completed' -or $run.conclusion -ne 'success') { continue }
    & git -C $repo merge-base --is-ancestor $run.headSha $base
    if ($LASTEXITCODE -ne 0) { continue }
    & git -C $repo diff --quiet $run.headSha $base -- @inputs
    if ($LASTEXITCODE -eq 0) { $selected=$run; break }
    if ($LASTEXITCODE -gt 1) { throw 'Could not compare package build inputs.' }
}
if (-not $selected) {
    if ($BuildRun) { throw 'Requested build is not a successful matching ancestor build.' }
    Invoke-Checked $repo 'gh' @('workflow','run','install.yml','--repo',$fork,'--ref','v2','-f','ref=v2') | Out-Host
    throw 'A matching package is not ready. CI build requested; rerun after it succeeds.'
}
$stamp=Get-Date -Format 'yyyyMMdd-HHmmss-fff'
$stage=Join-Path $repo ('.cache\layered-update\'+$stamp)
New-Item -ItemType Directory -Path $stage | Out-Null
$manifest=[ordered]@{fork=$fork; fork_sha=$base; artifact_run=$selected.databaseId; source_commit=$selected.headSha; installed=$false}
$manifestPath=Join-Path $stage 'update.json'
function Save-Manifest { $manifest | ConvertTo-Json -Depth 6 | Set-Content -LiteralPath $manifestPath -Encoding utf8 }
Save-Manifest
if ($ArchivePath) {
    $zip=(Resolve-Path -LiteralPath $ArchivePath).Path
    Write-Host 'Using operator-supplied cached artifact; its provenance must match -BuildRun.'
} else {
    Invoke-Checked $repo 'gh' @('run','download',[string]$selected.databaseId,'--repo',$fork,'--name','MaaEnd-win-x86_64-zip','--dir',(Join-Path $stage 'download')) | Out-Host
    $zip=Join-Path $stage 'download\MaaEnd-win-x86_64.zip'
}
$manifest.archive_sha256=(Get-FileHash -LiteralPath $zip).Hash
$package=Join-Path $stage 'package'
Expand-Archive -LiteralPath $zip -DestinationPath $package
# Snapshot tracked and untracked GUI source without touching the user's Git index.
$snapshot=New-GuiSnapshot $mxu (Join-Path $stage 'mxu-snapshot.index')
$guiWork=Join-Path $stage 'mxu'
$guiBranch='codex/layered-mxu-'+$stamp
Get-GitOutput $mxu @('worktree','add','-b',$guiBranch,$guiWork,$snapshot) | Out-Host
$manifest.mxu_worktree=$guiWork
$manifest.mxu_snapshot=$snapshot
Save-Manifest
try {
    Get-GitOutput $guiWork @('merge','--no-ff','-m','chore: merge MXU upstream into custom GUI layer',"$mxuRemote/main") | Out-Host
} catch {
    Write-Host "Resolve and commit conflicts in $guiWork; resume with -Resume '$manifestPath'."
    throw
}
}
$manifest.mxu_commit=Get-GitOutput $guiWork @('rev-parse','HEAD')
Save-Manifest
Invoke-Checked $guiWork 'pnpm' @('install','--frozen-lockfile') | Out-Host
Invoke-Checked $guiWork 'pnpm' @('test') | Out-Host
Invoke-Checked $guiWork 'pnpm' @('build') | Out-Host
# Reuse toolchain caches, but build the custom source with production Tauri assets.
$oldTarget=$env:CARGO_TARGET_DIR
$oldDebug=$env:CARGO_PROFILE_RELEASE_DEBUG
$oldFramework=$env:MXU_PAUSE_TEST_FRAMEWORK
try {
    $env:CARGO_TARGET_DIR=Join-Path $toolRoot 'src-tauri\target'
    $env:CARGO_PROFILE_RELEASE_DEBUG='0'
    $env:MXU_PAUSE_TEST_FRAMEWORK=Join-Path $package 'maafw'
    Invoke-Checked $guiWork 'cargo' @('test','--manifest-path','src-tauri/Cargo.toml','--test','task_pause_framework','--','--ignored') | Out-Host
    Invoke-Checked $guiWork 'cargo' @('build','--release','--features','tauri/custom-protocol','--manifest-path','src-tauri/Cargo.toml') | Out-Host
    $gui=Join-Path $stage 'custom-MaaEnd.exe'
    Copy-Item -LiteralPath (Join-Path $env:CARGO_TARGET_DIR 'release\mxu.exe') -Destination $gui
} finally {
    foreach ($setting in @(@('CARGO_TARGET_DIR',$oldTarget),@('CARGO_PROFILE_RELEASE_DEBUG',$oldDebug),@('MXU_PAUSE_TEST_FRAMEWORK',$oldFramework))) {
        if ($null -eq $setting[1]) { Remove-Item ('Env:'+$setting[0]) -ErrorAction SilentlyContinue } else { Set-Item ('Env:'+$setting[0]) $setting[1] }
    }
}
$manifest.gui_sha256=(Get-FileHash -LiteralPath $gui).Hash
$manifest.compatibility_test='passed: queue preservation and stop while paused'
Save-Manifest
$installerArgs=@{PackagePath=$package; GuiPath=$gui; InstallPath=$InstallPath; SourceCommit=$selected.headSha}
if ($Action -eq 'Install') { $installerArgs.Apply=$true }
& (Join-Path $PSScriptRoot 'Install-Layered.ps1') @installerArgs
if (-not $?) { throw 'Layered installer failed.' }
if ($Action -eq 'Install') {
    $manifest.installed=$true
    Save-Manifest
    $shell=New-Object -ComObject WScript.Shell
    $link=$shell.CreateShortcut($ShortcutPath)
    $link.TargetPath=Join-Path $InstallPath 'MaaEnd.exe'
    $link.WorkingDirectory=$InstallPath
    $link.Save()
    Write-Host 'Installed; desktop shortcut verified. Start MaaEnd from the shortcut.'
} else { Write-Host 'Staged and tested. App files have not been replaced.' }
Write-Host "Update manifest: $manifestPath"
