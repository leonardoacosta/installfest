# gk-github-auth.sh — shared definition for attaching the GitKraken CLI's GitHub
# provider token. SOURCE this (from bash or zsh); do not execute it.
#
# gk has no native env-var auth, so the GitHub PAT must be passed via `-t`. This
# single definition is used by both callers so they can never drift:
#   * home/run_onchange_after_github-auth.sh.tmpl  (chezmoi bootstrap, fresh setup)
#   * the `gkauth` helper in home/dot_zsh/rc/shared.zsh (manual post-rotation re-attach)

# gk_attach_github [token]
#   token : GitHub PAT to attach. Defaults to $GH_TOKEN.
#   Guards: gk installed, token present, gk account signed in.
#   Returns non-zero with a stderr reason on any guard failure.
gk_attach_github() {
	local _tok="${1:-${GH_TOKEN:-}}"
	command -v gk >/dev/null 2>&1 || { echo "gk-github-auth: gk (GitKraken CLI) not installed." >&2; return 1; }
	[ -n "$_tok" ]                || { echo "gk-github-auth: no token (pass one or set GH_TOKEN)." >&2; return 1; }
	gk whoami >/dev/null 2>&1      || { echo "gk-github-auth: not signed in — run 'gk auth login' first." >&2; return 1; }
	gk provider add github -t "$_tok" >/dev/null 2>&1
}
