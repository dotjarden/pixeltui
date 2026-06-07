# pixeltui — installer for Windows
#
# One-liner (PowerShell):
#   irm https://raw.githubusercontent.com/dotjarden/pixeltui/main/install.ps1 | iex
#
# Or from a clone:
#   Set-ExecutionPolicy -Scope Process Bypass
#   .\install.ps1
#
# What it does:
#   1. Downloads the prebuilt pixeltui.exe from the latest GitHub release
#   2. Installs it onto your PATH
#   3. Runs `pixeltui doctor --fix` to install playback deps (yt-dlp, mpv)
#
# Env overrides:
#   $env:PREFIX                  install location
#   -NoDoctor                    skip the auto doctor --fix
#   $env:PIXELTUI_NO_DOCTOR = "1"  same, via env (back-compat: PIXELTUI_NO_DEPS)

param([switch]$NoDoctor)

$ErrorActionPreference = "Stop"
$Repo = "dotjarden/pixeltui"

function Step { Write-Host "`n> $args" -ForegroundColor White }
function Ok   { Write-Host "  [ok]  $args" -ForegroundColor Green }
function Warn { Write-Host "  [!]   $args" -ForegroundColor Yellow }
function Info { Write-Host "  -     $args" -ForegroundColor DarkGray }
function Bail { Write-Host "`nError: $args`n" -ForegroundColor Red; exit 1 }

Write-Host "`npixeltui installer" -ForegroundColor Cyan

# ── platform ────────────────────────────────────────────────────────────────────
$arch = if ([System.Runtime.InteropServices.RuntimeInformation]::OSArchitecture -eq "Arm64") { "arm64" } else { "amd64" }
# Releases currently ship a single Windows build (amd64); it runs on Arm64 via emulation.
$asset = "pixeltui-windows-amd64.exe"
Info "Asset: $asset"

# ── install prefix (on PATH) ──────────────────────────────────────────────────────
$PREFIX = if ($env:PREFIX) { $env:PREFIX } else { "$env:LOCALAPPDATA\pixeltui\bin" }
New-Item -ItemType Directory -Force -Path $PREFIX | Out-Null
Info "Install prefix: $PREFIX"

$userPath = [Environment]::GetEnvironmentVariable("PATH", "User")
if ($userPath -notlike "*$PREFIX*") {
    [Environment]::SetEnvironmentVariable("PATH", "$PREFIX;$userPath", "User")
    $env:PATH = "$PREFIX;$env:PATH"
    Ok "Added $PREFIX to your PATH"
}

# ── download the prebuilt binary ──────────────────────────────────────────────────
Step "pixeltui"
$exe = "$PREFIX\pixeltui.exe"
$url = "https://github.com/$Repo/releases/latest/download/$asset"
Info "Downloading latest release ..."
try {
    Invoke-WebRequest -Uri $url -OutFile $exe -UseBasicParsing
} catch {
    # Fallback: build from source if Go is available.
    if (Get-Command go -ErrorAction SilentlyContinue) {
        Warn "Download failed — building from source with Go ..."
        $env:GOBIN = $PREFIX
        go install "github.com/$Repo/tui@latest"
        # The Go tree lives in ./tui, so the binary installs as tui.exe — rename it.
        if (Test-Path "$PREFIX\tui.exe") { Move-Item -Force "$PREFIX\tui.exe" $exe }
    } else {
        Bail "Could not download a release and Go is not installed.`nInstall Go (https://go.dev/dl/) or grab pixeltui.exe from:`n  https://github.com/$Repo/releases/latest"
    }
}

# Best-effort checksum verification.
try {
    $sumsUrl = "https://github.com/$Repo/releases/latest/download/SHA256SUMS"
    $sums = (Invoke-WebRequest -Uri $sumsUrl -UseBasicParsing).Content
    $want = ($sums -split "`n" | Where-Object { $_ -match [regex]::Escape($asset) } | Select-Object -First 1).Split(" ")[0]
    if ($want) {
        $got = (Get-FileHash -Algorithm SHA256 $exe).Hash.ToLower()
        if ($got -ne $want.ToLower()) { Bail "checksum mismatch — aborting" }
        Ok "checksum verified"
    }
} catch { }

Ok "pixeltui installed -> $exe"

# ── playback dependencies ─────────────────────────────────────────────────────────
$skipDoctor = $NoDoctor -or ($env:PIXELTUI_NO_DOCTOR -eq "1") -or ($env:PIXELTUI_NO_DEPS -eq "1")
if (-not $skipDoctor) {
    Step "Checking & installing playback dependencies"
    Info "Running 'pixeltui doctor --fix' (yt-dlp + mpv) ..."
    try { & $exe doctor --fix } catch { Warn "Some deps couldn't auto-install — run 'pixeltui doctor'." }
}

# ── done ──────────────────────────────────────────────────────────────────────────
Write-Host "`n  ✓ launch-ready" -ForegroundColor Green
Write-Host ""
Write-Host "  Next (optional):"
Write-Host "    pixeltui setup    add Last.fm key, Subsonic, folders" -ForegroundColor White
Write-Host "    pixeltui          launch the player" -ForegroundColor White
Write-Host ""
