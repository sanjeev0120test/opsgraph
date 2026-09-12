# Install opsgraph from a GitHub Release (or a local dist/ directory).
# Free, no accounts. Verifies SHA256 when SHA256SUMS is present.
$ErrorActionPreference = "Stop"

$Repo = if ($env:OPSGRAPH_REPO) { $env:OPSGRAPH_REPO } else { "sanjeev0120test/opsgraph" }
$Version = if ($env:OPSGRAPH_VERSION) { $env:OPSGRAPH_VERSION } else { "latest" }
$InstallDir = if ($env:OPSGRAPH_INSTALL_DIR) { $env:OPSGRAPH_INSTALL_DIR } else { Join-Path $env:LOCALAPPDATA "opsgraph\bin" }
$DistDir = $env:OPSGRAPH_DIST_DIR
$ReleaseDir = $env:OPSGRAPH_RELEASE_DIR

# Prefer OSArchitecture (true process arch) over PROCESSOR_ARCHITECTURE (can be
# AMD64 under ARM64 WoW64 / compatibility shims).
$archRaw = if ($env:PROCESSOR_ARCHITEW6432) { $env:PROCESSOR_ARCHITEW6432 } else { $env:PROCESSOR_ARCHITECTURE }
if ([Environment]::Is64BitOperatingSystem -and (Get-Command Get-CimInstance -ErrorAction SilentlyContinue)) {
  try {
    $osArch = (Get-CimInstance -ClassName Win32_OperatingSystem -ErrorAction Stop).OSArchitecture
    if ($osArch -match "ARM") { $archRaw = "ARM64" }
    elseif ($osArch -match "64") { $archRaw = "AMD64" }
  } catch {
    # Fall back to PROCESSOR_ARCHITECTURE when CIM is unavailable.
  }
}
$arch = switch -Regex ($archRaw) {
  "AMD64|X64|x86_64" { "amd64" }
  "ARM64|aarch64" { "arm64" }
  default { throw "unsupported arch: $archRaw" }
}
$os = "windows"

function Get-Sha256SumLine {
  param(
    [string]$SumsPath,
    [string]$AssetName
  )
  $escaped = [regex]::Escape($AssetName)
  $line = Get-Content $SumsPath | Where-Object {
    $_ -match "^\s*[A-Fa-f0-9]{64}\s+\*?${escaped}\s*$"
  } | Select-Object -First 1
  if (-not $line) { throw "SHA256SUMS has no exact entry for $AssetName" }
  return $line
}

function Install-FromArchive {
  param(
    [string]$Tag,
    [string]$ZipPath,
    [string]$SumsPath
  )
  $asset = Split-Path $ZipPath -Leaf
  if ($env:OPSGRAPH_INSECURE -ne "1") {
    if (-not (Test-Path $SumsPath) -or -not (Get-Item $SumsPath).Length) {
      throw "SHA256SUMS missing or empty (set OPSGRAPH_INSECURE=1 to skip)"
    }
    $expected = Get-Sha256SumLine -SumsPath $SumsPath -AssetName $asset
    $want = ($expected -split '\s+')[0].ToLower()
    $got = (Get-FileHash -Algorithm SHA256 -Path $ZipPath).Hash.ToLower()
    if ($want -ne $got) { throw "SHA256 mismatch for $asset" }
    Write-Host "checksum ok: $asset"
  } else {
    Write-Warning "skipping checksum verification (OPSGRAPH_INSECURE=1)"
  }
  Expand-Archive -Path $ZipPath -DestinationPath $tmp.FullName -Force
  $path = Join-Path $tmp.FullName "opsgraph_${Tag}_${os}_${arch}.exe"
  if (-not (Test-Path $path)) { throw "extracted binary missing: $path" }
  return $path
}

$tmp = New-Item -ItemType Directory -Path ([System.IO.Path]::Combine([System.IO.Path]::GetTempPath(), [guid]::NewGuid().ToString()))
try {
  if ($DistDir) {
    $src = Get-ChildItem -Path $DistDir -Filter "opsgraph-$os-$arch*" -File | Select-Object -First 1
    if (-not $src) { throw "no local binary for $os/$arch in $DistDir" }
    $bin = Join-Path $tmp.FullName "opsgraph.exe"
    Copy-Item $src.FullName $bin
  } elseif ($ReleaseDir) {
    if (-not $Version -or $Version -eq "latest") {
      throw "OPSGRAPH_VERSION must be set when using OPSGRAPH_RELEASE_DIR"
    }
    $tryTags = @($Version)
    if ($Version -notlike "v*") { $tryTags += "v$Version" }
    $tag = $null
    $zipPath = $null
    foreach ($try in $tryTags) {
      $candidate = Join-Path $ReleaseDir "opsgraph_${try}_${os}_${arch}.zip"
      if (Test-Path $candidate) { $tag = $try; $zipPath = $candidate; break }
    }
    if (-not $zipPath) { throw "missing release asset for $Version in $ReleaseDir" }
    $sumsPath = Join-Path $ReleaseDir "SHA256SUMS"
    $bin = Install-FromArchive -Tag $tag -ZipPath $zipPath -SumsPath $sumsPath
  } else {
    if ($Version -eq "latest") {
      $rel = Invoke-RestMethod -Uri "https://api.github.com/repos/$Repo/releases/latest"
      $tag = $rel.tag_name
      $tryTags = @($tag)
    } else {
      $tryTags = @($Version)
      if ($Version -notlike "v*") { $tryTags += "v$Version" }
    }
    if (-not $tryTags[0]) { throw "could not resolve release tag for $Repo" }
    $tag = $null
    $zipPath = $null
    $asset = $null
    foreach ($try in $tryTags) {
      $asset = "opsgraph_${try}_${os}_${arch}.zip"
      $url = "https://github.com/$Repo/releases/download/$try/$asset"
      Write-Host "downloading $url"
      $zipPath = Join-Path $tmp.FullName $asset
      try {
        Invoke-WebRequest -Uri $url -OutFile $zipPath
        $tag = $try
        break
      } catch {
        Remove-Item $zipPath -ErrorAction SilentlyContinue
      }
    }
    if (-not $tag) { throw "failed to download release asset for $Version" }
    $sumsPath = Join-Path $tmp.FullName "SHA256SUMS"
    try {
      Invoke-WebRequest -Uri "https://github.com/$Repo/releases/download/$tag/SHA256SUMS" -OutFile $sumsPath
    } catch {
      if ($env:OPSGRAPH_INSECURE -ne "1") {
        throw "failed to download SHA256SUMS (set OPSGRAPH_INSECURE=1 to skip): $($_.Exception.Message)"
      }
      Write-Warning "SHA256SUMS download failed; continuing insecurely"
      New-Item -ItemType File -Path $sumsPath -Force | Out-Null
    }
    $bin = Install-FromArchive -Tag $tag -ZipPath $zipPath -SumsPath $sumsPath
  }

  New-Item -ItemType Directory -Force -Path $InstallDir | Out-Null
  $dest = Join-Path $InstallDir "opsgraph.exe"
  try {
    Copy-Item $bin $dest -Force
  } catch {
    # Windows locks running executables; stage beside and instruct replace.
    $staged = Join-Path $InstallDir "opsgraph.exe.new"
    Copy-Item $bin $staged -Force
    throw "could not replace $dest (is opsgraph running?). Staged as $staged - stop the process and rename it to opsgraph.exe"
  }
  Write-Host "installed $dest"
  $normInstall = [System.IO.Path]::GetFullPath($InstallDir).TrimEnd('\')
  $onPath = $false
  foreach ($p in ($env:PATH -split ';')) {
    if (-not $p) { continue }
    try {
      if ([System.IO.Path]::GetFullPath($p).TrimEnd('\') -ieq $normInstall) { $onPath = $true; break }
    } catch {
      # Ignore malformed PATH entries.
    }
  }
  if (-not $onPath) {
    Write-Warning "add $InstallDir to PATH to run opsgraph from any shell"
    Write-Host "  `$env:PATH = `"$InstallDir;`$env:PATH`""
  }
  & $dest version
} finally {
  Remove-Item -Recurse -Force $tmp.FullName -ErrorAction SilentlyContinue
}
