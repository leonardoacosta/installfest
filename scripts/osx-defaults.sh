#!/bin/bash
(return 0 2>/dev/null) || set -uo pipefail  # sourced-lib guard — bare set would leak into callers

# Get the absolute path of the directory where the script is located
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

. $SCRIPT_DIR/utils.sh

apply_osx_system_defaults() {
    printf "\n"
    info "===================="
    info "OSX System Defaults"
    info "===================="
    info "Applying OSX system defaults..."

    # Enable key repeats
    defaults write -g ApplePressAndHoldEnabled -bool false

    # Enable three finger drag
    defaults write com.apple.AppleMultitouchTrackpad TrackpadThreeFingerDrag -bool false

    # # Prompt to use new external drives as Time Machine volume
    defaults write com.apple.TimeMachine DoNotOfferNewDisksForBackup -bool false

    # # Hide external hard drives on desktop
    defaults write com.apple.finder ShowExternalHardDrivesOnDesktop -bool false

    # # Hide hard drives on desktop
    defaults write com.apple.finder ShowHardDrivesOnDesktop -bool false

    # # Hide removable media hard drives on desktop
    defaults write com.apple.finder ShowRemovableMediaOnDesktop -bool false

    # # Hide mounted servers on desktop
    defaults write com.apple.finder ShowMountedServersOnDesktop -bool false

    # # Hide icons on desktop
    defaults write com.apple.finder CreateDesktop -bool false

    # # Avoid creating .DS_Store files on network volumes
    defaults write com.apple.desktopservices DSDontWriteNetworkStores -bool true

    # # Show path bar
    defaults write com.apple.finder ShowPathbar -bool true

    # # Show hidden files inside the finder
    defaults write com.apple.finder "AppleShowAllFiles" -bool true

    # # Show Status Bar
    defaults write com.apple.finder "ShowStatusBar" -bool true

    # # Do not show warning when changing the file extension
    defaults write com.apple.finder FXEnableExtensionChangeWarning -bool false

    # # Save screenshots in PNG format (other options: BMP, GIF, JPG, PDF, TIFF)
    defaults write com.apple.screencapture type -string "png"

    # # Set weekly software update checks
    defaults write com.apple.SoftwareUpdate ScheduleFrequency -int 7

    # # Spaces span all displays
    defaults write com.apple.spaces "spans-displays" -bool false

    # # Do not rearrange spaces automatically
    defaults write com.apple.dock "mru-spaces" -bool false

    # Set Dock autohide
    defaults write com.apple.dock autohide -bool true
    defaults write com.apple.dock largesize -float 68
    defaults write com.apple.dock "minimize-to-application" -bool true
    defaults write com.apple.dock tilesize -float 32

}

if [ "$(basename "$0")" = "$(basename "${BASH_SOURCE[0]}")" ]; then
    apply_osx_system_defaults
fi