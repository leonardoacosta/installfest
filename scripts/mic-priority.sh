#!/usr/bin/env bash
# mic-priority.sh - Set microphone priority order
# Uses SwitchAudioSource to select the highest-priority available mic
# Priority: Studio Display > MacBook Pro Microphone > AirPods

set -euo pipefail

# launchd login sessions get a bare PATH (/usr/bin:/bin:/usr/sbin:/sbin) with no
# Homebrew. SwitchAudioSource lives in the brew prefix, so prepend it here —
# otherwise the LaunchAgent run exits 1 with "SwitchAudioSource not found".
export PATH="/opt/homebrew/bin:/usr/local/bin:$PATH"

# Priority order (first available wins)
PREFERRED_MICS=(
    "Studio Display Microphone"
    "MacBook Pro Microphone"
    "Leo's AirPods Pro"
    "AirPods Pro"
)

# Get current input device
get_current_mic() {
    SwitchAudioSource -t input -c 2>/dev/null || echo ""
}

# Get all available input devices
get_available_mics() {
    SwitchAudioSource -t input -a 2>/dev/null || echo ""
}

# Set input device
set_mic() {
    local mic="$1"
    SwitchAudioSource -t input -s "$mic" 2>/dev/null
}

# Main logic
main() {
    if ! command -v SwitchAudioSource &>/dev/null; then
        echo "Error: SwitchAudioSource not found. Install with: brew install switchaudio-osx"
        exit 1
    fi

    local available
    available=$(get_available_mics)
    local current
    current=$(get_current_mic)

    # Find highest priority available mic
    for mic in "${PREFERRED_MICS[@]}"; do
        if echo "$available" | grep -qF "$mic"; then
            if [[ "$current" != "$mic" ]]; then
                set_mic "$mic"
                echo "Switched to: $mic"
            else
                echo "Already using: $mic"
            fi
            return 0
        fi
    done

    echo "No preferred microphone found. Available:"
    echo "$available"
    return 1
}

# Run if executed directly
if [[ "${BASH_SOURCE[0]}" == "${0}" ]]; then
    main "$@"
fi
