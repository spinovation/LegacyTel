#!/bin/bash
# ==============================================================================
# LegacyTel GitHub Deployer Utility
# ==============================================================================
# This script initializes a local Git repository, commits all production-ready 
# Go codebase files, configurations, and isolated platform guides, and pushes 
# them to your target GitHub repository: https://github.com/spinovation/LegacyTel
# ==============================================================================

set -e

# Target Remote Repository URL
REMOTE_URL="https://github.com/spinovation/LegacyTel.git"

echo "=== 1. Checking local file directory ==="
if [ ! -f "go.mod" ] || [ ! -f "config.yaml" ]; then
    echo "[ERROR] Run this script from the root of the LegacyTel project folder!"
    exit 1
fi

echo "=== 2. Creating .gitignore file ==="
cat <<EOF > .gitignore
# Binaries
bin/
legacytel
legacytel-zos
legacytel-iseries
legacytel-tandem
*.exe
*.dll
*.so
*.dylib

# Local Configuration Overrides
*.local.yaml
*.local.json

# Local Certificates and Private Keys (Strictly keep out of source control)
**/certs/*.key
**/certs/*.pem
**/certs/*.p12
**/certs/*.crt
**/certs/*.srl
**/certs/*.cnf
certs/

# Log Files and History Databases
logs/
*.log
*.sqlite
EOF

echo "=== 3. Initializing Local Git Repository ==="
if [ ! -d ".git" ]; then
    git init
    echo "[SUCCESS] Local git repository initialized."
else
    echo "[NOTE] Git repository already exists. Re-initializing remote parameters..."
fi

echo "=== 4. Staging files for commit ==="
git add .

echo "=== 5. Creating Initial Commit ==="
# Check if there is anything to commit
if git diff-index --quiet HEAD --; then
    echo "[NOTE] No changes detected since last commit."
else
    git commit -m "Initial commit: Production-ready LegacyTel agent with standard OTel data models, secure mTLS listeners, and isolated z/OS, AS400, and Tandem deployment guides"
    echo "[SUCCESS] Commit created successfully."
fi

# Force branch naming to main
git branch -M main

echo "=== 6. Binding GitHub Remote Origin ==="
# Remove origin if it already exists to prevent duplication errors
git remote remove origin 2>/dev/null || true
git remote add origin "${REMOTE_URL}"
echo "[SUCCESS] Remote origin bound to: ${REMOTE_URL}"

echo ""
echo "=============================================================================="
echo "=== READY TO PUSH! ==="
echo "=============================================================================="
echo "Before executing the push command below, ensure that:"
echo "1. You have created an empty repository named 'LegacyTel' on your GitHub account:"
echo "   https://github.com/spinovation/LegacyTel"
echo "2. Your local shell is logged in to git (credentials, PAT, or SSH keys configured)."
echo "------------------------------------------------------------------------------"
echo "To push the project, run this final command in your terminal:"
echo "   git push -u origin main"
echo "=============================================================================="
