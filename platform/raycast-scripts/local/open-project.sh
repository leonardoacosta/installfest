#!/bin/bash

# Required parameters:
# @raycast.schemaVersion 1
# @raycast.title open project local
# @raycast.mode silent

# Optional parameters:
# @raycast.icon 🚀
# @raycast.argument1 { "type": "dropdown", "placeholder": "project", "data": [{"title": "Fireball", "value": "fb"}, {"title": "Wholesale Architecture", "value": "ws"}, {"title": "Sales CRM", "value": "sc"}, {"title": "Submission Engine", "value": "se"}, {"title": "B3 Admin", "value": "ba"}, {"title": "B3 OWA", "value": "bo"}, {"title": "Otaku Odyssey", "value": "oo"}, {"title": "Modern Visa", "value": "mv"}, {"title": "Civalent", "value": "ct"}, {"title": "Tavern Ledger", "value": "tl"}, {"title": "Tribal Cities", "value": "tc"}, {"title": "Styles by Silas", "value": "ss"}, {"title": "Central Leo", "value": "cl"}, {"title": "Central Orchestrator", "value": "co"}, {"title": "Central Wholesale", "value": "cw"}, {"title": "Cortex", "value": "cx"}, {"title": "Home Lab", "value": "hl"}, {"title": "Installfest", "value": "if"}, {"title": "Central Claude", "value": "cc"}, {"title": "Leonardo Acosta", "value": "la"}, {"title": "Las Vegas", "value": "lv"}, {"title": "Nova", "value": "nv"}, {"title": "Nexus", "value": "nx"}, {"title": "XX", "value": "xx"}] }

# Documentation:
# @raycast.description Open project locally in Zed
# @raycast.author leonardoacosta
# @raycast.authorURL https://raycast.com/leonardoacosta

if [ "$1" = "cc" ]; then
  zed ~/.claude
else
  zed ~/dev/$1/
fi
