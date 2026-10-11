#!/bin/sh
# tmux 3.0a has no foreground (-D) mode. Keep a supervised process alive
# and recreate the dedicated server if it crashes. Session recovery belongs
# to the web gateway; this script never replays commands or kills a server.
set -u

if [ "$#" -ne 2 ]; then
    echo "Usage: terminal-server.sh CONFIG SOCKET" >&2
    exit 2
fi

terminal_config=$1
terminal_socket=$2
terminal_tmux=/usr/bin/tmux

if [ ! -r "$terminal_config" ]; then
    echo "Terminal configuration is not readable: $terminal_config" >&2
    exit 1
fi

trap 'exit 0' TERM INT HUP

server_ready() {
    "$terminal_tmux" -S "$terminal_socket" show-options -s exit-empty >/dev/null 2>&1
}

ensure_server() {
    if server_ready; then
        return 0
    fi
    echo "Starting FastRemote terminal server" >&2
    "$terminal_tmux" -f "$terminal_config" -S "$terminal_socket" start-server || return 1
    server_ready
}

if ! ensure_server; then
    echo "FastRemote terminal server did not start" >&2
    exit 1
fi

# Type=notify makes the HTTP gateway's After= dependency wait for a usable
# socket. Standalone checks do not need systemd or a notification socket.
if [ -n "${NOTIFY_SOCKET:-}" ]; then
    /usr/bin/systemd-notify --ready --status="Terminal server ready" || exit 1
fi

while :; do
    sleep 2 &
    wait "$!"
    if ! ensure_server; then
        echo "FastRemote terminal server recovery failed; supervisor will retry" >&2
        exit 1
    fi
done
