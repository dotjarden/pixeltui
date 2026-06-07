# pixeltui — installer for Windows
#
# Run in PowerShell:
#   Set-ExecutionPolicy -Scope Process Bypass
#   .\install.ps1
#
# Or as a one-liner (after publishing to GitHub):
#   irm https://raw.githubusercontent.com/USER/pixeltui/main/install.ps1 | iex
#
# What it does:
#   1. Builds & installs pixeltui
#   2. Installs yt-dlp  (standalone binary — no package manager)
#   3. Installs mpv via winget  (streaming player)
#   4. Sets LASTFM_API_KEY as a permanent user environment variable

$ErrorActionPreference = "Stop"

# ── cosmetics ─────────────────────────────────────────────────────────────────
function Step   { Write-Host "`n▸ $args" -ForegroundColor White -NoNewline; Write-Host "" }
function Ok     { Write-Host "  ✓  $args" -ForegroundColor Green }
function Warn   { Write-Host "  !  $args" -ForegroundColor Yellow }
function Info   { Write-Host "  –  $args" -ForegroundColor DarkGray }
function Bail   { Write-Host "`n✗  Error: $args`n" -ForegroundColor Red; exit 1 }

Write-Host "`npixeltui installer" -ForegroundColor Cyan -NoNewline
Write-Host " (Windows)`n"

# ── install prefix ────────────────────────────────────────────────────────────
$PREFIX = if ($env:PREFIX) { $env:PREFIX } else { "$env:LOCALAPPDATA\pixeltui\bin" }
New-Item -ItemType Directory -Force -Path $PREFIX | Out-Null
Info "Install prefix: $PREFIX"

# ── add PREFIX to user PATH if needed ────────────────────────────────────────
$userPath = [Environment]::GetEnvironmentVariable("PATH", "User")
if ($userPath -notlike "*$PREFIX*") {
    [Environment]::SetEnvironmentVariable("PATH", "$PREFIX;$userPath", "User")
    $env:PATH = "$PREFIX;$env:PATH"
    Ok "Added $PREFIX to your PATH"
} else {
    Info "PATH already includes $PREFIX"
}

# ── Go check ──────────────────────────────────────────────────────────────────
Step "Go toolchain"
try {
    $goVer = (go version 2>&1) -replace "go version ", ""
    Info "Found: $goVer"
} catch {
    Warn "Go not found."
    Write-Host ""
    Write-Host "  Install Go from https://go.dev/dl/ then re-run this script." -ForegroundColor Yellow
    Write-Host "  Or with winget:  winget install GoLang.Go" -ForegroundColor DarkGray
    Write-Host ""
    Bail "Go is required to build pixeltui"
}

# ── pixeltui ──────────────────────────────────────────────────────────────────
Step "pixeltui"

# Find repo root
$repoDir = $null
foreach ($candidate in @($PSScriptRoot, ".", "$HOME\pixeltui")) {
    $modFile = Join-Path $candidate "go.mod"
    if ((Test-Path $modFile) -and (Select-String -Path $modFile -Pattern "module pixeltui" -Quiet)) {
        $repoDir = (Resolve-Path $candidate).Path
        break
    }
}

if (-not $repoDir) {
    Bail "pixeltui repo not found.`nClone it first:`n  git clone https://github.com/USER/pixeltui`n  cd pixeltui`n  .\install.ps1"
}

Info "Building from $repoDir ..."
Push-Location $repoDir
try {
    go build -ldflags="-s -w" -trimpath -o "$PREFIX\pixeltui.exe" . 2>&1 | ForEach-Object { Info $_ }
} finally {
    Pop-Location
}
Ok "pixeltui installed → $PREFIX\pixeltui.exe"

# ── yt-dlp ────────────────────────────────────────────────────────────────────
Step "yt-dlp  (YouTube audio resolver)"

$ytdlpPath = "$PREFIX\yt-dlp.exe"
$ytdlpExists = Get-Command yt-dlp -ErrorAction SilentlyContinue
if ($ytdlpExists) {
    Info "Already installed: $(yt-dlp --version 2>$null)"
} else {
    Info "Downloading yt-dlp.exe ..."
    $url = "https://github.com/yt-dlp/yt-dlp/releases/latest/download/yt-dlp.exe"
    Invoke-WebRequest -Uri $url -OutFile $ytdlpPath -UseBasicParsing
    Ok "yt-dlp installed → $ytdlpPath"
}

# Fast pip yt-dlp (optional): the standalone .exe re-unpacks ~35MB on every
# call (~8s startup tax). A pip build starts in ~0.6s. pixeltui auto-detects
# the venv at ~\.pixeltui\ytdlp-venv\Scripts\yt-dlp.exe and prefers it.
$py = Get-Command python -ErrorAction SilentlyContinue
if (-not $py) { $py = Get-Command py -ErrorAction SilentlyContinue }
if ($py) {
    Info "Setting up fast pip yt-dlp (≈7× faster cold starts) ..."
    $venv = "$HOME\.pixeltui\ytdlp-venv"
    try {
        & $py.Source -m venv --clear $venv
        & "$venv\Scripts\python.exe" -m pip install -q -U yt-dlp mutagen
        Ok "Fast yt-dlp ready → $venv\Scripts\yt-dlp.exe"
    } catch {
        Warn "Fast yt-dlp setup skipped (standalone .exe still works)."
    }
} else {
    Info "Tip: install Python to enable fast yt-dlp (≈7× faster starts)."
}

# ── mpv (streaming player) ────────────────────────────────────────────────────
Step "mpv  (streaming player)"

$mpvExists = Get-Command mpv -ErrorAction SilentlyContinue
if ($mpvExists) {
    Info "mpv already installed"
} else {
    # Try winget first (built into Windows 11, available on 10)
    $wingetExists = Get-Command winget -ErrorAction SilentlyContinue
    if ($wingetExists) {
        Info "Installing mpv via winget ..."
        try {
            winget install --id=mpv.mpv --exact --accept-package-agreements --accept-source-agreements --silent 2>&1 | Out-Null
            Ok "mpv installed via winget"
        } catch {
            Warn "winget install failed. Trying direct download..."
            $mpvDir = "$env:LOCALAPPDATA\pixeltui\mpv"
            New-Item -ItemType Directory -Force -Path $mpvDir | Out-Null
            $mpvUrl = "https://github.com/shinchiro/mpv-winbuild-cmake/releases/latest/download/mpv-x86_64-latest.zip"
            $mpvZip = "$env:TEMP\mpv.zip"
            Invoke-WebRequest -Uri $mpvUrl -OutFile $mpvZip -UseBasicParsing
            Expand-Archive -Path $mpvZip -DestinationPath $mpvDir -Force
            $mpvBin = Get-ChildItem -Path $mpvDir -Name "mpv.exe" -Recurse | Select-Object -First 1
            if ($mpvBin) {
                Copy-Item (Join-Path $mpvDir $mpvBin) "$PREFIX\mpv.exe"
                Remove-Item $mpvZip -ErrorAction SilentlyContinue
                Ok "mpv installed → $PREFIX\mpv.exe"
            } else {
                Warn "mpv download failed. Install manually: winget install mpv.mpv"
            }
        }
    } else {
        Warn "winget not found."
        Write-Host "  Install mpv manually:" -ForegroundColor Yellow
        Write-Host "    winget install mpv.mpv" -ForegroundColor DarkGray
        Write-Host "    or: https://mpv.io/installation/" -ForegroundColor DarkGray
    }
}

# ── Last.fm API key ───────────────────────────────────────────────────────────
Step "Last.fm API key"

$existingKey = [Environment]::GetEnvironmentVariable("LASTFM_API_KEY", "User")
if ($existingKey) {
    Info "LASTFM_API_KEY already set as user environment variable"
} else {
    Write-Host "  Get a free key (30 seconds) → " -NoNewline
    Write-Host "https://www.last.fm/api/account/create" -ForegroundColor Cyan
    $apikey = Read-Host "  Paste your API key (or press Enter to skip)"

    if ($apikey) {
        [Environment]::SetEnvironmentVariable("LASTFM_API_KEY", $apikey, "User")
        $env:LASTFM_API_KEY = $apikey
        Ok "LASTFM_API_KEY saved as a permanent user environment variable"
    } else {
        Warn "Skipped. Set it later:"
        Write-Host '    [Environment]::SetEnvironmentVariable("LASTFM_API_KEY","your_key","User")' -ForegroundColor DarkGray
        Write-Host "    or via System Properties → Environment Variables" -ForegroundColor DarkGray
    }
}

# ── done ──────────────────────────────────────────────────────────────────────
Write-Host ""
Write-Host "  All done!" -ForegroundColor Green
Write-Host ""
Write-Host "  Open a new terminal, then try:"
Write-Host '    pixeltui "Radiohead" "Creep"' -ForegroundColor White
Write-Host '    pixeltui -explore=8 "Daft Punk" "Get Lucky"' -ForegroundColor White
Write-Host '    pixeltui --help' -ForegroundColor White
Write-Host ""
Write-Host "  Press Enter in the list to stream a track." -ForegroundColor DarkGray
Write-Host ""
