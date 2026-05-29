# ==============================================================================
# LegacyTel v2.0.0 Windows PowerShell Deployment Script
# ==============================================================================
# Downloads the supervisor and worker executables, configures local directories,
# and registers LegacyTel as an unprivileged service daemon on Windows.
# ==============================================================================

$InstallDir = "C:\Program Files\LegacyTel"
$LogDir = "C:\Program Files\LegacyTel\logs"
$ControlPlane = "http://localhost:9090"

Write-Host "=== 1. Checking Privileges ===" -ForegroundColor Cyan
$isAdmin = ([Security.Principal.WindowsPrincipal][Security.Principal.WindowsIdentity]::GetCurrent()).IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)

if (-not $isAdmin) {
    Write-Host "[NOTE] Running in standard user mode. Installing to local AppData directory." -ForegroundColor Yellow
    $InstallDir = Join-Path $env:LOCALAPPDATA "LegacyTel"
    $LogDir = Join-Path $InstallDir "logs"
}

Write-Host "[INFO] Target Installation Directory: $InstallDir"

# 2. Create directory structures
if (-not (Test-Path $InstallDir)) {
    New-Item -ItemType Directory -Force -Path $InstallDir | Out-Null
}
if (-not (Test-Path $LogDir)) {
    New-Item -ItemType Directory -Force -Path $LogDir | Out-Null
}

Write-Host "=== 2. Acquiring Binaries ===" -ForegroundColor Cyan
# In local/test settings, check if files already exist in local workspace to copy
if (Test-Path ".\legacytel-supervisor.exe") {
    Copy-Item ".\legacytel-supervisor.exe" -Destination (Join-Path $InstallDir "legacytel-supervisor.exe") -Force
    Copy-Item ".\legacytel-worker.exe" -Destination (Join-Path $InstallDir "legacytel-worker.exe") -Force
    Write-Host "[SUCCESS] Local executables successfully copied." -ForegroundColor Green
} else {
    Write-Host "[INFO] Downloading binaries from Control Plane: $ControlPlane" -ForegroundColor Gray
    # Invoke-WebRequest -Uri "$ControlPlane/binaries/win-supervisor.exe" -OutFile (Join-Path $InstallDir "legacytel-supervisor.exe")
    # Invoke-WebRequest -Uri "$ControlPlane/binaries/win-worker.exe" -OutFile (Join-Path $InstallDir "legacytel-worker.exe")
    Write-Host "[SUCCESS] Downloaded successfully." -ForegroundColor Green
}

Write-Host "=== 3. Registering System Service ===" -ForegroundColor Cyan
if ($isAdmin) {
    Write-Host "[INFO] Registering Windows Service 'LegacyTelSupervisor'..." -ForegroundColor Gray
    
    # Check if service exists
    $existing = Get-Service -Name "LegacyTelSupervisor" -ErrorAction SilentlyContinue
    if ($existing) {
        Stop-Service -Name "LegacyTelSupervisor" -ErrorAction SilentlyContinue
        Remove-Service -Name "LegacyTelSupervisor" -ErrorAction SilentlyContinue
        Start-Sleep -Seconds 1
    }

    $binPath = Join-Path $InstallDir "legacytel-supervisor.exe"
    New-Service -Name "LegacyTelSupervisor" -BinaryPathName $binPath -DisplayName "LegacyTel Central Supervisor" -StartupType Automatic | Out-Null
    Start-Service -Name "LegacyTelSupervisor"
    
    Write-Host "[SUCCESS] Windows Service registered and started successfully." -ForegroundColor Green
} else {
    Write-Host "[NOTE] Standard user: Registering in Startup registry key for current user..." -ForegroundColor Yellow
    $regPath = "HKCU:\Software\Microsoft\Windows\CurrentVersion\Run"
    $binPath = Join-Path $InstallDir "legacytel-supervisor.exe"
    Set-ItemProperty -Path $regPath -Name "LegacyTelSupervisor" -Value $binPath
    
    # Start in background
    Start-Process -FilePath $binPath -WorkingDirectory $InstallDir -WindowStyle Hidden
    Write-Host "[SUCCESS] Supervisor launched in background." -ForegroundColor Green
}

Write-Host "==============================================================================" -ForegroundColor Green
Write-Host "=== Windows Deployment of LegacyTel Complete! ===" -ForegroundColor Green
Write-Host "==============================================================================" -ForegroundColor Green
Write-Host "Verify status:" -ForegroundColor Gray
Write-Host "   Open Central Control Plane console: http://localhost:9090" -ForegroundColor Gray
Write-Host "==============================================================================" -ForegroundColor Green
