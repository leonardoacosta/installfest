#!/bin/zsh
# mux-remote.sh — Remote-invokable wrapper for cmux-workspaces
# Called via Apple Shortcuts, NFC, or SSH
# Uses zsh for Shortcuts compatibility (/bin/bash on macOS is 3.2)
# Project data: ~/dev/personal/installfest/home/projects.toml
#
# Usage:
#   ~/dev/personal/installfest/scripts/mux-remote.sh              # Interactive picker
#   ~/dev/personal/installfest/scripts/mux-remote.sh brown         # Launch the b-and-b org root
#   ~/dev/personal/installfest/scripts/mux-remote.sh priceless     # Launch the priceless org root
#   ~/dev/personal/installfest/scripts/mux-remote.sh cc            # Launch the cc org root
#   ~/dev/personal/installfest/scripts/mux-remote.sh personal      # Launch the personal org root
#   ~/dev/personal/installfest/scripts/mux-remote.sh brown priceless   # Launch two org roots
#
# mux never bulk-launches — every code opens exactly one workspace. There is
# no "launch everything" equivalent; pick specific codes or use the
# "Pick Projects..." picker for individual project codes.

set -uo pipefail

if [[ "${1:-}" == "--help" || "${1:-}" == "-h" ]]; then
	sed -n '2,17p' "$0" | sed 's/^# \{0,1\}//'
	exit 0
fi

export PATH="/opt/homebrew/bin:/usr/local/bin:$PATH"
SCRIPT=~/dev/personal/installfest/scripts/cmux-workspaces.sh
source ~/dev/personal/installfest/scripts/lib/registry.sh
TOML_FILE="$(registry_path)"

# Verify cmux is running
if ! cmux ping >/dev/null 2>&1; then
  osascript -e 'display notification "cmux is not running" with title "Mux"' 2>/dev/null
  echo "ERROR: cmux not running" >&2
  exit 1
fi

# Generate the AppleScript list items for the "Pick Projects" dialog from projects.toml
generate_picker_applescript() {
  PY="$(registry_python)" || exit 1
  "$PY" << 'PYEOF'
import tomllib, os

toml_file = os.path.expanduser(os.environ["TOML_FILE"])
with open(toml_file, "rb") as f:
    data = tomllib.load(f)

projects = data["projects"]

cat_meta = {
    "b-and-b":   {"emoji": "\U0001f7e1", "label": "B&B"},
    "priceless": {"emoji": "\U0001f7e2", "label": "Priceless"},
    "cc":        {"emoji": "\U0001f527", "label": "CC"},
    "personal":  {"emoji": "\U0001f535", "label": "Personal"},
}
cat_order = ["b-and-b", "priceless", "cc", "personal"]

# Group projects by category, preserving TOML order
groups = {c: [] for c in cat_order}
for p in projects:
    cat = p["category"]
    if cat in groups:
        groups[cat].append(p)

# Build AppleScript list items
items = []
for cat in cat_order:
    projs = groups[cat]
    if not projs:
        continue
    meta = cat_meta[cat]
    # Category header (non-selectable — filtered out by "starts with space" check)
    items.append(f'"{meta["emoji"]} {meta["label"]} ' + "\u2500" * 10 + '"')
    for i, p in enumerate(projs):
        code = p["code"]
        name = p["name"]
        connector = "\u2514" if i == len(projs) - 1 else "\u251c"
        # 2-char code, left-padded with spaces to align with header indent
        items.append(f'"  {connector} {code}  {name}"')

# Emit the full osascript command
items_str = ", \u00ac\n            ".join(items)
print(f'''set opts to {{ \u00ac
            {items_str} \u00ac
          }}
          set picked to choose from list opts with title "Pick Projects" with prompt "Select workspaces to open:" with multiple selections allowed
          if picked is false then return "cancel"
          set output to ""
          repeat with anItem in picked
            -- skip group headers
            if anItem does not start with "  " then
            else
              set code to text 5 thru 6 of anItem
              set output to output & code & linefeed
            end if
          end repeat
          return output''')
PYEOF
}

# No args → show interactive picker
if [[ $# -eq 0 ]]; then
  choice=$(osascript <<'EOF'
    set options to {"🟡 B&B", "🟢 Priceless", "🔧 CC", "🔵 Personal", "🔧 Pick Projects..."}
    set picked to choose from list options with title "Mux Workspaces" with prompt "What do you want to open?" with multiple selections allowed
    if picked is false then return "cancel"
    set output to ""
    repeat with anItem in picked
      set output to output & anItem & linefeed
    end repeat
    return output
EOF
  )

  [[ "$choice" == "cancel" ]] && exit 0

  args=()
  while IFS= read -r line; do
    case "$line" in
      *"B&B"*)        args+=(brown) ;;
      *Priceless*)    args+=(priceless) ;;
      *CC*)           args+=(cc) ;;
      *Personal*)     args+=(personal) ;;
      *Pick*)
        picker_script=$(TOML_FILE="$TOML_FILE" generate_picker_applescript)
        projects=$(osascript -e "$picker_script")
        [[ "$projects" == "cancel" ]] && exit 0
        while IFS= read -r proj; do
          [[ -n "$proj" ]] && args+=("$proj")
        done <<< "$projects"
        ;;
    esac
  done <<< "$choice"

  if [[ ${#args[@]} -eq 0 ]]; then
    exit 0
  fi

  exec "$SCRIPT" "${args[@]}"
fi

# Args provided → pass through directly
exec "$SCRIPT" "$@"
