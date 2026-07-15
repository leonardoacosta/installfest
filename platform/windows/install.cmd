@echo off
:: =============================================================================
:: Windows Dev Environment Installer (Bootstrap)
::
:: Double-click this file or run from CMD to kick off setup.
:: Handles execution policy, admin elevation, and launches setup.ps1.
:: =============================================================================

title Windows Dev Environment Setup

:: Check for admin privileges
net session >nul 2>&1
if %errorlevel% neq 0 (
    echo Requesting administrator privileges...
    powershell -Command "Start-Process '%~f0' -Verb RunAs"
    exit /b
)

:: Run the setup script
echo.
echo ========================================
echo   Windows Dev Environment Setup
echo   Launching setup.ps1...
echo ========================================
echo.

powershell -ExecutionPolicy Bypass -File "%~dp0setup.ps1"

if %errorlevel% neq 0 (
    echo.
    echo Setup encountered errors. Check output above.
    pause
    exit /b 1
)

echo.
echo Setup complete. Press any key to close.
pause
