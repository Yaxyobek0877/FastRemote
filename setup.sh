#!/bin/bash
set -e

echo "=== FastRemote Setup ==="
echo ""

PROJECT_DIR="$(cd "$(dirname "$0")" && pwd)"

# Check/Install Go
if ! command -v go &> /dev/null; then
    echo "[1/4] Installing Go..."
    GO_VERSION="1.22.5"
    wget -q "https://go.dev/dl/go${GO_VERSION}.linux-amd64.tar.gz" -O /tmp/go.tar.gz
    sudo tar -C /usr/local -xzf /tmp/go.tar.gz
    echo 'export PATH=$PATH:/usr/local/go/bin' | sudo tee /etc/profile.d/go.sh > /dev/null
    export PATH=$PATH:/usr/local/go/bin
    echo "  Go $(go version) installed"
else
    echo "[1/4] Go already installed: $(go version)"
fi

# Build server
echo "[2/4] Building server..."
cd "$PROJECT_DIR/server"
go mod tidy 2>/dev/null || true
go build -o "$PROJECT_DIR/server/fastremote-server" .
echo "  Server binary: server/fastremote-server"

# Build agent
echo "[3/4] Building agent..."
cd "$PROJECT_DIR/agent"
go mod tidy 2>/dev/null || true

# Linux agent
go build -o "$PROJECT_DIR/agent/fastremote-agent" .
echo "  Linux agent: agent/fastremote-agent"

# Windows agent
GOOS=windows GOARCH=amd64 go build -o "$PROJECT_DIR/agent/fastremote-agent.exe" .
echo "  Windows agent: agent/fastremote-agent.exe"

# Install web dependencies and build
echo "[4/4] Building web dashboard..."
cd "$PROJECT_DIR/web"
npm install --silent 2>/dev/null
npm run build 2>/dev/null
echo "  Dashboard built: web/dist/"

echo ""
echo "=== Setup Complete ==="
echo ""
echo "To start the system, run: ./run.sh"
echo ""
echo "Default credentials:"
echo "  Username: admin"
echo "  Password: admin123"
echo ""
echo "Agent key: fastremote-agent-key-change-me"
echo ""
