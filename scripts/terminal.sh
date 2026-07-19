#!/bin/bash
(return 0 2>/dev/null) || set -uo pipefail  # sourced-lib guard — bare set would leak into callers

# Get the absolute path of the directory where the script is located
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

. $SCRIPT_DIR/utils.sh


terminal() {
printf "\n"
    info "===================="
    info "Terminal"
    info "===================="

    info "Adding .hushlogin file to suppress 'last login' message in terminal..."
    touch ~/.hushlogin
}

if [ "$(basename "$0")" = "$(basename "${BASH_SOURCE[0]}")" ]; then
    terminal
fi