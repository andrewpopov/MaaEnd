$ErrorActionPreference='Stop'
# Mock process discovery so fixture-only tests can run while the real app is open.
function Get-Process { [CmdletBinding()] param([string[]]$Name); return @() }
$installer=Join-Path $PSScriptRoot 'Install-Layered.ps1'
$root=Join-Path $env:TEMP ('maaend-layer-test-'+[guid]::NewGuid().ToString('N'))
foreach($case in @('success','rollback')) {
    $base=Join-Path $root $case; $pkg=Join-Path $base 'package'; $app=Join-Path $base 'install'; $gui=Join-Path $base 'custom.exe'
    foreach($relative in @('MaaEnd.exe','interface.json','agent/go-service.exe','agent/cpp-algo.exe','maafw/MaaFramework.dll','resource/new.json','data/MapNavigator/static.json')) {
        $p=Join-Path $pkg $relative; New-Item -ItemType Directory -Force -Path (Split-Path -Parent $p)|Out-Null; Set-Content -LiteralPath $p -Value "new-$relative"
    }
    foreach($relative in @('MaaEnd.exe','interface.json','agent/go-service.exe','agent/cpp-algo.exe','maafw/MaaFramework.dll','resource/retired.json','data/MapNavigator/static.json','config/mxu-MaaEnd.json','data/AutoCollect/history.json')) {
        $p=Join-Path $app $relative; New-Item -ItemType Directory -Force -Path (Split-Path -Parent $p)|Out-Null; Set-Content -LiteralPath $p -Value "old-$relative"
    }
    Set-Content -LiteralPath $gui -Value 'custom-pause-gui'
    if($case -eq 'rollback') { $lock=[IO.File]::Open((Join-Path $app 'interface.json'),[IO.FileMode]::Open,[IO.FileAccess]::ReadWrite,[IO.FileShare]::None) }
    $failed=$false
    try { & $installer -PackagePath $pkg -GuiPath $gui -InstallPath $app -SourceCommit fixture -Apply } catch { $failed=$true; if($case -eq 'success'){throw} } finally { if($lock){$lock.Dispose();$lock=$null} }
    if($case -eq 'rollback' -and -not $failed){throw 'Expected locked-file failure'}
    if((Get-Content (Join-Path $app 'config/mxu-MaaEnd.json')).Trim() -ne 'old-config/mxu-MaaEnd.json'){throw 'Configuration changed'}
    if((Get-Content (Join-Path $app 'data/AutoCollect/history.json')).Trim() -ne 'old-data/AutoCollect/history.json'){throw 'History changed'}
    if($case -eq 'success') {
        if((Get-Content (Join-Path $app 'MaaEnd.exe')).Trim() -ne 'custom-pause-gui'){throw 'Custom GUI lost'}
        if(Test-Path (Join-Path $app 'resource/retired.json')){throw 'Stale resource retained'}
    } else {
        if((Get-Content (Join-Path $app 'MaaEnd.exe')).Trim() -ne 'old-MaaEnd.exe'){throw 'Rollback did not restore GUI'}
        if(-not (Test-Path (Join-Path $app 'resource/retired.json'))){throw 'Rollback did not restore resource'}
        if((Get-Content (Join-Path $app 'agent/go-service.exe')).Trim() -ne 'old-agent/go-service.exe'){throw 'Rollback did not restore agent'}
    }
    Write-Output "PASS $case"
}
$resolvedFixture = [IO.Path]::GetFullPath($root)
$tempPrefix = [IO.Path]::GetFullPath($env:TEMP).TrimEnd('\') + '\'
if (-not $resolvedFixture.StartsWith($tempPrefix, [StringComparison]::OrdinalIgnoreCase) -or (Split-Path -Leaf $resolvedFixture) -notlike 'maaend-layer-test-*') { throw 'Unexpected fixture cleanup path' }
Remove-Item -LiteralPath $resolvedFixture -Recurse -Force
