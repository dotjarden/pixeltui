#!/usr/bin/env sh
# pixeltui — installer for macOS and Linux
#
# Run from the repo:   ./install.sh
# Or one-liner:        curl -fsSL https://raw.githubusercontent.com/USER/pixeltui/main/install.sh | sh
#
# What it does:
#   1. Builds & installs pixeltui
#   2. Installs yt-dlp  (standalone binary — no package manager)
#   3. Installs a streaming player (mpv or ffplay)
#   4. Wires up LASTFM_API_KEY in your shell config

set -e

# ── cosmetics ─────────────────────────────────────────────────────────────────
ESC=$(printf '\033')
BOLD="${ESC}[1m"; DIM="${ESC}[2m"
GREEN="${ESC}[32m"; YELLOW="${ESC}[33m"; RED="${ESC}[31m"; CYAN="${ESC}[36m"
RESET="${ESC}[0m"

banner() { printf "\n${BOLD}${CYAN}%s${RESET}\n" "$*"; }
step()   { printf "\n${BOLD}▸ %s${RESET}\n" "$*"; }
ok()     { printf "  ${GREEN}✓${RESET}  %s\n" "$*"; }
warn()   { printf "  ${YELLOW}!${RESET}  %s\n" "$*"; }
info()   { printf "  ${DIM}–${RESET}  %s\n" "$*"; }
die()    { printf "\n${RED}✗  Error:${RESET} %s\n\n" "$*" >&2; exit 1; }

# ── platform ──────────────────────────────────────────────────────────────────
detect_platform() {
  OS=$(uname -s)
  ARCH=$(uname -m)
  case "$OS" in
    Darwin) OS=darwin ;;
    Linux)  OS=linux  ;;
    *) die "Unsupported OS: $OS — use install.ps1 for Windows" ;;
  esac
  case "$ARCH" in
    x86_64)          ARCH=amd64 ;;
    arm64|aarch64)   ARCH=arm64 ;;
    *) die "Unsupported architecture: $ARCH" ;;
  esac
}

# ── install prefix ────────────────────────────────────────────────────────────
find_prefix() {
  if [ -n "$PREFIX" ]; then
    mkdir -p "$PREFIX"
    return
  fi
  if [ "$OS" = darwin ] && [ -d /opt/homebrew/bin ]; then
    # Apple Silicon Homebrew prefix (always writable by the owner)
    PREFIX=/opt/homebrew/bin
  elif [ -w /usr/local/bin ]; then
    PREFIX=/usr/local/bin
  else
    PREFIX="$HOME/.local/bin"
    mkdir -p "$PREFIX"
  fi
}

# ── PATH check ────────────────────────────────────────────────────────────────
check_path() {
  case ":$PATH:" in
    *:"$PREFIX":*) return ;;
  esac
  warn "$PREFIX is not in your PATH."
  printf "  Add this to your shell config (~/.zshrc or ~/.bashrc):\n"
  printf "    ${DIM}export PATH=\"%s:\$PATH\"${RESET}\n\n" "$PREFIX"
}

# ── pixeltui ──────────────────────────────────────────────────────────────────
install_pixeltui() {
  step "pixeltui"

  # Locate the repo — works whether the script is in the repo or run via curl
  REPO_DIR=""
  for candidate in . "$(dirname "$0")" "$HOME/pixeltui"; do
    if [ -f "$candidate/go.mod" ] && grep -q 'module pixeltui' "$candidate/go.mod" 2>/dev/null; then
      REPO_DIR=$(cd "$candidate" && pwd)
      break
    fi
  done

  if [ -z "$REPO_DIR" ]; then
    die "pixeltui repo not found.\nClone it first:\n  git clone https://github.com/USER/pixeltui && cd pixeltui && ./install.sh"
  fi

  if ! command -v go >/dev/null 2>&1; then
    die "Go not found. Install it from https://go.dev/dl/ then re-run this script.\nQuick install:\n  curl -fsSL https://go.dev/dl/go1.23.linux-amd64.tar.gz | sudo tar xz -C /usr/local\n  export PATH=/usr/local/go/bin:\$PATH"
  fi

  info "Go $(go version | awk '{print $3}') detected"
  info "Building from $REPO_DIR..."

  (cd "$REPO_DIR" && go build -ldflags="-s -w" -trimpath -o "$PREFIX/pixeltui" .)
  ok "pixeltui installed → $PREFIX/pixeltui"
}

# ── yt-dlp ────────────────────────────────────────────────────────────────────
install_ytdlp() {
  step "yt-dlp  (YouTube audio resolver)"

  if command -v yt-dlp >/dev/null 2>&1; then
    info "Already installed: $(yt-dlp --version 2>/dev/null)"
    return
  fi

  case "$OS-$ARCH" in
    darwin-*)  URL="https://github.com/yt-dlp/yt-dlp/releases/latest/download/yt-dlp_macos" ;;
    linux-*)   URL="https://github.com/yt-dlp/yt-dlp/releases/latest/download/yt-dlp" ;;
  esac

  info "Downloading yt-dlp..."
  curl -fsSL "$URL" -o "$PREFIX/yt-dlp"
  chmod +x "$PREFIX/yt-dlp"
  ok "yt-dlp installed → $PREFIX/yt-dlp"
}

# ── fast pip yt-dlp ───────────────────────────────────────────────────────────
# The standalone binary re-unpacks ~35MB on every call (~8s startup tax). A pip
# build starts in ~0.6s, cutting cold play→audio ~7×. pixeltui auto-detects the
# venv at ~/.pixeltui/ytdlp-venv. Best-effort; needs python3.
install_fast_ytdlp() {
  command -v python3 >/dev/null 2>&1 || return 0
  step "fast yt-dlp  (pip — ~7× faster cold starts)"
  venv="$HOME/.pixeltui/ytdlp-venv"
  if python3 -m venv --clear "$venv" 2>/dev/null && \
     "$venv/bin/python" -m pip install -q -U yt-dlp mutagen 2>/dev/null && \
     "$venv/bin/yt-dlp" --version >/dev/null 2>&1; then
    ok "fast yt-dlp ready → $venv/bin/yt-dlp"
  else
    info "skipped (standalone yt-dlp still works)"
  fi
}

# ── streaming player ──────────────────────────────────────────────────────────
install_player() {
  step "Streaming player"

  if command -v mpv >/dev/null 2>&1; then
    info "mpv already installed (best option)"
    return
  fi
  if command -v ffplay >/dev/null 2>&1; then
    info "ffplay already installed"
    return
  fi

  case "$OS" in
    darwin)
      # mpv (pause/seek/volume + OS Now Playing) is installed by the app's own
      # self-contained, arch-aware bundle installer — no package manager needed.
      info "Run 'pixeltui doctor --fix' (or 'make stream-setup') to install mpv."
      info "Until then, audio plays via the built-in afplay fallback."
      ;;

    linux)
      info "Installing ffmpeg (provides ffplay)..."
      if command -v apt-get >/dev/null 2>&1; then
        sudo apt-get install -y -q ffmpeg 2>/dev/null && ok "ffmpeg installed via apt" && return
      fi
      if command -v dnf >/dev/null 2>&1; then
        sudo dnf install -y -q ffmpeg 2>/dev/null && ok "ffmpeg installed via dnf" && return
      fi
      if command -v pacman >/dev/null 2>&1; then
        sudo pacman -S --noconfirm ffmpeg 2>/dev/null && ok "ffmpeg installed via pacman" && return
      fi
      if command -v zypper >/dev/null 2>&1; then
        sudo zypper install -y ffmpeg 2>/dev/null && ok "ffmpeg installed via zypper" && return
      fi
      warn "Could not auto-install ffmpeg."
      printf "  Install it manually for your distro:\n"
      printf "    Ubuntu/Debian:   sudo apt install ffmpeg\n"
      printf "    Fedora:          sudo dnf install ffmpeg\n"
      printf "    Arch:            sudo pacman -S ffmpeg\n"
      ;;
  esac
}

# ── Last.fm API key ───────────────────────────────────────────────────────────
setup_apikey() {
  step "Last.fm API key"

  if [ -n "$LASTFM_API_KEY" ]; then
    info "LASTFM_API_KEY already set"
    return
  fi

  printf "  Get a free key (30 seconds) → ${CYAN}https://www.last.fm/api/account/create${RESET}\n"
  printf "  Paste your API key (or press Enter to skip): "

  # Read from /dev/tty so it works even when script is piped from curl
  if [ -e /dev/tty ]; then
    read -r apikey </dev/tty
  else
    read -r apikey
  fi

  if [ -z "$apikey" ]; then
    warn "Skipped. Set it later:"
    printf "    export LASTFM_API_KEY=your_key_here\n"
    return
  fi

  # Detect shell config file
  case "${SHELL:-sh}" in
    */zsh)  cfg="$HOME/.zshrc" ;;
    */fish) cfg="$HOME/.config/fish/config.fish" ;;
    *)      cfg="$HOME/.bashrc" ;;
  esac

  if grep -q "LASTFM_API_KEY" "$cfg" 2>/dev/null; then
    warn "LASTFM_API_KEY already present in $cfg — not overwriting"
    warn "Update it manually if needed"
  else
    printf '\nexport LASTFM_API_KEY=%s\n' "$apikey" >> "$cfg"
    ok "Added to $cfg"
  fi

  export LASTFM_API_KEY="$apikey"
}

# ── fix broken ffplay on macOS (libvpx version skew after brew upgrade) ───────
fix_ffplay_macos() {
  [ "$OS" = darwin ] || return 0
  command -v ffplay >/dev/null 2>&1 || return 0
  ffplay -version >/dev/null 2>&1 && return 0   # already working

  vpxdir=/usr/local/opt/libvpx/lib
  [ -d "$vpxdir" ] || return 0

  new=$(ls "$vpxdir"/libvpx.*.dylib 2>/dev/null | grep -v 'libvpx\.dylib$' | sort -V | tail -1)
  [ -z "$new" ] && return 0

  ver=$(basename "$new" .dylib | sed 's/libvpx\.//')
  prev=$((ver - 1))
  sym="$vpxdir/libvpx.$prev.dylib"
  if [ ! -e "$sym" ]; then
    ln -s "$new" "$sym" 2>/dev/null && ok "Fixed ffplay: linked libvpx.$prev → libvpx.$ver" || true
  fi
}

# ── done ──────────────────────────────────────────────────────────────────────
done_message() {
  printf "\n${GREEN}${BOLD}  All done!${RESET}\n\n"
  printf "  Reload your shell or run:\n"
  printf "    ${DIM}source ~/.zshrc   # (or .bashrc)${RESET}\n\n"
  printf "  Then try:\n"
  printf "    ${BOLD}pixeltui \"Radiohead\" \"Creep\"${RESET}\n"
  printf "    ${BOLD}pixeltui -explore=8 \"Daft Punk\" \"Get Lucky\"${RESET}\n"
  printf "    ${BOLD}pixeltui --help${RESET}\n\n"
  printf "  Press ${BOLD}↵ Enter${RESET} in the list to stream a track.\n\n"
}

# ── main ──────────────────────────────────────────────────────────────────────
main() {
  banner "pixeltui installer"
  detect_platform
  info "Platform: $OS/$ARCH"
  find_prefix
  info "Install prefix: $PREFIX"

  fix_ffplay_macos
  install_pixeltui
  install_ytdlp
  install_fast_ytdlp
  install_player
  setup_apikey
  check_path
  done_message
}

main "$@"
