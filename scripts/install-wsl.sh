#!/usr/bin/env bash
# Sets up den in WSL (or any Ubuntu/Debian): Git, tmux, Go, Node, the GitHub Copilot CLI, and den itself.
# Run it inside Ubuntu, not PowerShell:
#   curl -fsSL https://raw.githubusercontent.com/IgorAmi52/den/main/scripts/install-wsl.sh | bash
# Running it again updates den and skips whatever is already installed.
set -euo pipefail

minimumGoVersion="1.26"
minimumNodeMajorVersion=22
minimumTmuxVersion="3.3"
denSourceDirectory="${DEN_SOURCE_DIRECTORY:-$HOME/.local/src/den}"
denRepository="https://github.com/IgorAmi52/den.git"

step() { printf '\n\033[1;34m==>\033[0m %s\n' "$1"; }
warn() { printf '\033[1;33m!\033[0m %s\n' "$1"; }

# versionAtLeast is true when the first dotted version is the second or newer.
versionAtLeast() { [ "$(printf '%s\n%s\n' "$2" "$1" | sort -V | head -n 1)" = "$2" ]; }

if [ "$(uname -s)" != "Linux" ]; then
	echo "This script is for Linux inside WSL. On macOS, follow the README instead." >&2
	exit 1
fi
if ! command -v apt-get >/dev/null; then
	echo "This script needs apt, as on Ubuntu or Debian." >&2
	exit 1
fi

step "Installing Git, tmux and build tools"
sudo apt-get update -qq
sudo apt-get install -y -qq git tmux build-essential curl ca-certificates
tmuxVersion="$(tmux -V | grep -oE '[0-9]+(\.[0-9]+)?' | head -n 1)"
if ! versionAtLeast "$tmuxVersion" "$minimumTmuxVersion"; then
	warn "tmux $tmuxVersion is older than $minimumTmuxVersion, which den's popups need. Use Ubuntu 24.04 or newer (wsl --install -d Ubuntu-24.04)."
fi

step "Installing Go $minimumGoVersion or newer"
export PATH="/usr/local/go/bin:$HOME/.local/bin:$PATH"
installedGoVersion="$(go version 2>/dev/null | grep -oE 'go[0-9]+\.[0-9]+(\.[0-9]+)?' | head -n 1 | sed 's/^go//' || true)"
if [ -n "$installedGoVersion" ] && versionAtLeast "$installedGoVersion" "$minimumGoVersion"; then
	echo "Go $installedGoVersion is already installed."
else
	case "$(uname -m)" in
	x86_64) goArchitecture="amd64" ;;
	aarch64 | arm64) goArchitecture="arm64" ;;
	*)
		echo "No Go download for $(uname -m)." >&2
		exit 1
		;;
	esac
	latestGoRelease="$(curl -fsSL 'https://go.dev/VERSION?m=text' | head -n 1)"
	goArchive="$(mktemp)"
	curl -fsSL "https://go.dev/dl/$latestGoRelease.linux-$goArchitecture.tar.gz" -o "$goArchive"
	sudo rm -rf /usr/local/go
	sudo tar -C /usr/local -xzf "$goArchive"
	rm -f "$goArchive"
	echo "Installed $(go version)."
fi

step "Installing Node $minimumNodeMajorVersion or newer"
export NVM_DIR="${NVM_DIR:-$HOME/.nvm}"
if [ -s "$NVM_DIR/nvm.sh" ]; then
	# nvm's script reads unset variables, which set -u would stop on.
	set +u
	. "$NVM_DIR/nvm.sh"
	set -u
fi
nodeMajorVersion="$(node --version 2>/dev/null | sed -E 's/^v([0-9]+).*/\1/' || true)"
if [ -n "$nodeMajorVersion" ] && [ "$nodeMajorVersion" -ge "$minimumNodeMajorVersion" ]; then
	echo "Node $(node --version) is already installed."
else
	if [ ! -s "$NVM_DIR/nvm.sh" ]; then
		curl -fsSL https://raw.githubusercontent.com/nvm-sh/nvm/master/install.sh | bash
	fi
	set +u
	. "$NVM_DIR/nvm.sh"
	nvm install --lts
	nvm alias default 'lts/*'
	set -u
	echo "Installed Node $(node --version)."
fi

step "Installing the GitHub Copilot CLI"
# A Windows copilot under /mnt can run from WSL but can't work with den, so it doesn't count.
if command -v copilot >/dev/null && [[ "$(command -v copilot)" != /mnt/* ]]; then
	echo "Copilot CLI is already installed: $(command -v copilot)."
else
	npm install -g @github/copilot
fi

step "Building den"
if [ -d "$denSourceDirectory/.git" ]; then
	git -C "$denSourceDirectory" pull --ff-only
else
	mkdir -p "$(dirname "$denSourceDirectory")"
	git clone "$denRepository" "$denSourceDirectory"
fi
make -C "$denSourceDirectory" install

step "Adding Go and den to your PATH"
pathLine='export PATH="/usr/local/go/bin:$HOME/.local/bin:$PATH"'
for shellStartupFile in "$HOME/.bashrc" "$HOME/.zshrc"; do
	if [ -f "$shellStartupFile" ] && ! grep -qF "$pathLine" "$shellStartupFile"; then
		printf '\n# Added by den'"'"'s install-wsl.sh\n%s\n' "$pathLine" >>"$shellStartupFile"
		echo "Updated $shellStartupFile."
	fi
done

step "Done"
cat <<'EOF'
Next:
  1. Open a new terminal tab, so the PATH changes apply.
  2. Run `copilot`, type /login, finish signing in in your browser, then exit.
  3. cd into a repo and run `den`. Your Windows drives are under /mnt/c, but repos cloned
     into your WSL home (like ~/code) are much faster.
  4. In den, press M and pick copilot, so den's own model calls also go through Copilot.

Optional: to edit context files in VS Code, add  export EDITOR="code --wait"  to ~/.bashrc.
Without it, den opens them in nano.
EOF
