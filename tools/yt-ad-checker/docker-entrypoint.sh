#!/bin/sh
# xvfb-run cannot be PID 1: it blocks until Xvfb pings SIGUSR1, and the kernel
# does not deliver that signal to PID 1, so the app would never start.
# Start Xvfb here and exec the app instead, which also keeps SIGTERM handling.
set -eu

if [ "${HEADLESS:-false}" = "true" ]; then
    exec python -m app.main
fi

DISPLAY_NUM="${DISPLAY_NUM:-99}"
export DISPLAY=":${DISPLAY_NUM}"

Xvfb "${DISPLAY}" -screen 0 1280x800x24 -nolisten tcp >/tmp/xvfb.log 2>&1 &
xvfb_pid=$!

i=0
while [ ! -e "/tmp/.X11-unix/X${DISPLAY_NUM}" ]; do
    i=$((i + 1))
    if [ "$i" -gt 100 ]; then
        echo "Xvfb did not start on ${DISPLAY}" >&2
        cat /tmp/xvfb.log >&2 || true
        kill "$xvfb_pid" 2>/dev/null || true
        exit 1
    fi
    sleep 0.1
done

exec python -m app.main
