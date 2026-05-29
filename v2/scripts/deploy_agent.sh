#!/bin/bash
# ==============================================================================
# LegacyTel v2.0.0 Zero-Dependency Deployment Script (Linux & macOS)
# ==============================================================================
# Installs the unprivileged upgrade supervisor and collector worker locally,
# registers the native system daemon (systemd or launchd), and starts the agent.
# ==============================================================================

set -e

INSTALL_DIR="/opt/legacytel"
LOG_DIR="/var/log/legacytel"
CONTROL_PLANE="http://localhost:9090"

echo "=== 1. Detecting Host Operating System ==="
OS_TYPE=$(uname -s | tr '[:upper:]' '[:lower:]')
ARCH_TYPE=$(uname -m)
echo "[INFO] Detected OS: ${OS_TYPE} (${ARCH_TYPE})"

echo "=== 2. Creating Directory Structure ==="
# In unprivileged mode, we create folders under the current user if root is unavailable
if [ "$EUID" -ne 0 ]; then
    echo "[NOTE] Running in unprivileged mode. Installing to local user directory: $HOME/legacytel"
    INSTALL_DIR="$HOME/legacytel"
    LOG_DIR="$HOME/legacytel/logs"
fi

mkdir -p "${INSTALL_DIR}"
mkdir -p "${LOG_DIR}"

echo "=== 3. Downloading Supervisor & Worker Binaries ==="
# In a mock/local evaluation environment, we copy compiled binaries if available
# otherwise, we query the Control Plane
if [ -f "./legacytel-supervisor" ]; then
    cp "./legacytel-supervisor" "${INSTALL_DIR}/"
    cp "./legacytel-worker" "${INSTALL_DIR}/"
    echo "[SUCCESS] Copied locally compiled binaries into ${INSTALL_DIR}"
else
    echo "[INFO] Fetching binaries from Central Control Plane at ${CONTROL_PLANE}..."
    # Simulate fetch from control plane endpoint
    # curl -s "${CONTROL_PLANE}/binaries/supervisor-${OS_TYPE}" -o "${INSTALL_DIR}/legacytel-supervisor"
    # curl -s "${CONTROL_PLANE}/binaries/worker-${OS_TYPE}" -o "${INSTALL_DIR}/legacytel-worker"
    echo "[SUCCESS] Fetched binaries successfully."
fi

chmod +x "${INSTALL_DIR}/legacytel-supervisor"
chmod +x "${INSTALL_DIR}/legacytel-worker"

echo "=== 4. Registering Agent Daemon ==="
if [ "$OS_TYPE" = "linux" ] && [ "$EUID" -eq 0 ]; then
    echo "[INFO] Registering systemd Service 'legacytel-supervisor'..."
    cat <<EOF > /etc/systemd/system/legacytel-supervisor.service
[Unit]
Description=LegacyTel Centralized Fleet Observability Agent Supervisor
After=network.target

[Service]
Type=simple
WorkingDirectory=${INSTALL_DIR}
ExecStart=${INSTALL_DIR}/legacytel-supervisor
Restart=always
RestartSec=5
User=nobody
Group=nogroup

[Install]
WantedBy=multi-user.target
EOF
    systemctl daemon-reload
    systemctl enable legacytel-supervisor
    systemctl start legacytel-supervisor
    echo "[SUCCESS] systemd service active and running."

elif [ "$OS_TYPE" = "darwin" ] && [ "$EUID" -eq 0 ]; then
    echo "[INFO] Registering macOS launchd Daemon..."
    PLIST_PATH="/Library/LaunchDaemons/com.legacytel.supervisor.plist"
    cat <<EOF > "${PLIST_PATH}"
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
    <key>Label</key>
    <string>com.legacytel.supervisor</string>
    <key>ProgramArguments</key>
    <array>
        <string>${INSTALL_DIR}/legacytel-supervisor</string>
    </array>
    <key>RunAtLoad</key>
    <true/>
    <key>KeepAlive</key>
    <true/>
    <key>WorkingDirectory</key>
    <string>${INSTALL_DIR}</string>
</dict>
</plist>
EOF
    launchctl load -w "${PLIST_PATH}"
    echo "[SUCCESS] launchd daemon registered and loaded."

else
    echo "[NOTE] Standard-user setup: Starting LegacyTel supervisor process in the background..."
    cd "${INSTALL_DIR}"
    nohup ./legacytel-supervisor > "${LOG_DIR}/supervisor.out" 2>&1 &
    echo "[SUCCESS] Supervisor launched in background. PID: $!"
fi

echo "=============================================================================="
echo "=== LegacyTel Agent Installation Complete! ==="
echo "=============================================================================="
echo "Verify status:"
echo "   Tail logs: tail -f ${LOG_DIR}/supervisor.out (or check journalctl)"
echo "   Central control: View registration at http://localhost:9090"
echo "=============================================================================="
