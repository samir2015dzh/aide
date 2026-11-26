# Updates the Windows app as its owners get updates (docs/windows.md §6.2):
# installs an older build from its Setup.exe, serves a newer build's
# release files (its package and signed feed) on 127.0.0.1:8765, where
# both test builds look, opens the app, which downloads the newer version
# and has Squirrel install it beside itself; then it starts the newer
# version by itself (工单 #128: a test build does not wait for the owner to
# go away), which places its programs and restarts aided on them. Then
# it uninstalls. Both builds come from package-win.mjs with
# AIDE_UPDATE_FEED=http://127.0.0.1:8765, the same
# AIDE_RELEASE_URL, and a key of the test's (AIDE_RELEASE_PUBKEY_FILE,
# RELEASE_SIGNING_KEY_FILE).
#   check-windows-update.ps1 <older Setup.exe> <newer build's dist> <newer version>
param([Parameter(Mandatory)][string]$Setup, [Parameter(Mandatory)][string]$Release, [Parameter(Mandatory)][string]$Version)
$ErrorActionPreference = "Stop"

$root = Join-Path $env:LOCALAPPDATA "AideApp"
$bin = Join-Path $env:LOCALAPPDATA "Programs\Aide\bin"
$link = Join-Path $env:APPDATA "Microsoft\Windows\Start Menu\Programs\Aide.lnk"
$pipe = Join-Path $env:LOCALAPPDATA "Aide\run\aided.pipe"
function Fail($what) {
  # What there is, to see why.
  Get-ChildItem $root | Format-Table Name, LastWriteTime
  Get-CimInstance Win32_Process | Where-Object { $_.Name -match '^(Aide|aidedw|Update)\.exe$' } | Format-Table ProcessId, ExecutablePath, CommandLine -Wrap
  Get-Content (Join-Path $env:LOCALAPPDATA "Aide\logs\setup.log") -Tail 20 -ErrorAction SilentlyContinue
  Get-Content (Join-Path $env:LOCALAPPDATA "Aide\logs\app-daemon.log") -Tail 20 -ErrorAction SilentlyContinue
  Get-Content (Join-Path $root "Squirrel-Update.log") -Tail 40 -ErrorAction SilentlyContinue
  throw "check-windows-update: $what"
}
function Daemons { @(Get-CimInstance Win32_Process -Filter "Name='aidedw.exe'" | ForEach-Object ProcessId) }
function Wait($what, $seconds, [scriptblock]$done) {
  for ($i = 0; $i -lt $seconds; $i++) { if (& $done) { return }; Start-Sleep 1 }
  Fail "waited $seconds s for $what"
}

# From a clean start: the app keeps when it last checked for updates in the data folder, which uninstalling leaves,
# and checks again only 4 hours on (checkEvery). An app an earlier step left open past its first check (30 s after it
# starts, against the real feed: check-windows-setup.ps1) left that time there, and this one would not look for the
# newer build at all (工单 #128: main's CI, 2026-11-26, "waited 300 s for --squirrel-updated").
$desktop = Join-Path $env:LOCALAPPDATA "Aide\desktop"
Remove-Item (Join-Path $desktop "updates.json"), (Join-Path $desktop "update") -Recurse -Force -ErrorAction SilentlyContinue

$server = Start-Process python -ArgumentList "-m", "http.server", "8765", "--bind", "127.0.0.1", "--directory", (Resolve-Path $Release) -PassThru -WindowStyle Hidden
try {
  Write-Host "> installing the older build"
  $p = Start-Process -FilePath $Setup -ArgumentList "--silent" -Wait -PassThru
  if ($p.ExitCode -ne 0) { Fail "Setup.exe exited with $($p.ExitCode)" }
  Start-Process $link
  Wait "the daemon" 90 { Test-Path $pipe }
  $before = Daemons

  Write-Host "> waiting for the app to find, download and install $Version"
  # Installed once Squirrel has run the new version's step (setup.log) and is done.
  $log = Join-Path $env:LOCALAPPDATA "Aide\logs\setup.log"
  Wait "--squirrel-updated $Version" 300 { (Get-Content $log -ErrorAction SilentlyContinue) -match "--squirrel-updated $Version" }
  Wait "Update.exe to finish" 60 { -not (Get-Process Update -ErrorAction SilentlyContinue) }

  if (-not (Test-Path $link)) { Fail "the update removed Aide.lnk" }

  Write-Host "> waiting for the app to start $Version by itself"
  Wait "the app at $Version" 180 { Get-CimInstance Win32_Process -Filter "Name='Aide.exe'" | Where-Object { $_.ExecutablePath -like "*\app-$Version\*" } }
  Wait "its programs" 60 { (& (Join-Path $bin "aide.exe") version) -eq $Version -and (& (Join-Path $bin "aide.exe") version) -eq $Version }
  # Not busy, the app restarts aided 30 s after seeing its older version.
  Wait "aided restarted" 120 { $now = Daemons; $now.Count -gt 0 -and -not ($now | Where-Object { $before -contains $_ }) }
  Get-CimInstance Win32_Process | Where-Object { $_.Name -match '^(Aide|aidedw)\.exe$' } | Format-Table ProcessId, ExecutablePath -Wrap
  if (@(Get-CimInstance Win32_Process -Filter "Name='Aide.exe'" | Where-Object { $_.ExecutablePath -notlike "*\app-$Version\*" }).Count) { Fail "the older version still runs" }

  Write-Host "> uninstalling"
  $p = Start-Process -FilePath (Join-Path $root "Update.exe") -ArgumentList "--uninstall", "-s" -Wait -PassThru
  if ($p.ExitCode -ne 0) { Fail "Update.exe --uninstall exited with $($p.ExitCode)" }
} finally {
  Stop-Process -Id $server.Id -Force -ErrorAction SilentlyContinue
}
Write-Host "✓ updated to $Version"
