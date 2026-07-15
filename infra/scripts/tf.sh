#!/usr/bin/env bash
set -euo pipefail

# Resolve repo root relative to script location
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
INFRA_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"
REPO_DIR="$(cd "$INFRA_DIR/.." && pwd)"
ENV_DIR="$INFRA_DIR/environments/prod"
SECRETS_FILE="$INFRA_DIR/.secrets.env"

# Bootstrap .secrets.env if missing
if [[ ! -f "$SECRETS_FILE" ]]; then
  cat > "$SECRETS_FILE" <<'SECRETS'
# Terraform secrets for if-prod
# Fill in values and re-run: pnpm tf init
TF_VAR_cloudflare_api_token=""
TF_VAR_cloudflare_zone_id=""
SECRETS
  chmod 600 "$SECRETS_FILE"
  echo "Created $SECRETS_FILE — fill in your Cloudflare API token and zone ID, then retry."
  exit 1
fi

# Source secrets
set -a
source "$SECRETS_FILE"
set +a

# Validate required vars
if [[ -z "${TF_VAR_cloudflare_api_token:-}" ]]; then
  echo "ERROR: TF_VAR_cloudflare_api_token is empty in $SECRETS_FILE"
  exit 1
fi

if [[ -z "${TF_VAR_cloudflare_zone_id:-}" ]]; then
  echo "ERROR: TF_VAR_cloudflare_zone_id is empty in $SECRETS_FILE"
  exit 1
fi

# Change to environment directory
cd "$ENV_DIR"

# Get the terraform subcommand
CMD="${1:-}"

if [[ -z "$CMD" ]]; then
  echo "Usage: pnpm tf <command> [args...]"
  echo "Commands: init, plan, apply, import, destroy, output, etc."
  exit 1
fi

# Execute terraform
terraform "$@"
