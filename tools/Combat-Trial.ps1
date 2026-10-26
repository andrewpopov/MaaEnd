param(
    [Parameter(Mandatory=$true)][ValidateSet('Init','Preflight','Score','RecordContext')][string]$Mode,
    [Parameter(Mandatory=$true)][string]$Output,
    [string]$Profile = 'heat-si',
    [string]$Candidate,
    [string]$Instance,
    [int]$Duration = 120
)
$ErrorActionPreference = 'Stop'
$repoRoot = Split-Path -Parent $PSScriptRoot
$trialTool = Join-Path $PSScriptRoot 'combat_trial.py'
$profilePath = Join-Path $repoRoot "assets/data/CombatTeams/$Profile.json"
if ($Profile -notmatch '^[a-z0-9][a-z0-9-]{0,63}$') { throw 'Invalid profile ID' }
switch ($Mode) {
    Init { & python $trialTool init --profile $profilePath --output $Output }
    Preflight {
        if (-not $Candidate) { throw 'Preflight requires -Candidate' }
        & python $trialTool preflight --repo $repoRoot --profile $profilePath --candidate $Candidate --installed (Join-Path $repoRoot 'install/agent/go-service.exe') --output (Join-Path $Output 'preflight.json')
    }
    Score { & python $trialTool score --session $Output }
    RecordContext {
        if (-not $Instance) { throw 'RecordContext requires -Instance' }
        if ($Duration -lt 1 -or $Duration -gt 1200) { throw 'Duration must be within the 20-minute budget' }
        # Context-only cached stream; use actual combat-loop traces or video for timing.
        & python (Join-Path $PSScriptRoot 'record_workflow.py') --api-url 'http://127.0.0.1:12701/api' --instance $Instance --duration $Duration --output (Join-Path $Output 'context')
    }
}
if ($LASTEXITCODE -ne 0) { throw "Combat trial command failed ($LASTEXITCODE)" }
