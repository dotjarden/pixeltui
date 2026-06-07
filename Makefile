PREFIX  ?= /usr/local/bin
BINARY   = pixeltui
LDFLAGS  = -ldflags="-s -w" -trimpath
DISTDIR  = dist

.PHONY: build install uninstall release clean \
        deps-macos deps-linux stream-setup fast-ytdlp fix-ffplay demo help

# ── default ───────────────────────────────────────────────────────────────────

build:
	go build $(LDFLAGS) -o $(BINARY) ./tui

install: build
	@mkdir -p $(PREFIX)
	install -m 755 $(BINARY) $(PREFIX)/$(BINARY)
	@echo "Installed → $(PREFIX)/$(BINARY)"

uninstall:
	rm -f $(PREFIX)/$(BINARY)

# ── cross-platform release builds ─────────────────────────────────────────────
# Produces binaries for every supported platform in dist/
# Upload them as GitHub release assets and the install scripts will find them.

PLATFORMS = darwin/amd64 darwin/arm64 linux/amd64 linux/arm64 windows/amd64

release:
	@mkdir -p $(DISTDIR)
	@for platform in $(PLATFORMS); do \
		os=$$(echo $$platform | cut -d/ -f1); \
		arch=$$(echo $$platform | cut -d/ -f2); \
		out=$(DISTDIR)/pixeltui-$$os-$$arch; \
		[ "$$os" = "windows" ] && out=$$out.exe; \
		printf "  building %-40s" "$$out ..."; \
		GOOS=$$os GOARCH=$$arch go build $(LDFLAGS) -o $$out ./tui \
			&& echo "ok" || echo "FAILED"; \
	done
	@echo ""
	@ls -lh $(DISTDIR)/

clean:
	rm -f $(BINARY)
	rm -rf $(DISTDIR)/

# ── dependency installers ─────────────────────────────────────────────────────

# yt-dlp — required on all platforms (standalone binary, no package manager)
deps-macos:
	@echo "Installing yt-dlp (macOS standalone binary)..."
	curl -fsSL https://github.com/yt-dlp/yt-dlp/releases/latest/download/yt-dlp_macos \
		-o $(PREFIX)/yt-dlp
	chmod +x $(PREFIX)/yt-dlp
	@echo "Done — yt-dlp installed at $(PREFIX)/yt-dlp"
	@echo "Run 'make stream-setup' to add mpv for best streaming quality."

deps-linux:
	@echo "Installing yt-dlp (Linux standalone binary)..."
	mkdir -p ~/.local/bin
	curl -fsSL https://github.com/yt-dlp/yt-dlp/releases/latest/download/yt-dlp \
		-o ~/.local/bin/yt-dlp
	chmod +x ~/.local/bin/yt-dlp
	@echo "Done — yt-dlp installed at ~/.local/bin/yt-dlp"
	@echo "Install ffplay for audio: sudo apt install ffmpeg"

# mpv — recommended player: enables pause/seek/volume + OS Now Playing.
# macOS  : downloads the self-contained mpv.app bundle (arch-aware: arm64/Intel),
#          keeps it in ~/.pixeltui/mpv.app, and symlinks the inner binary into
#          PREFIX. No package manager required.
# Linux  : installs via the system package manager.
# Without mpv, audio still plays via ffplay/afplay but transport controls are off.
MPV_HOME ?= $(HOME)/.pixeltui
stream-setup:
	@if command -v mpv >/dev/null 2>&1; then \
		echo "mpv already installed: $$(command -v mpv)"; exit 0; \
	fi; \
	os=$$(uname -s); arch=$$(uname -m); \
	if [ "$$os" = "Darwin" ]; then \
		case "$$arch" in \
			arm64) file=mpv-arm64-latest.tar.gz ;; \
			*)     file=mpv-latest.tar.gz ;; \
		esac; \
		echo "Installing standalone mpv.app ($$arch) → $(MPV_HOME)/mpv.app ..."; \
		mkdir -p "$(MPV_HOME)"; tmp=$$(mktemp -d); \
		app=""; \
		if curl -fsSL "https://laboratory.stolendata.net/~djinn/mpv_osx/$$file" -o "$$tmp/mpv.tar.gz" \
			&& tar xzf "$$tmp/mpv.tar.gz" -C "$$tmp" \
			&& app=$$(find "$$tmp" -maxdepth 2 -name mpv.app -type d | head -1) \
			&& [ -n "$$app" ] \
			&& rm -rf "$(MPV_HOME)/mpv.app" \
			&& mv "$$app" "$(MPV_HOME)/mpv.app"; then \
			rm -rf "$$tmp"; mkdir -p "$(PREFIX)"; \
			ln -sf "$(MPV_HOME)/mpv.app/Contents/MacOS/mpv" "$(PREFIX)/mpv" \
				&& echo "Linked $(PREFIX)/mpv → bundle" \
				&& "$(PREFIX)/mpv" --version | head -1 \
				&& echo "Done. Restart pixeltui to enable pause/seek/volume."; \
		else \
			rm -rf "$$tmp"; \
			echo "Standalone download failed. Fallback: brew install mpv"; exit 1; \
		fi; \
	else \
		echo "Installing mpv via the system package manager..."; \
		if command -v apt-get >/dev/null 2>&1; then sudo apt-get install -y mpv; \
		elif command -v dnf >/dev/null 2>&1; then sudo dnf install -y mpv; \
		elif command -v pacman >/dev/null 2>&1; then sudo pacman -S --noconfirm mpv; \
		elif command -v zypper >/dev/null 2>&1; then sudo zypper install -y mpv; \
		else echo "No known package manager — install mpv from https://mpv.io"; exit 1; fi; \
	fi

# fast-ytdlp — install a pip yt-dlp into ~/.pixeltui/ytdlp-venv. The macOS
# standalone binary re-unpacks 35MB on every call (~8s startup tax); the pip
# build starts in ~0.6s, cutting play→audio latency ~7×. pixeltui auto-detects
# this venv and prefers it. Requires python3.
fast-ytdlp:
	@command -v python3 >/dev/null 2>&1 || { echo "python3 not found — install Python 3 first"; exit 1; }
	@echo "Installing fast pip yt-dlp → $(HOME)/.pixeltui/ytdlp-venv ..."
	@python3 -m venv --clear "$(HOME)/.pixeltui/ytdlp-venv"
	@"$(HOME)/.pixeltui/ytdlp-venv/bin/python" -m pip install -q -U yt-dlp mutagen
	@"$(HOME)/.pixeltui/ytdlp-venv/bin/yt-dlp" --version >/dev/null \
		&& echo "Done — pixeltui will auto-use it (~0.6s startup vs ~8s standalone)." \
		|| { echo "Install failed."; exit 1; }

# demo — render docs/demo.gif from demo.tape using VHS.
# Install the toolchain once:  brew install vhs ttyd ffmpeg   (or: go install
# github.com/charmbracelet/vhs@latest, plus ttyd + ffmpeg from your package mgr).
demo: build
	@command -v vhs >/dev/null 2>&1 || { echo "vhs not found — brew install vhs ttyd ffmpeg"; exit 1; }
	@mkdir -p docs
	vhs demo.tape
	@echo "Wrote docs/demo.gif"

# ── ffplay compat fix ─────────────────────────────────────────────────────────
# After `brew upgrade libvpx`, the dylib filename version can jump (e.g. .11 →
# .12) and break the ffplay binary. This creates a backwards-compat symlink so
# ffplay finds the library it was linked against.
fix-ffplay:
	@vpxdir=/usr/local/opt/libvpx/lib && \
	new=$$(ls "$$vpxdir"/libvpx.*.dylib 2>/dev/null | grep -vF libvpx.dylib | sort -V | tail -1) && \
	[ -z "$$new" ] && { echo "libvpx not found in $$vpxdir"; exit 1; } || true && \
	ver=$$(basename "$$new" .dylib | sed 's/libvpx\.//') && \
	prev=$$((ver - 1)) && \
	sym="$$vpxdir/libvpx.$$prev.dylib" && \
	{ [ -e "$$sym" ] && echo "$$sym already exists" || { ln -s "$$new" "$$sym" && echo "Linked libvpx.$$prev → libvpx.$$ver"; }; } && \
	ffplay -version 2>&1 | head -1

# ── help ──────────────────────────────────────────────────────────────────────
help:
	@echo ""
	@echo "  pixeltui Makefile"
	@echo ""
	@echo "  Build & install:"
	@echo "    make build          Build binary in current directory"
	@echo "    make install        Build and install to PREFIX (default: /usr/local/bin)"
	@echo "    make uninstall      Remove installed binary"
	@echo "    make release        Cross-compile for all platforms → dist/"
	@echo "    make clean          Delete built binary and dist/"
	@echo ""
	@echo "  Dependencies:"
	@echo "    make deps-macos     Install yt-dlp standalone binary (macOS)"
	@echo "    make deps-linux     Install yt-dlp standalone binary (Linux)"
	@echo "    make stream-setup   Install mpv (macOS bundle arm64/Intel, or Linux pkg)"
	@echo "    make fast-ytdlp     Install pip yt-dlp (~7× faster cold starts)"
	@echo "    make fix-ffplay     Fix broken ffplay after brew libvpx upgrade (macOS)"
	@echo ""
	@echo "  Variables:"
	@echo "    PREFIX=/path        Install location  (default: /usr/local/bin)"
	@echo ""
	@echo "  First-time setup (any platform): ./install.sh"
	@echo "  Windows setup:                   .\\install.ps1"
	@echo ""
