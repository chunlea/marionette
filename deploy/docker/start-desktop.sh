#!/bin/bash
set -e

# Start Xvfb (virtual framebuffer)
echo "Starting Xvfb on display :99..."
Xvfb :99 -screen 0 1920x1080x24 -ac +extension GLX +render -noreset &
XVFB_PID=$!

# Wait for X server to be ready
sleep 2
until xdpyinfo -display :99 >/dev/null 2>&1; do
    echo "Waiting for X server..."
    sleep 1
done
echo "X server is ready"

# Start D-Bus session
export $(dbus-launch)

# Start PulseAudio (for audio support)
pulseaudio --start --exit-idle-time=-1 2>/dev/null || true

# Start window manager
echo "Starting Openbox window manager..."
openbox &

# Start a terminal for visual feedback
echo "Starting xterm..."
xterm -geometry 100x30+50+50 -title "Marionette Desktop" -e "echo 'Marionette Desktop Agent Ready'; exec bash" &

# Cleanup on exit
cleanup() {
    echo "Shutting down..."
    kill $XVFB_PID 2>/dev/null || true
    pulseaudio --kill 2>/dev/null || true
}
trap cleanup EXIT

# Start the marionette agent
echo "Starting Marionette agent..."
exec /usr/local/bin/agent "$@"
