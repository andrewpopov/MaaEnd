$ErrorActionPreference='Stop'
$scriptPath=Join-Path $PSScriptRoot 'Update-Layered.ps1'
$errors=$null
$ast=[Management.Automation.Language.Parser]::ParseFile($scriptPath,[ref]$null,[ref]$errors)
if ($errors) { throw ($errors | Out-String) }
$names=@('Invoke-Checked','Get-GitOutput','New-GuiSnapshot')
foreach ($definition in $ast.FindAll({param($node) $node -is [Management.Automation.Language.FunctionDefinitionAst]},$true)) {
    if ($definition.Name -in $names) { . ([scriptblock]::Create($definition.Extent.Text)) }
}
$root=Join-Path $env:TEMP ('maaend-snapshot-test-'+[guid]::NewGuid().ToString('N'))
New-Item -ItemType Directory -Path $root | Out-Null
Get-GitOutput $root @('init','-b','main') | Out-Host
Get-GitOutput $root @('config','user.name','Snapshot test') | Out-Host
Get-GitOutput $root @('config','user.email','snapshot@example.invalid') | Out-Host
foreach ($name in @('.gitignore','package.json','pnpm-lock.yaml','index.html','tsconfig.json','tsconfig.node.json','vite.config.ts','src/pause.ts','src-tauri/pause.rs')) {
    $file=Join-Path $root $name
    New-Item -ItemType Directory -Force -Path (Split-Path -Parent $file) | Out-Null
    Set-Content -LiteralPath $file -Value 'base'
}
Get-GitOutput $root @('add','.') | Out-Host
Get-GitOutput $root @('commit','-m','base') | Out-Host
$head=Get-GitOutput $root @('rev-parse','HEAD')
Set-Content (Join-Path $root 'src/pause.ts') 'staged-version'
Get-GitOutput $root @('add','src/pause.ts') | Out-Host
$indexHash=(Get-FileHash (Join-Path $root '.git/index')).Hash
Set-Content (Join-Path $root 'src/pause.ts') 'working-version'
Set-Content (Join-Path $root 'src/new.ts') 'untracked-custom-feature'
$envBefore=$env:GIT_INDEX_FILE
$commit=New-GuiSnapshot $root (Join-Path $root '.git/snapshot-test.index')
if ((Get-FileHash (Join-Path $root '.git/index')).Hash -ne $indexHash) { throw 'User Git index changed' }
if ((Get-GitOutput $root @('rev-parse','HEAD')) -ne $head) { throw 'User branch moved' }
if ($env:GIT_INDEX_FILE -ne $envBefore) { throw 'Index environment was not restored' }
if ((Get-GitOutput $root @('show',"${commit}:src/pause.ts")) -ne 'working-version') { throw 'Working edit not captured' }
if ((Get-GitOutput $root @('show',"${commit}:src/new.ts")) -ne 'untracked-custom-feature') { throw 'Untracked GUI feature lost' }
$failed=$false
try { Get-GitOutput $root @('rev-parse','refs/heads/does-not-exist') | Out-Host } catch { $failed=$true }
if (-not $failed) { throw 'Command failure did not stop updater' }
Write-Output 'PASS: GUI snapshot preserves index, branch, environment, staged edits and untracked features; failed commands stop.'
$resolved=[IO.Path]::GetFullPath($root)
$tempPrefix=[IO.Path]::GetFullPath($env:TEMP).TrimEnd('\')+'\'
if (-not $resolved.StartsWith($tempPrefix,[StringComparison]::OrdinalIgnoreCase) -or (Split-Path -Leaf $resolved) -notlike 'maaend-snapshot-test-*') { throw 'Unexpected fixture cleanup path' }
Remove-Item -LiteralPath $resolved -Recurse -Force
