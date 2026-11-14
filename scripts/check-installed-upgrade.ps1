param(
  [Parameter(Mandatory = $true)][string]$CandidateSetup,
  [Parameter(Mandatory = $true)][string]$CandidateExe,
  [Parameter(Mandatory = $true)][string]$Version,
  [switch]$LiveUpdate,
  [switch]$RequireSigningContinuity
)
$ErrorActionPreference = 'Stop'
if ($env:GITHUB_ACTIONS -ne 'true' -or $env:RUNNER_ENVIRONMENT -ne 'github-hosted' -or $env:RUNNER_OS -ne 'Windows' -or -not $env:RUNNER_TEMP) {
  throw 'This installer smoke requires a disposable GitHub-hosted Actions runner'
}
if (Get-Process -Name Yozora -ErrorAction SilentlyContinue) { throw 'Unexpected existing Yozora process on runner' }
$CandidateSetup = (Resolve-Path $CandidateSetup).Path
$CandidateExe = (Resolve-Path $CandidateExe).Path
$root = Join-Path $env:RUNNER_TEMP ('yozora-upgrade-' + [guid]::NewGuid().ToString('N'))
$install = Join-Path $root 'install'
$oldAppdata, $oldLocal = $env:APPDATA, $env:LOCALAPPDATA
$env:APPDATA = Join-Path $root 'roaming'
$env:LOCALAPPDATA = Join-Path $root 'local'
New-Item -ItemType Directory -Force $root, $env:APPDATA, $env:LOCALAPPDATA | Out-Null
$exe = Join-Path $install 'Yozora.exe'
$baseline = Join-Path $root 'baseline'
New-Item -ItemType Directory $baseline | Out-Null
function Wait-For([scriptblock]$condition, [int]$seconds, [string]$failure) {
  $until = [DateTime]::UtcNow.AddSeconds($seconds)
  do { if (& $condition) { return }; Start-Sleep -Seconds 1 } while ([DateTime]::UtcNow -lt $until)
  throw $failure
}
function Hash([string]$path) { (Get-FileHash -Algorithm SHA256 $path).Hash }
function Check-Sums([string]$dir) {
  $sums = @{}
  foreach ($line in Get-Content (Join-Path $dir 'SHA256SUMS.txt')) {
    if ($line -match '^([0-9a-fA-F]{64})\s+\*?(Yozora(?:-Setup)?\.exe)$') { $sums[$Matches[2]] = $Matches[1] }
  }
  foreach ($name in @('Yozora-Setup.exe', 'Yozora.exe')) {
    if (-not $sums.ContainsKey($name) -or (Hash (Join-Path $dir $name)) -ne $sums[$name]) { throw "Published checksum mismatch or missing entry: $name" }
  }
}
function Check-Marker {
  if ((Get-Content $config -Raw) -notmatch 'ci-upgrade-marker-731' -or (Hash $config) -ne $configHash) { throw 'Settings file changed during the upgrade' }
}
try {
  foreach ($name in @('Yozora-Setup.exe', 'Yozora.exe', 'SHA256SUMS.txt')) {
    Invoke-WebRequest -Uri "https://github.com/mariyaholic/yozora/releases/download/v0.2.1/$name" -OutFile (Join-Path $baseline $name) -TimeoutSec 120
  }
  Check-Sums $baseline
  if ($LiveUpdate -or $RequireSigningContinuity) { Check-Sums (Split-Path $CandidateExe) }
  $setupVersion = (Get-Item $CandidateSetup).VersionInfo.ProductVersion
  $setupParts = [version]$setupVersion
  if ("$($setupParts.Major).$($setupParts.Minor).$($setupParts.Build)" -ne $Version -or $setupParts.Revision -gt 0) { throw "Candidate installer version $setupVersion does not match $Version" }
  if ($RequireSigningContinuity) {
    $baseSignature = Get-AuthenticodeSignature (Join-Path $baseline 'Yozora.exe')
    $exeSignature = Get-AuthenticodeSignature $CandidateExe
    $setupSignature = Get-AuthenticodeSignature $CandidateSetup
    foreach ($sig in @($baseSignature, $exeSignature, $setupSignature)) {
      if ($sig.Status -ne 'Valid' -and $sig.Status -ne 'NotSigned') { throw "A release signature is present but not valid: $($sig.Status)" }
      if ($sig.Status -eq 'Valid' -and -not $sig.SignerCertificate.Subject) { throw 'Signing subject is missing' }
    }
    if ($exeSignature.Status -eq 'Valid' -or $setupSignature.Status -eq 'Valid' -or $baseSignature.Status -eq 'Valid') {
      if ($exeSignature.Status -ne 'Valid' -or $setupSignature.Status -ne 'Valid' -or $exeSignature.SignerCertificate.Subject -ine $setupSignature.SignerCertificate.Subject) { throw 'Candidate executable and installer must have matching valid publisher signatures' }
      if ($baseSignature.Status -eq 'Valid' -and $baseSignature.SignerCertificate.Subject -ine $setupSignature.SignerCertificate.Subject) { throw 'Candidate signing does not match the signed baseline publisher' }
    }
  }
  $p = Start-Process -FilePath (Join-Path $baseline 'Yozora-Setup.exe') -ArgumentList @('/VERYSILENT','/SUPPRESSMSGBOXES','/NORESTART','/NORESTARTAPPLICATIONS',("/DIR=`"$install`"")) -Wait -PassThru
  if ($p.ExitCode -ne 0) { throw "Baseline setup failed: $($p.ExitCode)" }
  if ((Hash $exe) -ne (Hash (Join-Path $baseline 'Yozora.exe'))) { throw 'Baseline installed executable differs from published asset' }
  $configDir = Join-Path $env:APPDATA 'uika-resonance'
  New-Item -ItemType Directory -Force $configDir | Out-Null
  $config = Join-Path $configDir 'cadence.toml'
  $enabled = if ($LiveUpdate) { 'true' } else { 'false' }
  @"
[server]
enabled = true
[spotify]
mode = "system"
[update]
check = $enabled
[template]
details = "ci-upgrade-marker-731"
"@ | Set-Content -Encoding utf8 $config
  $configHash = Hash $config
  # The GUI binary may attach to its parent's console and print its dashboard
  # URL. Give it a hidden, separate console so its session token never enters CI logs.
  $launcher = Start-Process -FilePath $env:ComSpec -ArgumentList @('/d', '/c', 'start', '""', '/wait', ('"{0}"' -f $exe), 'serve') -WindowStyle Hidden -PassThru
  $endpointFile = Join-Path $env:LOCALAPPDATA 'uika-resonance\dashboard.json'
  Wait-For { Test-Path $endpointFile } 60 'Baseline dashboard did not start'
  $endpoint = Get-Content $endpointFile -Raw | ConvertFrom-Json
  $old = Get-Process -Id $endpoint.pid -ErrorAction Stop
  $oldProcess = Get-CimInstance Win32_Process -Filter "ProcessId=$($old.Id)"
  if ($oldProcess.ExecutablePath -ne $exe) { throw 'Baseline dashboard process is not the isolated installation' }
  $baseStatus = Invoke-RestMethod -Uri ($endpoint.base_url + '/api/update') -Headers @{ 'X-Uika-Token' = $endpoint.token } -TimeoutSec 5
  if ($baseStatus.version -ne '0.2.1') { throw 'Installed baseline did not report version 0.2.1' }
  # The setup's restart decision depends on the tray host existing, not merely the dashboard.
  Add-Type -TypeDefinition 'using System; using System.Runtime.InteropServices; public static class TrayProbe { [DllImport("user32.dll", CharSet=CharSet.Unicode)] public static extern IntPtr FindWindow(string c, string n); }'
  Wait-For { [TrayProbe]::FindWindow('YozoraTrayHost', $null) -ne [IntPtr]::Zero } 30 'Baseline tray host did not start'
  if ($LiveUpdate) {
    $uri = $endpoint.base_url + '/api/update'
    $headers = @{ 'X-Uika-Token' = $endpoint.token }
    $until = [DateTime]::UtcNow.AddMinutes(6)
    $status = $null
    do {
      $status = Invoke-RestMethod -Uri $uri -Headers $headers -TimeoutSec 5
      if ($status.available -and $status.latest -eq $Version) { break }
      Start-Sleep -Seconds 5
    } while ([DateTime]::UtcNow -lt $until)
    if (-not $status.available -or $status.latest -ne $Version) { throw "Baseline did not discover published version $Version" }
    Invoke-RestMethod -Method Post -Uri "$uri/install" -Headers $headers -TimeoutSec 5 | Out-Null
  } else {
    $p = Start-Process -FilePath $CandidateSetup -ArgumentList @('/VERYSILENT','/SUPPRESSMSGBOXES','/NORESTART','/NORESTARTAPPLICATIONS',("/DIR=`"$install`"")) -Wait -PassThru
    if ($p.ExitCode -ne 0) { throw "Candidate setup failed: $($p.ExitCode)" }
  }
  Wait-For { -not (Get-Process -Id $old.Id -ErrorAction SilentlyContinue) } 90 'Baseline process did not exit'
  Wait-For { (Test-Path $exe) -and ((Hash $exe) -eq (Hash $CandidateExe)) } 120 'Installed executable did not advance to candidate'
  Wait-For {
    $new = Get-CimInstance Win32_Process -Filter "Name='Yozora.exe'" | Where-Object { $_.ExecutablePath -eq $exe -and $_.ProcessId -ne $old.Id -and $_.CommandLine -match '\bserve\b' }
    $null -ne $new
  } 90 'Candidate was not restarted from the isolated install directory'
  Wait-For {
    if (-not (Test-Path $endpointFile)) { return $false }
    try {
      $next = Get-Content $endpointFile -Raw | ConvertFrom-Json
      if ($next.pid -eq $old.Id) { return $false }
      $nextProcess = Get-CimInstance Win32_Process -Filter "ProcessId=$($next.pid)"
      if ($nextProcess.ExecutablePath -ne $exe) { return $false }
      $newStatus = Invoke-RestMethod -Uri ($next.base_url + '/api/update') -Headers @{ 'X-Uika-Token' = $next.token } -TimeoutSec 5
      return $newStatus.version -eq $Version
    } catch { return $false }
  } 60 'Restarted candidate did not report its version through the local service'
  Check-Marker
  Write-Host "Installed upgrade 0.2.1 -> $Version passed; live updater: $LiveUpdate"
} finally {
  # Only processes started from the isolated test installation are touched.
  Get-CimInstance Win32_Process -Filter "Name='Yozora.exe'" | Where-Object { $_.ExecutablePath -eq $exe } | ForEach-Object { Stop-Process -Id $_.ProcessId -Force -ErrorAction SilentlyContinue }
  if ($launcher) { try { if (-not $launcher.HasExited) { $launcher.Kill() } } catch {} }
  $env:APPDATA, $env:LOCALAPPDATA = $oldAppdata, $oldLocal
  Remove-Item -Recurse -Force $root -ErrorAction SilentlyContinue
}
