# Installs the Windows app from its Setup.exe as a person would, checks
# what the install set up, opens the app, and uninstalls it, checking that
# what the app set up is gone and the data folder stays (docs/windows.md
# §6.1). CI runs it on windows-latest; it also runs on a PC or VM where
# Aide isn't installed.
#   check-windows-setup.ps1 <Setup.exe>
param([Parameter(Mandatory)][string]$Setup)
$ErrorActionPreference = "Stop"

$root = Join-Path $env:LOCALAPPDATA "AideApp"
$bin = Join-Path $env:LOCALAPPDATA "Programs\Aide\bin"
$link = Join-Path $env:APPDATA "Microsoft\Windows\Start Menu\Programs\Aide.lnk"
$pipe = Join-Path $env:LOCALAPPDATA "Aide\run\aided.pipe"
$task = "Aide ($env:USERNAME)"
function Fail($what) { throw "check-windows-setup: $what" }
function UserPath { (Get-Item "HKCU:\Environment").GetValue("Path", "", "DoNotExpandEnvironmentNames") -split ';' }
function Link { (Get-ItemProperty "HKCU:\Software\Classes\aide\shell\open\command" -ErrorAction SilentlyContinue).'(default)' }

Write-Host "> installing"
$p = Start-Process -FilePath $Setup -ArgumentList "--silent" -Wait -PassThru
if ($p.ExitCode -ne 0) { Fail "Setup.exe exited with $($p.ExitCode)" }
foreach ($f in "Aide.exe", "Update.exe") { if (-not (Test-Path (Join-Path $root $f))) { Fail "no $f in $root" } }
$version = Get-ChildItem $root -Directory -Filter "app-*" | Select-Object -First 1
if (-not $version -or -not (Test-Path (Join-Path $version.FullName "bin\aidedw.exe"))) { Fail "no app-<version>\bin\aidedw.exe" }
Write-Host "installed $($version.Name)"
if (-not (Test-Path $link)) { Fail "no Start menu shortcut" }
if (Test-Path (Join-Path ([Environment]::GetFolderPath("Desktop")) "Aide.lnk")) { Fail "a desktop shortcut" }
if ((UserPath) -notcontains $bin) { Fail "$bin is not on the user's PATH" }

Write-Host "> opening it from the Start menu's shortcut"
Start-Process $link
for ($i = 0; $i -lt 90 -and -not (Test-Path $pipe); $i++) { Start-Sleep 1 }
Get-CimInstance Win32_Process | Where-Object { $_.Name -match '^(Aide|aidedw)\.exe$' } | Format-Table ProcessId, CommandLine -Wrap
if (-not (Test-Path $pipe)) { Fail "the app started no daemon" }
foreach ($cli in "aide", "aided") {
  & (Join-Path $bin "$cli.exe") version
  if ($LASTEXITCODE -ne 0) { Fail "$cli version" }
}
# Her is gone from the app: nothing of hers beside aided.
foreach ($f in "her.exe", "her-console", "pdfium.dll", "agent-desktop.exe") {
  if (Test-Path (Join-Path $bin $f)) { Fail "$f beside aided" }
}
$action = (Get-ScheduledTask -TaskName $task).Actions[0]
Write-Host "logon task: $($action.Execute) $($action.Arguments)"
if ($action.Execute -ne (Join-Path $bin "aidedw.exe") -or $action.Arguments -notmatch '--owner') { Fail "the logon task runs something else" }
if ((Link) -notlike "*$root\Aide.exe*") { Fail "aide: links open $(Link)" }

Write-Host "> uninstalling"
$p = Start-Process -FilePath (Join-Path $root "Update.exe") -ArgumentList "--uninstall", "-s" -Wait -PassThru
if ($p.ExitCode -ne 0) { Fail "Update.exe --uninstall exited with $($p.ExitCode)" }
$left = @()
if (Get-ScheduledTask -TaskName $task -ErrorAction SilentlyContinue) { $left += "the logon task" }
if (Test-Path $link) { $left += "the Start menu shortcut" }
if ((UserPath) -contains $bin) { $left += "$bin on PATH" }
if (Link) { $left += "aide: links" }
if (Test-Path $bin) { $left += $bin }
if (Test-Path (Join-Path $root "Aide.exe")) { $left += "the app" }
if ($left) {
  # What the app's steps said.
  Get-Content (Join-Path $env:LOCALAPPDATA "Aide\logs\setup.log") -ErrorAction SilentlyContinue
  Fail "these stayed: $($left -join ', ')"
}
if (-not (Test-Path (Join-Path $env:LOCALAPPDATA "Aide"))) { Fail "the data folder went" }
Write-Host "✓ installed, opened and uninstalled"
