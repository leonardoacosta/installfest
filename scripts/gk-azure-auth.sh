# gk-azure-auth.sh — shared definition for attaching the GitKraken CLI's Azure
# DevOps provider token. SOURCE this (from bash or zsh); do not execute it.
#
# Mirrors gk-github-auth.sh. Used by both callers so they can never drift:
#   * home/run_onchange_after_ado-auth.sh.tmpl   (chezmoi bootstrap, fresh setup)
#   * the `gkado` helper in home/dot_zsh/rc/shared.zsh (manual re-attach)

# gk_attach_azure [token] [url]
#   token : Azure DevOps PAT. Defaults to $AZURE_DEVOPS_EXT_PAT.
#   url   : optional org URL (--url), e.g. https://dev.azure.com/brownandbrowninc.
#           Defaults to $ADO_URL. Cloud ADO usually resolves from the PAT, so
#           leave unset unless gk asks for it. A PAT scoped to "all accessible
#           organizations" covers brownandbrowninc + brownandbrown at once.
#   Guards: gk installed, token present, gk account signed in.
gk_attach_azure() {
	local _tok="${1:-${AZURE_DEVOPS_EXT_PAT:-}}"
	local _url="${2:-${ADO_URL:-}}"
	command -v gk >/dev/null 2>&1 || { echo "gk-azure-auth: gk (GitKraken CLI) not installed." >&2; return 1; }
	[ -n "$_tok" ]                || { echo "gk-azure-auth: no token (pass one or set AZURE_DEVOPS_EXT_PAT)." >&2; return 1; }
	gk whoami >/dev/null 2>&1      || { echo "gk-azure-auth: not signed in — run 'gk auth login' first." >&2; return 1; }
	if [ -n "$_url" ]; then
		gk provider add azure -t "$_tok" --url "$_url" >/dev/null 2>&1
	else
		gk provider add azure -t "$_tok" >/dev/null 2>&1
	fi
}
