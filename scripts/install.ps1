# Aide installer for Windows. Downloads release programs from GitHub
# Releases, verifies them against the signed checksum list, installs them
# under %LOCALAPPDATA%\Programs\Hermesh\bin and (for a machine) logs it in
# to your server with your account and password (docs/accounts.md).
# Windows PowerShell 5.1 or later; no administrator needed.
#
#   Add a machine:   & ([scriptblock]::Create((irm https://github.com/samir2015dzh/hermesh/releases/latest/download/install.ps1))) <server> -Name build-box
#                    (it asks for the account and password, or takes them from
#                    $env:AIDE_ACCOUNT and $env:AIDE_PASSWORD)
#   Agent CLI only:  & ([scriptblock]::Create((irm .../install.ps1))) -Cli
#
# Environment: AIDE_VERSION (default latest), AIDE_BIN_DIR (default
# %LOCALAPPDATA%\Programs\Hermesh\bin), AIDE_NO_PATH=1 (leave the user's
# PATH alone).
param([string]$Address = "", [switch]$Cli, [string]$Name = "")

$ErrorActionPreference = "Stop"
$ProgressPreference = "SilentlyContinue" # Invoke-WebRequest crawls with a progress bar
[Net.ServicePointManager]::SecurityProtocol = [Net.ServicePointManager]::SecurityProtocol -bor [Net.SecurityProtocolType]::Tls12

$Repo = "samir2015dzh/hermesh"
# Each AIDE_ variable, or the HERMESH_ one it was before the product was called Aide.
function Get-Setting([string]$name) { if ((Get-Item "env:AIDE_$name" -ErrorAction SilentlyContinue).Value) { (Get-Item "env:AIDE_$name").Value } else { (Get-Item "env:HERMESH_$name" -ErrorAction SilentlyContinue).Value } }
$Version = if (Get-Setting VERSION) { Get-Setting VERSION } else { "latest" }
$BinDir = if (Get-Setting BIN_DIR) { Get-Setting BIN_DIR } else { Join-Path $env:LOCALAPPDATA "Programs\Hermesh\bin" }

# Release signing public key (ECDSA P-256). Replaced at release time.
$PubKey = '__HERMESH_RELEASE_PUBKEY__'

switch ([System.Runtime.InteropServices.RuntimeInformation]::OSArchitecture) {
  "X64" { $Arch = "amd64" }
  "Arm64" { $Arch = "arm64" }
  default { throw "aide install: unsupported architecture $_" }
}
if (Get-Setting DOWNLOAD_BASE) {
  $Base = Get-Setting DOWNLOAD_BASE
} elseif ($Version -eq "latest") {
  $Base = "https://github.com/$Repo/releases/latest/download"
} else {
  $Base = "https://github.com/$Repo/releases/download/$Version"
}

# Test-Signature checks openssl's DER signature of Data by the P-256 key in
# Pem, with what Windows PowerShell 5.1's .NET has (CNG).
function Test-Signature([byte[]]$Data, [byte[]]$Der, [string]$Pem) {
  $spki = [Convert]::FromBase64String(($Pem -replace '-----[^-]+-----', '' -replace '\s', ''))
  # A P-256 SubjectPublicKeyInfo ends with its point: 04, X, Y.
  if ($spki.Length -ne 91 -or $spki[26] -ne 4) { throw "aide install: unexpected release key" }
  $blob = New-Object byte[] 72
  [BitConverter]::GetBytes([uint32]0x31534345).CopyTo($blob, 0) # "ECS1": an ECDSA P-256 public key
  [BitConverter]::GetBytes([uint32]32).CopyTo($blob, 4)
  [Array]::Copy($spki, 27, $blob, 8, 64)
  $ecdsa = New-Object Security.Cryptography.ECDsaCng([Security.Cryptography.CngKey]::Import($blob, [Security.Cryptography.CngKeyBlobFormat]::EccPublicBlob))
  $ecdsa.HashAlgorithm = [Security.Cryptography.CngAlgorithm]::Sha256
  # DER is SEQUENCE { INTEGER r, INTEGER s }; CNG wants r and s, 32 bytes each.
  $raw = New-Object byte[] 64
  $i = 2
  foreach ($at in 0, 32) {
    if ($Der[$i] -ne 2) { return $false }
    $len = $Der[$i + 1]
    $v = [byte[]]$Der[($i + 2)..($i + 1 + $len)]
    $skip = 0
    while ($v.Length - $skip -gt 32 -and $v[$skip] -eq 0) { $skip++ }
    if ($v.Length - $skip -gt 32) { return $false }
    [Array]::Copy($v, $skip, $raw, $at + 32 - ($v.Length - $skip), $v.Length - $skip)
    $i += 2 + $len
  }
  return $ecdsa.VerifyData($Data, $raw)
}

$Tmp = Join-Path ([IO.Path]::GetTempPath()) ("hermesh-" + [guid]::NewGuid())
New-Item -ItemType Directory -Path $Tmp | Out-Null
try {
  Write-Host "> downloading checksums"
  Invoke-WebRequest -UseBasicParsing "$Base/checksums.txt" -OutFile "$Tmp\checksums.txt"
  Invoke-WebRequest -UseBasicParsing "$Base/checksums.txt.sig" -OutFile "$Tmp\checksums.txt.sig"
  if ($PubKey.StartsWith("__HERMESH")) { throw "aide install: this installer has no release signing key; download a released install.ps1" }
  if (-not (Test-Signature ([IO.File]::ReadAllBytes("$Tmp\checksums.txt")) ([IO.File]::ReadAllBytes("$Tmp\checksums.txt.sig")) $PubKey)) {
    throw "aide install: checksum signature verification FAILED; refusing to install"
  }
  $sums = @{}
  foreach ($line in Get-Content "$Tmp\checksums.txt") {
    $f = $line -split '\s+'
    if ($f.Length -ge 2) { $sums[$f[1].TrimStart('*')] = $f[0].ToLower() }
  }
  # A release file, downloaded and checked against the signed list.
  function Get-Verified([string]$asset) {
    Write-Host "> downloading $asset"
    $file = Join-Path $Tmp $asset
    Invoke-WebRequest -UseBasicParsing "$Base/$asset" -OutFile $file
    if (-not $sums.ContainsKey($asset)) { throw "aide install: $asset is not in the signed checksum list" }
    $sha = [Security.Cryptography.SHA256]::Create()
    $hash = -join ($sha.ComputeHash([IO.File]::ReadAllBytes($file)) | ForEach-Object { $_.ToString("x2") })
    if ($hash -ne $sums[$asset]) { throw "aide install: $asset checksum mismatch" }
    return $file
  }
  # A running program can be renamed, not overwritten: the old one moves
  # aside, and goes once nothing runs it.
  function Install-Program([string]$file, [string]$name) {
    $dest = Join-Path $BinDir $name
    Get-ChildItem -Path $BinDir -Filter "$name.*.old" -ErrorAction SilentlyContinue | Remove-Item -Recurse -Force -ErrorAction SilentlyContinue
    if (Test-Path $dest) { Move-Item $dest "$dest.$(Get-Random).old" }
    Move-Item $file $dest
  }
  $bins = if ($Cli) { @("hermesh") } else { @("hermeshd", "hermeshdw", "hermesh") }
  New-Item -ItemType Directory -Force -Path $BinDir | Out-Null
  foreach ($b in $bins) {
    $file = Get-Verified "$b-windows-$Arch.exe"
    # Also under the name it has since the product was called Aide: a copy, as
    # the logon task and agents' hooks run it by the name it had (RENAME-AIDE.md B1, B2).
    $alias = @{ "hermeshd" = "aided"; "hermesh" = "aide" }[$b]
    if ($alias) { $copy = "$file.$alias"; Copy-Item $file $copy; Install-Program $copy "$alias.exe" }
    Install-Program $file "$b.exe"
  }
  # Her's kernel and its console, beside hermeshd, which runs them while Her
  # is on for this PC (docs/her-server.md §6.4): from a release that has them.
  if (-not $Cli -and $sums.ContainsKey("her-windows-$Arch.exe")) {
    $console = Get-Verified "her-console.tar.gz"
    $unpacked = Join-Path $Tmp "her-console"
    New-Item -ItemType Directory -Path $unpacked | Out-Null
    # Windows' own tar (10 and later), not another on PATH that takes C: for a host.
    & (Join-Path $env:SystemRoot "System32\tar.exe") -xzf $console -C $unpacked
    if ($LASTEXITCODE -ne 0) { throw "aide install: cannot unpack her-console.tar.gz" }
    Install-Program (Get-Verified "her-windows-$Arch.exe") "her.exe"
    Install-Program $unpacked "her-console"
  }
  Write-Host "installed to $BinDir"
} finally {
  Remove-Item -Recurse -Force $Tmp -ErrorAction SilentlyContinue
}

# The user's PATH, kept as written (its %VARIABLES% unexpanded).
$envKey = Get-Item -Path "HKCU:\Environment"
$userPath = $envKey.GetValue("Path", "", [Microsoft.Win32.RegistryValueOptions]::DoNotExpandEnvironmentNames)
if (-not (Get-Setting NO_PATH) -and -not (($userPath -split ';') -contains $BinDir)) {
  Set-ItemProperty -Path "HKCU:\Environment" -Name Path -Type ExpandString -Value ((($userPath.TrimEnd(';')) + ";" + $BinDir).TrimStart(';'))
  # Setting any user variable tells running programs the environment changed.
  [Environment]::SetEnvironmentVariable("HERMESH_INSTALLED", "1", "User")
  [Environment]::SetEnvironmentVariable("HERMESH_INSTALLED", $null, "User")
  Write-Host "added $BinDir to your PATH (new terminals see it)"
}
if (-not (($env:Path -split ';') -contains $BinDir)) { $env:Path = "$env:Path;$BinDir" }

if ($Cli) {
  Write-Host "Next: aide auth login <your server> --account <your account>"
  return
}
$login = @("login")
if ($Name) { $login += @("--name", $Name) }
if ($Address) { $login += $Address }
& (Join-Path $BinDir "hermeshd.exe") @login
