#!/usr/bin/env sh
# pixeltui — installer for macOS and Linux
#
# One-liner:
#   curl -fsSL https://raw.githubusercontent.com/dotjarden/pixeltui/main/install.sh | sh
#
# Or from a clone:
#   ./install.sh
#
# What it does:
#   1. Detects your OS + CPU and downloads the matching prebuilt binary from the
#      latest GitHub release (no Go required)
#   2. Clears the macOS "unidentified developer" quarantine flag
#   3. Installs it onto your PATH as `pixeltui`
#   4. Runs `pixeltui doctor --fix` to install playback deps (yt-dlp, mpv)
#
# Env overrides:
#   PREFIX=/path        install location
#   PIXELTUI_NO_DEPS=1  skip the yt-dlp/mpv auto-install step

set -e

REPO="dotjarden/pixeltui"

# ── cosmetics ───────────────────────────────────────────────────────────────────
ESC=$(printf '\033')
BOLD="${ESC}[1m"; DIM="${ESC}[2m"
GREEN="${ESC}[32m"; YELLOW="${ESC}[33m"; RED="${ESC}[31m"; CYAN="${ESC}[36m"
RESET="${ESC}[0m"
step() { printf "\n${BOLD}▸ %s${RESET}\n" "$*"; }
ok()   { printf "  ${GREEN}✓${RESET}  %s\n" "$*"; }
warn() { printf "  ${YELLOW}!${RESET}  %s\n" "$*"; }
info() { printf "  ${DIM}–${RESET}  %s\n" "$*"; }
die()  { printf "\n${RED}✗  Error:${RESET} %s\n\n" "$*" >&2; exit 1; }

printf "\n${BOLD}${CYAN}pixeltui installer${RESET}\n"

# ── detect platform ─────────────────────────────────────────────────────────────
OS=$(uname -s); ARCH=$(uname -m)
case "$OS" in
  Darwin) os=darwin ;;
  Linux)  os=linux  ;;
  *) die "Unsupported OS: $OS — on Windows use install.ps1" ;;
esac
case "$ARCH" in
  x86_64|amd64)   arch=amd64 ;;
  arm64|aarch64)  arch=arm64 ;;
  *) die "Unsupported CPU: $ARCH" ;;
esac
ASSET="pixeltui-$os-$arch"
info "Platform: $os/$arch  →  $ASSET"

# ── choose an install dir on PATH ────────────────────────────────────────────────
choose_prefix() {
  [ -n "$PREFIX" ] && { mkdir -p "$PREFIX"; return; }
  if [ "$os" = darwin ] && [ -d /opt/homebrew/bin ] && [ -w /opt/homebrew/bin ]; then
    PREFIX=/opt/homebrew/bin
  elif [ -w /usr/local/bin ]; then
    PREFIX=/usr/local/bin
  else
    PREFIX="$HOME/.local/bin"; mkdir -p "$PREFIX"
  fi
}
choose_prefix
info "Install prefix: $PREFIX"

command -v curl >/dev/null 2>&1 || die "curl is required"

# ── download the prebuilt binary ─────────────────────────────────────────────────
install_binary() {
  step "pixeltui"
  url="https://github.com/$REPO/releases/latest/download/$ASSET"
  tmp=$(mktemp -d)
  info "Downloading latest release ..."
  if ! curl -fSL --progress-bar "$url" -o "$tmp/pixeltui"; then
    rm -rf "$tmp"
    return 1
  fi

  # Best-effort checksum verification.
  if curl -fsSL "https://github.com/$REPO/releases/latest/download/SHA256SUMS" -o "$tmp/SHA256SUMS" 2>/dev/null; then
    want=$(grep " $ASSET\$" "$tmp/SHA256SUMS" | awk '{print $1}')
    got=""
    if [ -n "$want" ]; then
      if command -v shasum >/dev/null 2>&1; then got=$(shasum -a 256 "$tmp/pixeltui" | awk '{print $1}')
      elif command -v sha256sum >/dev/null 2>&1; then got=$(sha256sum "$tmp/pixeltui" | awk '{print $1}')
      fi
      [ -n "$got" ] && [ "$got" != "$want" ] && { rm -rf "$tmp"; die "checksum mismatch — aborting"; }
      [ -n "$got" ] && ok "checksum verified"
    fi
  fi

  chmod +x "$tmp/pixeltui"
  [ "$os" = darwin ] && xattr -d com.apple.quarantine "$tmp/pixeltui" 2>/dev/null || true

  if mv "$tmp/pixeltui" "$PREFIX/pixeltui" 2>/dev/null; then :;
  else
    warn "Need elevated permission to write $PREFIX"
    sudo mv "$tmp/pixeltui" "$PREFIX/pixeltui"
  fi
  rm -rf "$tmp"
  ok "pixeltui installed → $PREFIX/pixeltui"
}

# ── fallback: install from source with Go ────────────────────────────────────────
install_from_source() {
  command -v go >/dev/null 2>&1 || die "could not download a release and Go is not installed.\nInstall Go (https://go.dev/dl/) or grab a binary from:\n  https://github.com/$REPO/releases/latest"
  step "pixeltui (building from source)"
  info "go install github.com/$REPO@latest ..."
  GOBIN="$PREFIX" go install "github.com/$REPO@latest"
  ok "pixeltui installed → $PREFIX/pixeltui"
}

install_binary || { warn "Prebuilt download failed — falling back to source"; install_from_source; }

# ── PATH check ───────────────────────────────────────────────────────────────────
case ":$PATH:" in
  *:"$PREFIX":*) ;;
  *) warn "$PREFIX is not on your PATH. Add to ~/.zshrc or ~/.bashrc:"
     printf "       ${DIM}export PATH=\"%s:\$PATH\"${RESET}\n" "$PREFIX" ;;
esac

# ── playback dependencies (yt-dlp + mpv) ─────────────────────────────────────────
if [ "$PIXELTUI_NO_DEPS" != "1" ]; then
  step "Playback dependencies"
  info "Installing yt-dlp + mpv via 'pixeltui doctor --fix' ..."
  "$PREFIX/pixeltui" doctor --fix || warn "Some deps couldn't auto-install — run 'pixeltui doctor' to see what's left."
fi

# ── done ─────────────────────────────────────────────────────────────────────────
printf "\n${GREEN}${BOLD}  Done!${RESET}\n\n"
printf "  Next:\n"
printf "    ${BOLD}pixeltui setup${RESET}    configure Last.fm key, Subsonic, folders\n"
printf "    ${BOLD}pixeltui${RESET}          launch the player\n\n"
printf "  ${DIM}(open a new terminal first if 'pixeltui' isn't found)${RESET}\n\n"
