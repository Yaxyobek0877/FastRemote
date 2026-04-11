#!/bin/bash

PROJECT_DIR="$(cd "$(dirname "$0")" && pwd)"
export PATH=$PATH:/usr/local/go/bin

# Default config
export PORT="${PORT:-8080}"
export AGENT_KEY="${AGENT_KEY:-fastremote-agent-key-change-me}"
export JWT_SECRET="${JWT_SECRET:-fastremote-jwt-secret-change-in-production}"
export SERVER_URL="${SERVER_URL:-ws://localhost:${PORT}}"
export DEVICE_NAME="${DEVICE_NAME:-$(hostname)}"

# Colors
GREEN='\033[0;32m'
CYAN='\033[0;36m'
YELLOW='\033[1;33m'
NC='\033[0m'

echo ""
echo -e "${CYAN}=== FastRemote ===${NC}"
echo ""

# Parse args
START_AGENT=false
DEV_MODE=false

for arg in "$@"; do
    case $arg in
        --agent) START_AGENT=true ;;
        --dev) DEV_MODE=true ;;
        *) ;;
    esac
done

cleanup() {
    echo ""
    echo "Shutting down..."
    kill $SERVER_PID 2>/dev/null
    kill $WEB_PID 2>/dev/null
    kill $AGENT_PID 2>/dev/null
    exit 0
}
trap cleanup SIGINT SIGTERM

# Start server
echo -e "${GREEN}[1] Starting server on port ${PORT}...${NC}"
cd "$PROJECT_DIR/server"
if [ ! -f "fastremote-server" ]; then
    echo "  Building server..."
    go build -o fastremote-server .
fi

./fastremote-server &
SERVER_PID=$!
sleep 1

if ! kill -0 $SERVER_PID 2>/dev/null; then
    echo "ERROR: Server failed to start"
    exit 1
fi
echo -e "  Server running (PID: $SERVER_PID)"

# Start web dashboard
echo -e "${GREEN}[2] Starting web dashboard...${NC}"
cd "$PROJECT_DIR/web"

if [ "$DEV_MODE" = true ]; then
    npm run dev -- --host 0.0.0.0 &
    WEB_PID=$!
    echo -e "  Dashboard running at ${CYAN}http://localhost:5173${NC} (dev mode)"
else
    # Serve built files using a simple Go static server if available
    if [ -d "dist" ]; then
        npx -y serve dist -l 5173 -s &
        WEB_PID=$!
        echo -e "  Dashboard running at ${CYAN}http://localhost:5173${NC}"
    else
        echo -e "  ${YELLOW}Dashboard not built. Run: cd web && npm run build${NC}"
        echo -e "  Starting dev server instead..."
        npm run dev -- --host 0.0.0.0 &
        WEB_PID=$!
        echo -e "  Dashboard running at ${CYAN}http://localhost:5173${NC} (dev mode)"
    fi
fi

# Start agent (optional)
if [ "$START_AGENT" = true ]; then
    echo -e "${GREEN}[3] Starting agent...${NC}"
    cd "$PROJECT_DIR/agent"
    if [ ! -f "fastremote-agent" ]; then
        echo "  Building agent..."
        go build -o fastremote-agent .
    fi

    ./fastremote-agent &
    AGENT_PID=$!
    echo -e "  Agent running (PID: $AGENT_PID)"
fi

echo ""
echo -e "${CYAN}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"
echo -e "  ${GREEN}FastRemote is running!${NC}"
echo ""
echo -e "  Dashboard:  ${CYAN}http://localhost:5173${NC}"
echo -e "  Server API: ${CYAN}http://localhost:${PORT}${NC}"
echo ""
echo -e "  Login:      ${YELLOW}admin / admin123${NC}"
echo -e "  Agent Key:  ${YELLOW}${AGENT_KEY}${NC}"
echo ""
echo -e "  To start agent: ${CYAN}./run.sh --agent${NC}"
echo -e "  Dev mode:       ${CYAN}./run.sh --dev${NC}"
echo -e "${CYAN}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"
echo ""
echo "Press Ctrl+C to stop all services."

# Wait for all processes
wait
