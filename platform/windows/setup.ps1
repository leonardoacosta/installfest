#Requires -RunAsAdministrator
<#
.SYNOPSIS
    Windows dev environment setup - SSH, WSL2 Arch, Mac keyboard (Synergy), dev tools.

.DESCRIPTION
    Configures a Windows workstation for development with:
    - OpenSSH Server with Ed25519 key authentication
    - WSL2 with Arch Linux (mirrors homelab/mac dotfiles)
    - WezTerm terminal with Mac keyboard bindings
    - AutoHotKey for Synergy Mac keyboard remapping
    - Dev tools via winget (Cursor, VS Code, Visual Studio)
    - Docker Engine inside WSL2 (no Docker Desktop)

.NOTES
    Run as Administrator: Right-click PowerShell -> Run as Administrator
    Or: Start-Process powershell -Verb RunAs -ArgumentList "-File $PSCommandPath"
#>

Set-StrictMode -Version Latest
$ErrorActionPreference = "Stop"

# ============================================================================
# Configuration
# ============================================================================

$Config = @{
    # Dotfiles repo (cloned inside WSL2)
    DotfilesRepo   = "git@github.com:leonardoacosta/dotfiles.git"
    DotfilesPath   = "~/dev/personal/installfest"

    # WSL2 username (should match your other machines)
    WslUser        = "nyaptor"

    # SSH
    SshPort        = 22

    # Tailscale interface name (for firewall rules)
    TailscaleIface = "Tailscale"

    # Windows apps to install via winget
    WingetApps     = @(
        # --- Terminal & Shell ---
        "wez.wezterm"
        "Microsoft.PowerToys"
        "gerardog.gsudo"              # sudo for Windows

        # --- Editors & IDEs ---
        "Anysphere.Cursor"
        "Microsoft.VisualStudioCode"
        "Microsoft.VisualStudio.2022.Professional"

        # --- AI ---
        "Anthropic.Claude"            # Claude Desktop
        "Anthropic.ClaudeCode"        # Claude Code CLI

        # --- Dev Runtimes & Tools ---
        "CoreyButler.NVMforWindows"   # nvm (manages Node versions)
        "pnpm.pnpm"
        "Microsoft.DotNet.SDK.9"      # .NET SDK
        "Microsoft.AzureCLI"

        # --- Git ---
        "Git.Git"
        "GitHub.cli"
        "Axosoft.GitKraken"

        # --- API Testing ---
        "Bruno.Bruno"

        # --- Networking ---
        "Tailscale.Tailscale"
        "Symless.Synergy"

        # --- Input & Productivity ---
        "AutoHotkey.AutoHotkey"
        "Raycast.Raycast"

        # --- Knowledge & Notes ---
        "Notion.Notion"
        "Obsidian.Obsidian"

        # --- Browser ---
        "Google.Chrome"

        # --- Fonts ---
        "JetBrains.Mono"
    )

    # Microsoft Store apps (no winget ID available)
    MsStoreApps    = @(
        "9n1b9jwb3m35"                # Wispr Flow (voice dictation)
    )
}

# ============================================================================
# Helpers
# ============================================================================

function Write-Step {
    param([string]$Message)
    Write-Host "`n==> $Message" -ForegroundColor Cyan
}

function Write-Ok {
    param([string]$Message)
    Write-Host "==> $Message" -ForegroundColor Green
}

function Write-Warn {
    param([string]$Message)
    Write-Host "==> $Message" -ForegroundColor Yellow
}

function Write-Err {
    param([string]$Message)
    Write-Host "==> $Message" -ForegroundColor Red
}

function Test-CommandExists {
    param([string]$Command)
    $null -ne (Get-Command $Command -ErrorAction SilentlyContinue)
}

function Prompt-YesNo {
    param([string]$Question)
    $response = Read-Host "$Question [y/n]"
    return $response -match '^[Yy]'
}

# ============================================================================
# 1. OpenSSH Server
# ============================================================================

function Install-OpenSSHServer {
    Write-Step "Configuring OpenSSH Server..."

    # Install built-in OpenSSH Server if not present (bootstrap only — replaced by GitHub release below)
    $sshCapability = Get-WindowsCapability -Online | Where-Object Name -like "OpenSSH.Server*"
    if ($sshCapability.State -ne "Installed") {
        Write-Step "Installing OpenSSH Server (built-in)..."
        Add-WindowsCapability -Online -Name "OpenSSH.Server~~~~0.0.1.0"
    } else {
        Write-Ok "OpenSSH Server capability present"
    }

    # Upgrade to Win32-OpenSSH from GitHub (the built-in is 8.x with SOCKS/ControlMaster bugs)
    # Target: OpenSSH 10.0+ from https://github.com/PowerShell/Win32-OpenSSH
    $currentVersion = ""
    $sshdPath = "$env:ProgramFiles\OpenSSH\sshd.exe"
    $builtinSshdPath = "$env:SystemRoot\System32\OpenSSH\sshd.exe"

    # Check current version (prefer Program Files install over built-in)
    if (Test-Path $sshdPath) {
        $currentVersion = (& $sshdPath -V 2>&1) -replace '.*OpenSSH_for_Windows_(\S+).*', '$1'
    } elseif (Test-Path $builtinSshdPath) {
        $currentVersion = (& $builtinSshdPath -V 2>&1) -replace '.*OpenSSH_for_Windows_(\S+).*', '$1'
    }
    Write-Step "Current OpenSSH version: $currentVersion"

    $targetVersion = "10.0.0.0"
    $needsUpgrade = $true
    if ($currentVersion -match "^(\d+)\.") {
        $majorVersion = [int]$Matches[1]
        if ($majorVersion -ge 10) {
            $needsUpgrade = $false
            Write-Ok "OpenSSH $currentVersion is current (>= 10.x)"
        }
    }

    if ($needsUpgrade) {
        Write-Step "Upgrading OpenSSH to latest from GitHub..."

        # Get latest release URL
        $releaseApi = "https://api.github.com/repos/PowerShell/Win32-OpenSSH/releases/latest"
        $release = Invoke-RestMethod -Uri $releaseApi -UseBasicParsing
        $asset = $release.assets | Where-Object { $_.name -like "OpenSSH-Win64*.msi" } | Select-Object -First 1

        if (-not $asset) {
            # Fallback to zip if no MSI
            $asset = $release.assets | Where-Object { $_.name -like "OpenSSH-Win64.zip" } | Select-Object -First 1
        }

        if ($asset) {
            $downloadPath = "$env:TEMP\$($asset.name)"
            Write-Step "Downloading $($asset.name) ($($release.tag_name))..."
            Invoke-WebRequest -Uri $asset.browser_download_url -OutFile $downloadPath -UseBasicParsing

            # Stop sshd before upgrading
            Stop-Service sshd -ErrorAction SilentlyContinue

            if ($asset.name -like "*.msi") {
                Write-Step "Installing via MSI..."
                Start-Process msiexec.exe -ArgumentList "/i `"$downloadPath`" /qn" -Wait -NoNewWindow
            } else {
                # ZIP install — extract to Program Files\OpenSSH
                Write-Step "Installing from ZIP..."
                $installDir = "$env:ProgramFiles\OpenSSH"
                if (Test-Path $installDir) {
                    # Backup existing
                    Rename-Item $installDir "$installDir.bak.$(Get-Date -Format yyyyMMdd)" -ErrorAction SilentlyContinue
                }
                Expand-Archive -Path $downloadPath -DestinationPath "$env:ProgramFiles" -Force
                # The ZIP extracts to OpenSSH-Win64, rename to OpenSSH
                if (Test-Path "$env:ProgramFiles\OpenSSH-Win64") {
                    Rename-Item "$env:ProgramFiles\OpenSSH-Win64" "OpenSSH"
                }
                # Install sshd service from new binary
                & "$installDir\install-sshd.ps1"
            }

            Remove-Item $downloadPath -ErrorAction SilentlyContinue

            # Verify upgrade
            if (Test-Path "$env:ProgramFiles\OpenSSH\sshd.exe") {
                $newVersion = (& "$env:ProgramFiles\OpenSSH\sshd.exe" -V 2>&1)
                Write-Ok "Upgraded to: $newVersion"
            }
        } else {
            Write-Warn "Could not find OpenSSH Win64 release asset — skipping upgrade"
        }
    }

    # Generate default config if it doesn't exist
    $sshDir = "$env:ProgramData\ssh"
    $sshdConfig = "$sshDir\sshd_config"

    if (-not (Test-Path $sshDir)) {
        New-Item -ItemType Directory -Path $sshDir -Force | Out-Null
        Write-Ok "Created $sshDir"
    }

    if (-not (Test-Path $sshdConfig)) {
        Write-Step "Generating default sshd_config..."

        # Method 1: Try starting the service (generates config on first run)
        Set-Service -Name sshd -StartupType Manual -ErrorAction SilentlyContinue
        Start-Service sshd -ErrorAction SilentlyContinue
        Start-Sleep -Seconds 3
        Stop-Service sshd -ErrorAction SilentlyContinue

        # Method 2: If service didn't generate it, write a default config
        if (-not (Test-Path $sshdConfig)) {
            Write-Step "Service didn't generate config, writing default..."
            @"
# OpenSSH Server Configuration (generated by setup.ps1)
Port 22
PubkeyAuthentication yes
AuthorizedKeysFile .ssh/authorized_keys
PasswordAuthentication no
PermitEmptyPasswords no
Subsystem sftp sftp-server.exe
"@ | Set-Content -Path $sshdConfig
            Write-Ok "Default sshd_config written"
        }
    }

    # Backup original config
    if (-not (Test-Path "$sshdConfig.bak")) {
        Copy-Item $sshdConfig "$sshdConfig.bak"
        Write-Ok "Backed up original sshd_config"
    }

    # Enable key-based auth, disable password auth
    $configContent = Get-Content $sshdConfig -Raw
    $updates = @{
        '#PubkeyAuthentication yes'          = 'PubkeyAuthentication yes'
        '#AuthorizedKeysFile'                = 'AuthorizedKeysFile'
        '#PasswordAuthentication yes'        = 'PasswordAuthentication no'
        'PasswordAuthentication yes'         = 'PasswordAuthentication no'
    }

    foreach ($old in $updates.Keys) {
        if ($configContent -match [regex]::Escape($old)) {
            $configContent = $configContent -replace [regex]::Escape($old), $updates[$old]
        }
    }

    # Disable the admin authorized_keys override (important for non-admin users too)
    # Comment out the Match Group administrators block at the end
    $configContent = $configContent -replace '(?m)^(Match Group administrators)', '# $1'
    $configContent = $configContent -replace '(?m)^(\s+AuthorizedKeysFile __PROGRAMDATA__)', '# $1'

    Set-Content -Path $sshdConfig -Value $configContent
    Write-Ok "sshd_config updated (pubkey auth enabled, password auth disabled)"

    # Create .ssh directory and authorized_keys for current user
    $sshDir = "$env:USERPROFILE\.ssh"
    if (-not (Test-Path $sshDir)) {
        New-Item -ItemType Directory -Path $sshDir -Force | Out-Null
    }

    $authKeys = "$sshDir\authorized_keys"
    if (-not (Test-Path $authKeys)) {
        New-Item -ItemType File -Path $authKeys -Force | Out-Null
        Write-Warn "Created empty authorized_keys - add your Ed25519 public key:"
        Write-Warn "  $authKeys"
    }

    # Set correct permissions on authorized_keys (Windows ACL)
    $acl = Get-Acl $authKeys
    $acl.SetAccessRuleProtection($true, $false) # Disable inheritance
    $currentUser = [System.Security.Principal.WindowsIdentity]::GetCurrent().Name
    $adminRule = New-Object System.Security.AccessControl.FileSystemAccessRule(
        "BUILTIN\Administrators", "FullControl", "Allow"
    )
    $systemRule = New-Object System.Security.AccessControl.FileSystemAccessRule(
        "NT AUTHORITY\SYSTEM", "FullControl", "Allow"
    )
    $userRule = New-Object System.Security.AccessControl.FileSystemAccessRule(
        $currentUser, "FullControl", "Allow"
    )
    $acl.AddAccessRule($adminRule)
    $acl.AddAccessRule($systemRule)
    $acl.AddAccessRule($userRule)
    Set-Acl $authKeys $acl
    Write-Ok "authorized_keys permissions set"

    # Set default shell to PowerShell 7 (if installed) or PowerShell 5
    $pwsh7 = "C:\Program Files\PowerShell\7\pwsh.exe"
    $defaultShell = if (Test-Path $pwsh7) { $pwsh7 } else { "C:\Windows\System32\WindowsPowerShell\v1.0\powershell.exe" }
    New-ItemProperty -Path "HKLM:\SOFTWARE\OpenSSH" -Name DefaultShell -Value $defaultShell -PropertyType String -Force | Out-Null
    Write-Ok "Default SSH shell: $defaultShell"

    # Start and enable sshd service
    Set-Service -Name sshd -StartupType Automatic
    Start-Service sshd
    Write-Ok "sshd service started and enabled"

    # ── SSH Mesh: Deploy keys and configs for all users ──

    # SSH key must be transferred securely - do not embed in scripts
    # The public key can be deployed here (it's public)
    $publicKey = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIFqT0bMXcrQGgWvYoLg66dCCvhgAPx1rmrJmzGpMeFVR"

    # SSH Config for outbound connections to other machines
    $sshMeshConfig = @"
# SSH Mesh Config (managed by setup.ps1)
Host homelab
    HostName 100.73.182.4
    User nyaptor
    IdentityFile ~/.ssh/id_ed25519
    ServerAliveInterval 60
    ServerAliveCountMax 3
    ConnectTimeout 10

Host mac
    HostName 100.91.88.16
    User leonardoacosta
    IdentityFile ~/.ssh/id_ed25519
    ServerAliveInterval 60
    ServerAliveCountMax 3
    ConnectTimeout 10
"@

    # Paths - Windows has two potential users
    $sshUsers = @(
        @{
            Name      = "leo (SSH login user)"
            SshDir    = "C:\Users\leo\.ssh"
            AuthKeys  = "C:\Users\leo\.ssh\authorized_keys"
        },
        @{
            Name       = "LeonardoAcosta (AzureAD user)"
            SshDir     = "C:\Users\LeonardoAcosta\.ssh"
            AuthKeys   = "C:\Users\LeonardoAcosta\.ssh\authorized_keys"
            Config     = "C:\Users\LeonardoAcosta\.ssh\config"
            PublicKey  = "C:\Users\LeonardoAcosta\.ssh\id_ed25519.pub"
        }
    )

    foreach ($sshUser in $sshUsers) {
        Write-Step "SSH Mesh: Processing $($sshUser.Name)..."

        # Create .ssh directory
        if (-not (Test-Path $sshUser.SshDir)) {
            New-Item -ItemType Directory -Force -Path $sshUser.SshDir | Out-Null
            Write-Ok "Created: $($sshUser.SshDir)"
        }

        # Handle existing authorized_keys
        if (Test-Path $sshUser.AuthKeys) {
            takeown /f $sshUser.AuthKeys 2>$null | Out-Null
            Remove-Item $sshUser.AuthKeys -Force -ErrorAction SilentlyContinue
        }

        # Write authorized_keys with the mesh public key
        Set-Content -Path $sshUser.AuthKeys -Value $publicKey -Force
        Write-Ok "authorized_keys deployed: $($sshUser.AuthKeys)"

        # For AzureAD user, also set up outbound SSH config and public key
        if ($sshUser.Config) {
            Set-Content -Path $sshUser.Config -Value $sshMeshConfig -Force
            Write-Ok "SSH config deployed: $($sshUser.Config)"
        }
        if ($sshUser.PublicKey) {
            Set-Content -Path $sshUser.PublicKey -Value $publicKey -Force
            Write-Ok "Public key deployed: $($sshUser.PublicKey)"
        }
    }

    # Admin authorized_keys (required for admin users on Windows OpenSSH)
    Write-Step "Setting up administrator authorized_keys..."
    $sshProgramData = "C:\ProgramData\ssh"
    if (-not (Test-Path $sshProgramData)) {
        New-Item -ItemType Directory -Force -Path $sshProgramData | Out-Null
    }
    $adminAuthKeys = "$sshProgramData\administrators_authorized_keys"
    if (Test-Path $adminAuthKeys) {
        takeown /f $adminAuthKeys 2>$null | Out-Null
        Remove-Item $adminAuthKeys -Force -ErrorAction SilentlyContinue
    }
    Set-Content -Path $adminAuthKeys -Value $publicKey -Force
    icacls $adminAuthKeys /inheritance:r /grant "SYSTEM:F" /grant "Administrators:F" | Out-Null
    Write-Ok "Admin authorized_keys deployed with correct permissions"

    # ── End SSH Mesh ──

    # Firewall rule for Tailscale
    $ruleName = "OpenSSH-Server-Tailscale"
    $existingRule = Get-NetFirewallRule -Name $ruleName -ErrorAction SilentlyContinue
    if (-not $existingRule) {
        New-NetFirewallRule -Name $ruleName `
            -DisplayName "OpenSSH Server (Tailscale)" `
            -Direction Inbound `
            -Protocol TCP `
            -LocalPort $Config.SshPort `
            -InterfaceAlias $Config.TailscaleIface `
            -Action Allow
        Write-Ok "Firewall rule created for SSH on Tailscale interface"
    } else {
        Write-Ok "Firewall rule already exists"
    }
}

# ============================================================================
# 2. WSL2 + Arch Linux
# ============================================================================

function Install-WSL2Arch {
    Write-Step "Setting up WSL2 with Arch Linux..."

    # Enable WSL feature
    $wslFeature = Get-WindowsOptionalFeature -Online -FeatureName Microsoft-Windows-Subsystem-Linux
    if ($wslFeature.State -ne "Enabled") {
        Write-Step "Enabling WSL..."
        Enable-WindowsOptionalFeature -Online -FeatureName Microsoft-Windows-Subsystem-Linux -NoRestart
        Write-Warn "WSL enabled - reboot may be required"
    }

    # Enable Virtual Machine Platform
    $vmFeature = Get-WindowsOptionalFeature -Online -FeatureName VirtualMachinePlatform
    if ($vmFeature.State -ne "Enabled") {
        Write-Step "Enabling Virtual Machine Platform..."
        Enable-WindowsOptionalFeature -Online -FeatureName VirtualMachinePlatform -NoRestart
        Write-Warn "VM Platform enabled - reboot may be required"
    }

    # Set WSL 2 as default
    wsl --set-default-version 2 2>$null

    Write-Ok "WSL2 features enabled"

    # Install Arch Linux via scoop (ArchWSL)
    if (-not (Test-CommandExists "scoop")) {
        Write-Step "Installing Scoop (needed for ArchWSL)..."
        [System.Net.ServicePointManager]::SecurityProtocol = [System.Net.SecurityProtocolType]::Tls12

        # Try Set-ExecutionPolicy first, fall back if Group Policy blocks it
        try {
            Set-ExecutionPolicy RemoteSigned -Scope CurrentUser -Force -ErrorAction Stop
        } catch {
            Write-Warn "Set-ExecutionPolicy blocked (likely Group Policy). Using bypass method."
        }

        # Download and run installer via powershell -ep bypass (works even with Group Policy)
        $scoopInstaller = "$env:TEMP\scoop-install.ps1"
        Invoke-RestMethod -Uri https://get.scoop.sh -OutFile $scoopInstaller
        & powershell.exe -ExecutionPolicy Bypass -File $scoopInstaller
        Remove-Item $scoopInstaller -ErrorAction SilentlyContinue

        # Refresh PATH so scoop is available in this session
        $env:PATH = "$env:USERPROFILE\scoop\shims;$env:PATH"
    }

    # Add extras bucket for ArchWSL
    scoop bucket add extras 2>$null

    if (-not (Test-CommandExists "Arch.exe")) {
        Write-Step "Installing ArchWSL..."
        scoop install archwsl
        Write-Ok "ArchWSL installed"
    } else {
        Write-Ok "ArchWSL already installed"
    }

    Write-Step "Post-install: Arch Linux WSL2 bootstrap"
    Write-Host @"

  ArchWSL is installed. Run these commands to bootstrap:

  1. Launch Arch:
     > Arch.exe

  2. Inside Arch, initialize pacman and create your user:
     # pacman-key --init
     # pacman-key --populate archlinux
     # pacman -Syu --noconfirm
     # pacman -S --noconfirm sudo zsh git base-devel
     # useradd -m -G wheel -s /bin/zsh $($Config.WslUser)
     # passwd $($Config.WslUser)
     # echo '%wheel ALL=(ALL:ALL) ALL' > /etc/sudoers.d/wheel
     # exit

  3. Set default user (from PowerShell):
     > Arch.exe config --default-user $($Config.WslUser)

  4. Re-enter as your user and clone dotfiles:
     > Arch.exe
     $ mkdir -p ~/dev && cd ~/dev
     $ git clone $($Config.DotfilesRepo) if
     $ cd if && ./install.sh

  5. Install yay (AUR helper):
     $ cd /tmp
     $ git clone https://aur.archlinux.org/yay.git && cd yay
     $ makepkg -si --noconfirm

  6. Install Docker Engine (no Docker Desktop):
     $ sudo pacman -S docker docker-buildx docker-compose
     $ sudo systemctl enable --now docker  # (or use dockerd manually)
     $ sudo usermod -aG docker $($Config.WslUser)

"@
}

# ============================================================================
# 3. Windows Apps via winget
# ============================================================================

function Install-WingetApps {
    Write-Step "Installing Windows apps via winget..."

    foreach ($app in $Config.WingetApps) {
        $installed = winget list --id $app 2>$null
        if ($LASTEXITCODE -eq 0 -and $installed -match $app) {
            Write-Ok "$app already installed"
        } else {
            Write-Step "Installing $app..."
            winget install --id $app --accept-source-agreements --accept-package-agreements --silent
            if ($LASTEXITCODE -eq 0) {
                Write-Ok "$app installed"
            } else {
                Write-Warn "Failed to install $app (may need manual install)"
            }
        }
    }

    # Microsoft Store apps (installed via winget --source msstore)
    if ($Config.MsStoreApps) {
        Write-Step "Installing Microsoft Store apps..."
        foreach ($storeId in $Config.MsStoreApps) {
            Write-Step "Installing Store app $storeId..."
            $output = winget install --id $storeId --source msstore --accept-source-agreements --accept-package-agreements --silent 2>&1
            if ($LASTEXITCODE -eq 0 -or $output -match "already installed|No available upgrade") {
                Write-Ok "Store app $storeId installed (or already up to date)"
            } else {
                Write-Warn "Failed to install Store app $storeId (open Microsoft Store manually)"
            }
        }
    }
}

# ============================================================================
# 4. WezTerm Configuration (Windows-adapted)
# ============================================================================

function Install-WezTermConfig {
    Write-Step "Configuring WezTerm for Windows..."

    $weztermDir = "$env:USERPROFILE\.config\wezterm"
    if (-not (Test-Path $weztermDir)) {
        New-Item -ItemType Directory -Path $weztermDir -Force | Out-Null
    }

    # Copy the Windows-adapted WezTerm config
    $scriptDir = Split-Path -Parent $MyInvocation.ScriptName
    $sourceConfig = Join-Path $scriptDir "wezterm-windows.lua"

    if (Test-Path $sourceConfig) {
        Copy-Item $sourceConfig "$weztermDir\wezterm.lua" -Force
        Write-Ok "WezTerm config installed to $weztermDir\wezterm.lua"
    } else {
        Write-Warn "wezterm-windows.lua not found in script directory"
        Write-Warn "Expected at: $sourceConfig"
    }
}

# ============================================================================
# 5. AutoHotKey Script (Mac Keyboard via Synergy)
# ============================================================================

function Install-AHKScript {
    Write-Step "Setting up AutoHotKey Mac keyboard remapping..."

    $ahkDir = "$env:APPDATA\AutoHotKey"
    if (-not (Test-Path $ahkDir)) {
        New-Item -ItemType Directory -Path $ahkDir -Force | Out-Null
    }

    # Copy AHK script
    $scriptDir = Split-Path -Parent $MyInvocation.ScriptName
    $sourceAhk = Join-Path $scriptDir "mac-keyboard.ahk"

    if (Test-Path $sourceAhk) {
        Copy-Item $sourceAhk "$ahkDir\mac-keyboard.ahk" -Force
        Write-Ok "AHK script installed to $ahkDir\mac-keyboard.ahk"
    } else {
        Write-Warn "mac-keyboard.ahk not found in script directory"
    }

    # Create startup shortcut so AHK runs on login
    $startupDir = "$env:APPDATA\Microsoft\Windows\Start Menu\Programs\Startup"
    $shortcutPath = "$startupDir\MacKeyboard.lnk"

    if (-not (Test-Path $shortcutPath)) {
        $shell = New-Object -ComObject WScript.Shell
        $shortcut = $shell.CreateShortcut($shortcutPath)
        $shortcut.TargetPath = "$ahkDir\mac-keyboard.ahk"
        $shortcut.Description = "Mac keyboard remapping for Synergy"
        $shortcut.Save()
        Write-Ok "AHK startup shortcut created"
    } else {
        Write-Ok "AHK startup shortcut already exists"
    }
}

# ============================================================================
# 6. Git Configuration
# ============================================================================

function Install-GitConfig {
    Write-Step "Configuring Git..."

    if (-not (Test-CommandExists "git")) {
        Write-Warn "Git not installed yet, skipping config"
        return
    }

    git config --global user.name "leonardoacosta"
    git config --global user.email "leo@leonardoacosta.dev"
    git config --global init.defaultBranch "main"
    git config --global pull.rebase true
    git config --global core.autocrlf input
    git config --global core.eol lf

    # SSH signing (Ed25519)
    $sshKey = "$env:USERPROFILE\.ssh\id_ed25519.pub"
    if (Test-Path $sshKey) {
        git config --global gpg.format ssh
        git config --global user.signingKey $sshKey
        git config --global commit.gpgSign true
        Write-Ok "Git SSH signing configured with Ed25519"
    } else {
        Write-Warn "No Ed25519 key found at $sshKey"
        Write-Warn "Generate one: ssh-keygen -t ed25519"
    }

    # Git aliases
    git config --global alias.co "checkout"
    git config --global alias.br "branch"
    git config --global alias.ci "commit"
    git config --global alias.st "status"

    # Credential helper (Git Credential Manager)
    git config --global credential.helper manager

    Write-Ok "Git configured"
}

# ============================================================================
# 7. PowerShell Profile (minimal - WSL2 is primary shell)
# ============================================================================

function Install-PSProfile {
    Write-Step "Setting up PowerShell profile..."

    $profileDir = Split-Path $PROFILE
    if (-not (Test-Path $profileDir)) {
        New-Item -ItemType Directory -Path $profileDir -Force | Out-Null
    }

    $profileContent = @'
# PowerShell profile - minimal (WSL2 Arch is primary dev shell)

# Quick access to WSL
function wsl-arch { wsl -d Arch }
Set-Alias -Name arch -Value wsl-arch

# gsudo alias
if (Get-Command gsudo -ErrorAction SilentlyContinue) {
    Set-Alias -Name sudo -Value gsudo
}

# Starship prompt (if installed on Windows side)
if (Get-Command starship -ErrorAction SilentlyContinue) {
    Invoke-Expression (&starship init powershell)
}

# Quick navigation
function dev { wsl -d Arch --cd "~/dev" }
Set-Alias -Name ll -Value Get-ChildItem

# SSH into WSL from anywhere
function ssh-wsl { ssh localhost }
'@

    Set-Content -Path $PROFILE -Value $profileContent
    Write-Ok "PowerShell profile created at $PROFILE"
}

# ============================================================================
# 8. Nerd Fonts
# ============================================================================

function Install-NerdFonts {
    Write-Step "Installing Nerd Fonts..."

    if (-not (Test-CommandExists "scoop")) {
        Write-Warn "Scoop not installed, skipping nerd fonts"
        return
    }

    scoop bucket add nerd-fonts 2>$null

    $fonts = @(
        "GeistMono-NF"
        "JetBrainsMono-NF"
        "CascadiaMono-NF"
    )

    foreach ($font in $fonts) {
        $installed = scoop list $font 2>$null
        if ($LASTEXITCODE -ne 0) {
            Write-Step "Installing $font..."
            sudo scoop install -g $font
        } else {
            Write-Ok "$font already installed"
        }
    }
}

# ============================================================================
# 9. WSL2 Configuration
# ============================================================================

function Install-WSLConfig {
    Write-Step "Configuring WSL2 settings..."

    $wslConfig = "$env:USERPROFILE\.wslconfig"

    $wslContent = @"
[wsl2]
memory=8GB
processors=4
swap=4GB
localhostForwarding=true

[experimental]
autoMemoryReclaim=gradual
sparseVhd=true
"@

    Set-Content -Path $wslConfig -Value $wslContent
    Write-Ok ".wslconfig created (8GB RAM, 4 cores for WSL2)"
    Write-Warn "Adjust memory/processors in $wslConfig based on your hardware"
}

# ============================================================================
# Main
# ============================================================================

Write-Host @"

========================================
  Windows Dev Environment Setup
  $(Get-Date -Format "yyyy-MM-dd HH:mm")
========================================

This script will configure:
  1. OpenSSH Server (Ed25519 auth, Tailscale firewall)
  2. WSL2 + Arch Linux
  3. Windows apps (WezTerm, Cursor, VS Code, VS Studio, etc.)
  4. WezTerm config (Mac keyboard bindings)
  5. AutoHotKey (Synergy Mac keyboard remapping)
  6. Git (SSH signing, aliases)
  7. PowerShell profile
  8. Nerd Fonts
  9. WSL2 resource limits

"@

# Run each section with confirmation
$sections = @(
    @{ Name = "OpenSSH Server";         Fn = { Install-OpenSSHServer } }
    @{ Name = "WSL2 + Arch Linux";      Fn = { Install-WSL2Arch } }
    @{ Name = "Windows apps (winget)";  Fn = { Install-WingetApps } }
    @{ Name = "WezTerm config";         Fn = { Install-WezTermConfig } }
    @{ Name = "AutoHotKey (Mac keys)";  Fn = { Install-AHKScript } }
    @{ Name = "Git configuration";      Fn = { Install-GitConfig } }
    @{ Name = "PowerShell profile";     Fn = { Install-PSProfile } }
    @{ Name = "Nerd Fonts";             Fn = { Install-NerdFonts } }
    @{ Name = "WSL2 resource config";   Fn = { Install-WSLConfig } }
)

$runAll = Prompt-YesNo "Run all sections automatically?"

foreach ($section in $sections) {
    if ($runAll -or (Prompt-YesNo "Setup $($section.Name)?")) {
        try {
            & $section.Fn
        } catch {
            Write-Err "Failed: $($section.Name) - $($_.Exception.Message)"
            if (-not (Prompt-YesNo "Continue with remaining sections?")) {
                exit 1
            }
        }
    }
}

Write-Host @"

========================================
  Setup Complete!
========================================

Next steps:
  1. Reboot (if WSL/VM features were just enabled)
  2. Run Arch.exe and follow the bootstrap instructions above
  3. Add your Ed25519 public key to ~\.ssh\authorized_keys
  4. Start AHK script: $env:APPDATA\AutoHotKey\mac-keyboard.ahk
  5. Configure Synergy to connect to this PC
  6. Test SSH from Mac: ssh <tailscale-ip>

"@
