#!/usr/bin/env bash
# shellcheck disable=SC2015
# (SC2015) `cmd && success "ok" || warning "fail"` recurs throughout for status
# lines. Both arms are harmless prints — never load-bearing logic — so the
# A && B || C caveat does not apply. Suppressed file-wide rather than per-line.
#
# platform/bootstrap.sh — one-command cold-start orchestrator for a fresh machine.
#
# macOS-primary (degrades gracefully on Linux). Idempotent + re-runnable: every
# step detects its own completion and skips/refreshes rather than redoing work.
#
# Driven by the 2026-06-14 MacBook restore, which exposed every cold-start gap:
# missing Homebrew bundle, no git identity (commits as @Host.local), no Tailscale
# hostname, un-provisioned Xcode/Metal toolchain, missing signing cert, no gh auth,
# and no project clones.
#
# This script ORCHESTRATES existing scaffolding — it does not reinvent it:
#   - platform/homebrew/Brewfile        (declarative tool list)
#   - scripts/utils.sh                  (info/success/warning/error helpers)
#   - scripts/prerequisites.sh          (install_xcode / install_homebrew)
#   - chezmoi apply                     (ssh config, authorized_keys, gitconfig)
#   - home/projects.toml                (project registry for the clone loop)
#
# Three steps require supervised manual interaction behind Apple's walls and
# PAUSE for the operator (Apple-ID 2FA, signing cert minting, GitHub device flow).
#
# Usage:
#   bash platform/bootstrap.sh          # full run
#   bash platform/bootstrap.sh --help
#
# NOT `set -e`: we want to continue past soft failures (one bad repo clone, an
# optional cask) and surface them as warnings rather than aborting the whole run.
set -uo pipefail

# ---------------------------------------------------------------------------
# Locate the repo + reuse shared helpers
# ---------------------------------------------------------------------------
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
BREWFILE="$REPO_ROOT/platform/homebrew/Brewfile"
PROJECTS_TOML="$REPO_ROOT/home/projects.toml"
# Optional companion file mapping project code -> clone URL. projects.toml has
# no URL field by design (URLs are heterogeneous: GitHub under two orgs + Azure
# DevOps with embedded PATs), so the clone loop never *guesses* a URL. It reads
# overrides from here when present and warns+skips otherwise. See § clone loop.
REPOS_TOML="${BOOTSTRAP_REPOS_TOML:-$REPO_ROOT/platform/bootstrap-repos.toml}"

# scripts/utils.sh gives us info/success/warning/error (tput-coloured).
if [[ -f "$REPO_ROOT/scripts/utils.sh" ]]; then
	# shellcheck disable=SC1091
	. "$REPO_ROOT/scripts/utils.sh"
else
	info()    { printf '==> %s\n' "$1"; }
	success() { printf '==> %s\n' "$1"; }
	warning() { printf 'WARN: %s\n' "$1"; }
	error()   { printf 'ERROR: %s\n' "$1"; }
fi

OS="$(uname -s)"
IS_MAC=0
[[ "$OS" == "Darwin" ]] && IS_MAC=1

# ---------------------------------------------------------------------------
# Presentation helpers
# ---------------------------------------------------------------------------
section() {
	printf '\n'
	info "============================================================"
	info "  $1"
	info "============================================================"
}

# gate <title> -- print supervised-manual instructions, then block on Enter.
# The body (what-to-do lines) is fed on stdin via a heredoc by the caller.
gate() {
	local title="$1"
	printf '\n'
	warning "------------------------------------------------------------"
	warning "  MANUAL GATE: $title"
	warning "------------------------------------------------------------"
	while IFS= read -r line; do
		printf '    %s\n' "$line"
	done
	printf '\n'
	# In a non-interactive context (no TTY) we cannot pause — warn and continue
	# so an automated/CI invocation does not hang forever.
	if [[ -t 0 ]]; then
		read -r -p "    Press Enter when done (or Ctrl-C to abort)... " _
	else
		warning "Non-interactive shell — skipping pause for: $title"
	fi
}

# ---------------------------------------------------------------------------
section "Nexus cold-start bootstrap  ($OS, $(uname -m))"
# ---------------------------------------------------------------------------
info "Repo:     $REPO_ROOT"
info "Brewfile: $BREWFILE"
if [[ $IS_MAC -eq 0 ]]; then
	warning "Non-macOS host: macOS-only steps (Xcode, Metal, Tailscale.app CLI) are skipped."
fi

# ===========================================================================
# GATE 1 — Remote Login (SSH) so the mesh + remote deploy hooks work
# ===========================================================================
section "Step 1/9: Remote Login (SSH server)"
if [[ $IS_MAC -eq 1 ]]; then
	if systemsetup -getremotelogin 2>/dev/null | grep -qi 'On'; then
		success "Remote Login already enabled — skipping gate."
	else
		gate "Enable Remote Login (SSH server)" <<-'EOF'
		The SSH mesh + cross-machine deploy hooks need inbound SSH.
		Run in another terminal:

		    sudo systemsetup -setremotelogin on

		Then allow your user under:
		    System Settings -> General -> Sharing -> Remote Login
		EOF
	fi
else
	if command -v systemctl >/dev/null 2>&1 && systemctl is-active --quiet sshd 2>/dev/null; then
		success "sshd active — skipping."
	else
		warning "sshd not detected. Enable it for your distro (e.g. 'sudo systemctl enable --now sshd')."
	fi
fi

# ===========================================================================
# Step 2 — Homebrew bundle
# ===========================================================================
section "Step 2/9: Homebrew bundle"
if command -v brew >/dev/null 2>&1; then
	if [[ -f "$BREWFILE" ]]; then
		info "Running brew bundle (this can take a while on a fresh machine)..."
		if brew bundle --file="$BREWFILE"; then
			success "brew bundle complete."
		else
			warning "brew bundle returned non-zero (some casks may need manual install)."
		fi
	else
		error "Brewfile not found at $BREWFILE — skipping."
	fi
else
	warning "Homebrew not installed. Install it, then re-run this script:"
	# shellcheck disable=SC2016  # literal command for the operator to copy, not expanded
	warning '  /bin/bash -c "$(curl -fsSL https://raw.githubusercontent.com/Homebrew/install/HEAD/install.sh)"'
fi

# ===========================================================================
# Step 3 — chezmoi apply (ssh config, authorized_keys, gitconfig identity)
# ===========================================================================
section "Step 3/9: chezmoi apply (dotfiles + git identity)"
if command -v chezmoi >/dev/null 2>&1; then
	if chezmoi apply; then
		success "chezmoi apply complete — ~/.gitconfig now pins leonardoacosta <leo@priceless.dev>."
	else
		warning "chezmoi apply returned non-zero — inspect with 'chezmoi apply -v'."
	fi
else
	warning "chezmoi not installed (expected from the Brewfile). Skipping dotfile apply."
fi

# ===========================================================================
# Step 4 — Tailscale hostname
# ===========================================================================
section "Step 4/9: Tailscale hostname"
TS_CLI=""
if [[ $IS_MAC -eq 1 && -x "/Applications/Tailscale.app/Contents/MacOS/Tailscale" ]]; then
	TS_CLI="/Applications/Tailscale.app/Contents/MacOS/Tailscale"
elif command -v tailscale >/dev/null 2>&1; then
	TS_CLI="$(command -v tailscale)"
fi
if [[ -n "$TS_CLI" ]]; then
	TS_HOSTNAME="$(hostname -s 2>/dev/null || hostname)"
	info "Setting Tailscale hostname to '$TS_HOSTNAME' via $TS_CLI ..."
	if "$TS_CLI" set --hostname="$TS_HOSTNAME" 2>/dev/null; then
		success "Tailscale hostname set to $TS_HOSTNAME."
	else
		warning "tailscale set failed — is Tailscale logged in? Run '$TS_CLI up' first."
	fi
else
	warning "Tailscale CLI not found. Install Tailscale.app (in the Brewfile) and run 'tailscale up'."
fi

# ===========================================================================
# Step 5 — macOS Xcode provisioning (first-launch + Metal toolchain)
# ===========================================================================
section "Step 5/9: Xcode provisioning (first-launch + Metal toolchain)"
if [[ $IS_MAC -eq 1 ]]; then
	if [[ -d /Applications/Xcode.app ]]; then
		info "Running 'sudo xcodebuild -runFirstLaunch' (installs required components)..."
		sudo xcodebuild -runFirstLaunch 2>/dev/null \
			&& success "xcodebuild -runFirstLaunch complete." \
			|| warning "xcodebuild -runFirstLaunch failed (re-run after 'sudo xcode-select -s /Applications/Xcode.app')."
		# Metal toolchain download needs NO sudo.
		info "Downloading the Metal toolchain (no sudo required)..."
		xcodebuild -downloadComponent MetalToolchain 2>/dev/null \
			&& success "Metal toolchain present." \
			|| warning "MetalToolchain download failed (re-run: xcodebuild -downloadComponent MetalToolchain)."
	else
		warning "Xcode.app not present — provisioning deferred to GATE 2 below."
	fi
else
	info "Not macOS — skipping Xcode provisioning."
fi

# ===========================================================================
# GATE 2 — Install Xcode (Apple-ID 2FA) when absent
# ===========================================================================
section "Step 6/9: Xcode install gate"
if [[ $IS_MAC -eq 1 && ! -d /Applications/Xcode.app ]]; then
	gate "Install Xcode (Apple-ID 2FA required)" <<-'EOF'
	Xcode.app is missing. Install it (xcodes is in the Brewfile):

	    xcodes install --latest

	This prompts for your Apple ID + a 2FA code, then downloads several GB.
	After it finishes, re-run this bootstrap to provision the Metal toolchain.
	EOF
else
	[[ $IS_MAC -eq 1 ]] && success "Xcode.app present — install gate not needed." \
		|| info "Not macOS — Xcode install gate skipped."
fi

# ===========================================================================
# GATE 3 — Signing identity (Xcode account sign-in + Apple Development cert)
# ===========================================================================
section "Step 7/9: Code-signing identity gate"
if [[ $IS_MAC -eq 1 ]]; then
	# `security find-identity` prints one "  1) <hash> "name"" line per cert,
	# then a trailing "N valid identities found". Count the numbered cert lines.
	CERT_COUNT="$(security find-identity -v -p codesigning 2>/dev/null | grep -cE '^[[:space:]]*[0-9]+\)' || true)"
	if [[ "${CERT_COUNT:-0}" -gt 0 ]]; then
		success "$CERT_COUNT code-signing identity(ies) present — skipping gate."
	else
		gate "Mint an Apple Development signing certificate" <<-'EOF'
		No valid code-signing identity found. In Xcode:

		    Xcode -> Settings -> Accounts -> (+) -> sign in with your Apple ID
		    Then select the team -> "Manage Certificates..." -> (+) -> Apple Development

		Verify afterwards:
		    security find-identity -v -p codesigning
		EOF
	fi
else
	info "Not macOS — signing gate skipped."
fi

# ===========================================================================
# Step 8 — GitHub auth
# ===========================================================================
section "Step 8/9: GitHub authentication"
if command -v gh >/dev/null 2>&1; then
	if gh auth status >/dev/null 2>&1; then
		success "gh already authenticated."
	else
		gate "Authenticate the GitHub CLI" <<-'EOF'
		gh is not logged in. Run (interactive device flow):

		    gh auth login

		Choose GitHub.com -> HTTPS -> "Login with a web browser",
		then paste the one-time code at https://github.com/login/device
		EOF
		# Best-effort verify after the operator returns.
		gh auth status >/dev/null 2>&1 \
			&& success "gh authentication confirmed." \
			|| warning "gh still not authenticated — re-run 'gh auth login'."
	fi
else
	warning "gh not installed (expected from the Brewfile). Skipping GitHub auth."
fi

# ===========================================================================
# Step 9 — Clone + install project repos from projects.toml
# ===========================================================================
section "Step 9/9: Clone + install projects (from projects.toml)"

# Resolve a python3 with tomllib (stdlib in 3.11+). Mirrors the resolver in
# scripts/cmux-workspaces.sh — macOS /usr/bin/python3 (3.9) lacks tomllib.
BS_PY=""
for _py in python3.14 python3.13 python3.12 python3.11 python3; do
	if command -v "$_py" >/dev/null 2>&1 && "$_py" -c 'import tomllib' 2>/dev/null; then
		BS_PY="$_py"; break
	fi
done

clone_and_install() {
	if [[ -z "$BS_PY" ]]; then
		warning "No python3 with tomllib (need >= 3.11) — cannot parse projects.toml. Skipping clone loop."
		return 0
	fi
	if [[ ! -f "$PROJECTS_TOML" ]]; then
		warning "projects.toml not found at $PROJECTS_TOML — skipping clone loop."
		return 0
	fi

	local home_base="$HOME"
	# Emit "code<TAB>abs_path" pairs (only locally-relevant tiers). The path
	# field is relative to the home dir, exactly as projects.toml documents.
	local pairs
	pairs="$(
		TOML_FILE="$PROJECTS_TOML" HOME_BASE="$home_base" "$BS_PY" <<-'PYEOF'
		import os, tomllib
		with open(os.environ["TOML_FILE"], "rb") as f:
		    data = tomllib.load(f)
		base = os.environ["HOME_BASE"]
		for p in data.get("projects", []):
		    code = p.get("code", "")
		    path = p.get("path", "")
		    tiers = p.get("tiers", [])
		    # Only repos that have a "local" tier land on a workstation.
		    if "local" not in tiers:
		        continue
		    if not code or not path:
		        continue
		    print(f"{code}\t{os.path.join(base, path)}")
		PYEOF
	)"

	if [[ -z "$pairs" ]]; then
		warning "No local-tier projects parsed from projects.toml."
		return 0
	fi

	# Optional override map: code -> clone URL. Parsed only if the file exists.
	# Format (TOML): a [repos] table of  code = "git-url"  entries.
	declare -A REPO_URL=()
	if [[ -f "$REPOS_TOML" ]]; then
		while IFS=$'\t' read -r rc ru; do
			[[ -n "$rc" ]] && REPO_URL["$rc"]="$ru"
		done < <(
			TOML_FILE="$REPOS_TOML" "$BS_PY" <<-'PYEOF'
			import os, tomllib
			with open(os.environ["TOML_FILE"], "rb") as f:
			    data = tomllib.load(f)
			for code, url in (data.get("repos", {}) or {}).items():
			    print(f"{code}\t{url}")
			PYEOF
		)
		info "Loaded $(printf '%s\n' "${!REPO_URL[@]}" | grep -c . || true) repo URL override(s) from $REPOS_TOML."
	else
		info "No $REPOS_TOML override file — only existing clones are refreshed."
		info "  (projects.toml has no URL field; URLs are heterogeneous GitHub/Azure with PATs.)"
	fi

	local cloned=0 pulled=0 skipped=0 failed=0
	while IFS=$'\t' read -r code path; do
		[[ -z "$code" ]] && continue
		if [[ -d "$path/.git" ]]; then
			info "[$code] exists — pulling $path"
			if git -C "$path" pull --ff-only >/dev/null 2>&1; then
				pulled=$((pulled + 1))
			else
				warning "[$code] git pull failed (dirty tree or non-ff?) — left untouched."
				failed=$((failed + 1))
			fi
		else
			local url="${REPO_URL[$code]:-}"
			if [[ -z "$url" ]]; then
				warning "[$code] missing at $path and no clone URL known — skipping (add it to $(basename "$REPOS_TOML"))."
				skipped=$((skipped + 1))
				continue
			fi
			info "[$code] cloning $url -> $path"
			mkdir -p "$(dirname "$path")"
			if git clone "$url" "$path" >/dev/null 2>&1; then
				cloned=$((cloned + 1))
			else
				warning "[$code] git clone failed — continuing."
				failed=$((failed + 1))
				continue
			fi
		fi

		# Run a per-repo installer if present. nx-style: deploy/install.sh.
		# Otherwise a JS workspace: pnpm install. Never abort the whole run.
		if [[ -x "$path/deploy/install.sh" ]]; then
			info "[$code] running deploy/install.sh"
			( cd "$path" && ./deploy/install.sh ) \
				|| warning "[$code] deploy/install.sh failed — continuing."
		elif [[ -f "$path/pnpm-lock.yaml" || -f "$path/package.json" ]] && command -v pnpm >/dev/null 2>&1; then
			info "[$code] running pnpm install"
			( cd "$path" && pnpm install ) >/dev/null 2>&1 \
				&& success "[$code] pnpm install complete." \
				|| warning "[$code] pnpm install failed — continuing."
		fi
	done <<< "$pairs"

	printf '\n'
	success "Clone loop: $cloned cloned, $pulled pulled, $skipped skipped (no URL), $failed failed."
}

clone_and_install

# ===========================================================================
section "Bootstrap complete"
# ===========================================================================
success "Cold-start steps finished. Re-run this script any time — it is idempotent."
info "Re-verify health later with:  chezmoi apply  (runs run_after_doctor.sh)"
