$ErrorActionPreference='Stop'
# Mock process discovery so fixture-only tests can run while the real app is open.
function Get-Process { [CmdletBinding()] param([string[]]$Name); return @() }
function Get-CimInstance { [CmdletBinding()] param([string]$ClassName,[string]$Filter); return @() }
$installer=Join-Path $PSScriptRoot 'Install-Layered.ps1'
# Inject corruption only into a temporary fixture after its agent folder moves.
function Move-Item {
    [CmdletBinding()] param([string]$LiteralPath,[string]$Destination)
    Microsoft.PowerShell.Management\Move-Item -LiteralPath $LiteralPath -Destination $Destination
    if ($global:InstallFixtureCorruptMovedRuntime -and $Destination -eq (Join-Path $global:InstallFixtureAppUnderTest 'agent')) {
        Set-Content -LiteralPath (Join-Path $Destination 'MaaUtils.dll') -Value 'corrupted-during-install'
        $global:InstallFixtureCorruptMovedRuntime=$false
    }
}
$root=Join-Path $env:TEMP ('maaend-layer-test-'+[guid]::NewGuid().ToString('N'))
foreach($case in @('success','rollback','corrupt-runtime')) {
    $base=Join-Path $root $case; $pkg=Join-Path $base 'package'; $app=Join-Path $base 'install'; $gui=Join-Path $base 'custom.exe'
    foreach($relative in @('MaaEnd.exe','interface.json','agent/go-service.exe','agent/cpp-algo.exe','maafw/MaaFramework.dll','agent/MaaAgentServer.dll','agent/MaaUtils.dll','agent/onnxruntime_maa.dll','agent/opencv_world4_maa.dll','agent/WebView2Loader.dll','resource/new.json','data/MapNavigator/static.json','tools/combat_team.py')) {
        $p=Join-Path $pkg $relative; New-Item -ItemType Directory -Force -Path (Split-Path -Parent $p)|Out-Null; Set-Content -LiteralPath $p -Value "new-$relative"
    }
    foreach($relative in @('MaaEnd.exe','interface.json','agent/go-service.exe','agent/cpp-algo.exe','maafw/MaaFramework.dll','resource/retired.json','data/MapNavigator/static.json','config/mxu-MaaEnd.json','config/maa_option.json','data/AutoCollect/history.json','data/AccountScans/account_scans.sqlite3','EssenceInventory.json')) {
        $p=Join-Path $app $relative; New-Item -ItemType Directory -Force -Path (Split-Path -Parent $p)|Out-Null; Set-Content -LiteralPath $p -Value "old-$relative"
    }
    $global:InstallFixtureAppUnderTest=$app
    $global:InstallFixtureCorruptMovedRuntime=$case -eq 'corrupt-runtime'
    Set-Content -LiteralPath $gui -Value 'custom-pause-gui'
    if($case -eq 'success') {
        $runtime=Join-Path $pkg 'agent/MaaAgentServer.dll'
        Remove-Item -LiteralPath $runtime
        $rejected=$false
        try { & $installer -PackagePath $pkg -GuiPath $gui -InstallPath $app -SourceCommit fixture -Apply } catch { $rejected=$true }
        if(-not $rejected){throw 'Package missing C++ runtime must be rejected'}
        if((Get-Content (Join-Path $app 'MaaEnd.exe')).Trim() -ne 'old-MaaEnd.exe'){throw 'Incomplete package modified installation'}
        Set-Content -LiteralPath $runtime -Value 'new-agent/MaaAgentServer.dll'
        Write-Output 'PASS missing-runtime rejection'
        $account=Join-Path $pkg 'data/AccountScans/account_scans.sqlite3'
        New-Item -ItemType Directory -Force -Path (Split-Path -Parent $account)|Out-Null
        Set-Content -LiteralPath $account -Value 'must-not-overwrite-account'
        $rejected=$false
        try { & $installer -PackagePath $pkg -GuiPath $gui -InstallPath $app -SourceCommit fixture -Apply } catch { $rejected=$true }
        if(-not $rejected){throw 'Package carrying account data must be rejected'}
        Remove-Item -LiteralPath $account
        $rejected=$false
        try { & $installer -PackagePath $app -GuiPath $gui -InstallPath $app -SourceCommit fixture -Apply } catch { $rejected=$_.Exception.Message -like '*must not overlap*' }
        if(-not $rejected){throw 'Overlapping package/install directories must be rejected'}
        Write-Output 'PASS account overwrite and overlapping-path rejection'

    }

    if($case -eq 'rollback') { $lock=[IO.File]::Open((Join-Path $app 'interface.json'),[IO.FileMode]::Open,[IO.FileAccess]::ReadWrite,[IO.FileShare]::None) }
    $failed=$false
    try { & $installer -PackagePath $pkg -GuiPath $gui -InstallPath $app -SourceCommit fixture -Apply } catch { $failed=$true; if($case -eq 'success'){throw} } finally { if($lock){$lock.Dispose();$lock=$null} }
    if($case -in @('rollback','corrupt-runtime') -and -not $failed){throw 'Expected installation failure and rollback'}
    if((Get-Content (Join-Path $app 'config/mxu-MaaEnd.json')).Trim() -ne 'old-config/mxu-MaaEnd.json'){throw 'Configuration changed'}
    if((Get-Content (Join-Path $app 'data/AutoCollect/history.json')).Trim() -ne 'old-data/AutoCollect/history.json'){throw 'History changed'}
    foreach($relative in @('config/maa_option.json','data/AccountScans/account_scans.sqlite3','EssenceInventory.json')) {
        if((Get-Content (Join-Path $app $relative)).Trim() -ne ('old-'+$relative)){throw ('Protected file changed: '+$relative)}
    }
    if($case -eq 'success') {
        if((Get-Content (Join-Path $app 'MaaEnd.exe')).Trim() -ne 'custom-pause-gui'){throw 'Custom GUI lost'}
        if(-not (Test-Path (Join-Path $app 'agent/MaaAgentServer.dll'))){throw 'C++ runtime DLL not installed'}
        if(Test-Path (Join-Path $app 'resource/retired.json')){throw 'Stale resource retained'}
        if(-not (Test-Path (Join-Path $app 'tools/combat_team.py'))){throw 'Planner tool not installed'}
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

Remove-Variable InstallFixtureAppUnderTest,InstallFixtureCorruptMovedRuntime -Scope Global -ErrorAction SilentlyContinue
