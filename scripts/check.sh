#!/usr/bin/env bash
# check.sh - one-command verification baseline for this dotfiles repo.
#
# Runs static checks that a broken `chezmoi apply` on a fresh machine would
# otherwise only surface at apply time: zsh syntax, POSIX/bash syntax, chezmoi
# template render (+ bash -n on rendered *.sh.tmpl), shellcheck (error severity),
# and — when initialized — `terraform validate`. Also runs each apps/* suite
# (wavetui go test, ctx-scan/daily-brief bun test, cc-tmux self-test), each
# skipped with a warning when its toolchain is absent.
#
# Intentionally NOT `set -e`: every section must run and report so one failure
# does not hide the rest. Sections whose tool is absent are skipped with a warning.
#
# Usage: scripts/check.sh   (or: npm run check)   -> exit 0 all pass, 1 any fail.

if [[ "${1:-}" == "--help" || "${1:-}" == "-h" ]]; then
	cat <<'EOF'
Usage: scripts/check.sh   (or: npm run check)

One-command verification baseline for this dotfiles repo: zsh syntax,
POSIX/bash syntax, chezmoi template render (+ bash -n on rendered
*.sh.tmpl), shellcheck (error severity), terraform validate when
initialized, and each apps/* suite (wavetui go test, ctx-scan/daily-brief
bun test, cc-tmux self-test — each skipped with a warning when its
toolchain is absent). Every section runs and reports even if an earlier
one fails.

Exit: 0 all pass, 1 any fail.
EOF
	exit 0
fi

set -uo pipefail

# --- log helpers (reuse repo's scripts/utils.sh, fan-in 22) -----------------
if [ -f "scripts/utils.sh" ]; then
    # shellcheck source=scripts/utils.sh
    . "scripts/utils.sh"
elif command -v git >/dev/null 2>&1 && [ -f "$(git rev-parse --show-toplevel 2>/dev/null)/scripts/utils.sh" ]; then
    # shellcheck disable=SC1091
    . "$(git rev-parse --show-toplevel)/scripts/utils.sh"
else
    info()    { printf '==> %s\n' "$1"; }
    success() { printf '==> %s\n' "$1"; }
    error()   { printf '==> %s\n' "$1"; }
    warning() { printf '==> %s\n' "$1"; }
fi

# Resolve repo root so file globs are stable regardless of CWD.
ROOT="$(git rev-parse --show-toplevel 2>/dev/null || pwd)"
cd "$ROOT" || { error "cannot cd to repo root"; exit 1; }
# Re-source utils now that we're at root (the first attempt used a relative path
# that only resolves when already at root; this makes colors work from anywhere).
[ -f "scripts/utils.sh" ] && . "scripts/utils.sh"

# Pre-existing shellcheck findings (error severity) — do NOT silence globally.
# Burn down via docs/plans/005+; reviewer rejects additions without a plan ref.
#   scripts/mux-remote.sh    -> #!/bin/zsh, SC1071 (shellcheck cannot parse zsh)
#   scripts/gk-github-auth.sh -> sourced fragment, no shebang, SC2148
SHELLCHECK_EXCLUDE=(
    "scripts/mux-remote.sh"
    "scripts/gk-github-auth.sh"
)

FAIL=0
TMP_RENDER="$(mktemp)"; TMP_ERR="$(mktemp)"
trap 'rm -f "$TMP_RENDER" "$TMP_ERR"' EXIT

is_excluded() {
    local needle="$1" e
    for e in "${SHELLCHECK_EXCLUDE[@]}"; do [ "$e" = "$needle" ] && return 0; done
    return 1
}

# Build the shell-script file set (shared by sh-syntax + shellcheck sections).
mapfile -d '' SH_FILES < <(
    find scripts -name '*.sh' -type f -print0
    find ssh-mesh/scripts -name '*.sh' -type f \
        -not -path 'ssh-mesh/scripts/remote/cmux-bridge/*' -print0
    find packages/workspace/bin packages/workspace/lib/trackers \
        -type f -not -name '*.md' -print0
)
for f in platform/*.sh; do [ -f "$f" ] && SH_FILES+=("$f"); done

# --- Section 1: zsh-syntax --------------------------------------------------
section_zsh() {
    if ! command -v zsh >/dev/null 2>&1; then
        warning "skip: zsh not installed (zsh-syntax)"; return 0
    fi
    local bad=0 f
    mapfile -d '' ZSH_FILES < <(find home/dot_zsh -name '*.zsh' -type f -print0)
    ZSH_FILES+=("home/dot_zshrc")
    for f in "${ZSH_FILES[@]}"; do
        [ -f "$f" ] || continue
        zsh -n "$f" 2>"$TMP_ERR" || { error "  zsh -n: $f"; sed 's/^/    /' "$TMP_ERR"; bad=1; }
    done
    if [ "$bad" -eq 0 ]; then success "PASS: zsh-syntax (${#ZSH_FILES[@]} files)"
    else error "FAIL: zsh-syntax"; FAIL=1; fi
}

# --- Section 2: sh-syntax ---------------------------------------------------
section_sh() {
    local bad=0 f
    for f in "${SH_FILES[@]}"; do
        bash -n "$f" 2>"$TMP_ERR" || { error "  bash -n: $f"; sed 's/^/    /' "$TMP_ERR"; bad=1; }
    done
    # POSIX hooks (extensionless, #!/bin/sh) under scripts/hooks/
    if command -v sh >/dev/null 2>&1; then
        for f in scripts/hooks/*; do
            [ -f "$f" ] || continue
            case "$f" in *.sh) continue;; esac   # .sh already covered above
            head -1 "$f" | grep -q 'bin/sh' || continue
            sh -n "$f" 2>"$TMP_ERR" || { error "  sh -n: $f"; sed 's/^/    /' "$TMP_ERR"; bad=1; }
        done
    fi
    if [ "$bad" -eq 0 ]; then success "PASS: sh-syntax (${#SH_FILES[@]} bash + hooks)"
    else error "FAIL: sh-syntax"; FAIL=1; fi
}

# --- Section 3: template-render ---------------------------------------------
section_template() {
    if ! command -v chezmoi >/dev/null 2>&1; then
        warning "skip: chezmoi not installed (template-render)"; return 0
    fi
    local bad=0 n=0 f
    while IFS= read -r -d '' f; do
        n=$((n + 1))
        if ! chezmoi execute-template < "$f" >"$TMP_RENDER" 2>"$TMP_ERR"; then
            error "  render: $f"; sed 's/^/    /' "$TMP_ERR"; bad=1; continue
        fi
        case "$f" in
            *.sh.tmpl)
                bash -n "$TMP_RENDER" 2>"$TMP_ERR" \
                    || { error "  rendered bash -n: $f"; sed 's/^/    /' "$TMP_ERR"; bad=1; }
                ;;
        esac
    done < <(find home -name '*.tmpl' -type f -print0)
    if [ "$bad" -eq 0 ]; then success "PASS: template-render ($n templates)"
    else error "FAIL: template-render"; FAIL=1; fi
}

# --- Section 4: shellcheck --------------------------------------------------
section_shellcheck() {
    if ! command -v shellcheck >/dev/null 2>&1; then
        warning "skip: shellcheck not installed (shellcheck)"; return 0
    fi
    local bad=0 n=0 x=0 f
    for f in "${SH_FILES[@]}"; do
        if is_excluded "$f"; then x=$((x + 1)); continue; fi
        n=$((n + 1))
        shellcheck --severity=error "$f" >"$TMP_ERR" 2>&1 \
            || { error "  shellcheck: $f"; sed 's/^/    /' "$TMP_ERR"; bad=1; }
    done
    if [ "$bad" -eq 0 ]; then success "PASS: shellcheck ($n checked, $x excluded)"
    else error "FAIL: shellcheck"; FAIL=1; fi
}

# --- Section 5: terraform (conditional) -------------------------------------
section_terraform() {
    if ! command -v terraform >/dev/null 2>&1; then
        warning "skip: terraform not installed (terraform)"; return 0
    fi
    if [ ! -d infra/environments/prod/.terraform ]; then
        warning "skip: infra/environments/prod not initialized (terraform)"; return 0
    fi
    if terraform -chdir=infra/environments/prod validate -no-color >"$TMP_ERR" 2>&1; then
        success "PASS: terraform"
    else
        error "FAIL: terraform"; sed 's/^/    /' "$TMP_ERR"; FAIL=1
    fi
}

# --- Section 6: apps/wavetui go test -----------------------------------------
section_apps_go() {
    if ! command -v go >/dev/null 2>&1; then
        warning "SKIP: go not installed (apps/wavetui untested)"; return 0
    fi
    if (cd "$ROOT/apps/wavetui" && go test ./... >"$TMP_ERR" 2>&1); then
        success "PASS: wavetui go test"
    else
        error "FAIL: wavetui go test"; sed 's/^/    /' "$TMP_ERR"; FAIL=1
    fi
}

# --- Section 7: apps/ctx-scan + apps/daily-brief bun test --------------------
section_apps_bun() {
    if ! command -v bun >/dev/null 2>&1; then
        warning "SKIP: bun not installed (apps/ctx-scan, apps/daily-brief untested)"; return 0
    fi
    if (cd "$ROOT/apps/ctx-scan" && bun test >"$TMP_ERR" 2>&1); then
        success "PASS: ctx-scan bun test"
    else
        error "FAIL: ctx-scan bun test"; sed 's/^/    /' "$TMP_ERR"; FAIL=1
    fi
    if (cd "$ROOT/apps/daily-brief" && bun test >"$TMP_ERR" 2>&1); then
        success "PASS: daily-brief bun test"
    else
        error "FAIL: daily-brief bun test"; sed 's/^/    /' "$TMP_ERR"; FAIL=1
    fi
}

# --- Section 8: apps/cc-tmux self-test ---------------------------------------
section_apps_cctmux() {
    if ! command -v cc-tmux >/dev/null 2>&1; then
        warning "SKIP: cc-tmux not installed (apps/cc-tmux untested)"; return 0
    fi
    if cc-tmux self-test >"$TMP_ERR" 2>&1; then
        success "PASS: cc-tmux self-test"
    else
        error "FAIL: cc-tmux self-test"; sed 's/^/    /' "$TMP_ERR"; FAIL=1
    fi
}

info "check.sh — verifying repo (root: $ROOT)"
section_zsh
section_sh
section_template
section_shellcheck
section_terraform
section_apps_go
section_apps_bun
section_apps_cctmux

if [ "$FAIL" -eq 0 ]; then success "ALL CHECKS PASSED"; else error "CHECKS FAILED"; fi
exit "$FAIL"
