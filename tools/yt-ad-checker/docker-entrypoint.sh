#!/bin/sh
# xvfb-run cannot be PID 1: it blocks until Xvfb pings SIGUSR1, and the kernel
# does not deliver that signal to PID 1, so the app would never start.
# Start Xvfb here and exec the app instead, which also keeps SIGTERM handling.
set -eu

# Docker passes the image CMD here; the fallback keeps a bare
# `docker run --entrypoint docker-entrypoint.sh` working.
if [ "$#" -eq 0 ]; then
    set -- python -m app.main
fi

if [ "${HEADLESS:-false}" = "true" ]; then
    exec "$@"
fi

DISPLAY_NUM="${DISPLAY_NUM:-99}"
export DISPLAY=":${DISPLAY_NUM}"
SOCKET="/tmp/.X11-unix/X${DISPLAY_NUM}"
LOCK="/tmp/.X${DISPLAY_NUM}-lock"

# Only PID 1 receives SIGTERM, so Xvfb is killed with SIGKILL when the container
# stops and never removes its lock and socket. /tmp is not a volume, so both
# survive into the next start: Xvfb then dies with "Server is already active for
# display N" while the leftover socket still satisfies the wait below, and the
# app ends up on a display that does not exist ("Missing X server or $DISPLAY").
rm -f "${LOCK}" "${SOCKET}"

Xvfb "${DISPLAY}" -screen 0 1280x800x24 -nolisten tcp >/tmp/xvfb.log 2>&1 &
xvfb_pid=$!

# The socket can now only come from the Xvfb started above, so waiting for it
# means the display is really there. A dead Xvfb ends the wait with its own log
# instead of leaving the app attached to nothing.
i=0
while [ ! -e "${SOCKET}" ]; do
    i=$((i + 1))
    if [ "$i" -gt 100 ]; then
        echo "Xvfb did not start on ${DISPLAY}" >&2
        cat /tmp/xvfb.log >&2 || true
        kill "$xvfb_pid" 2>/dev/null || true
        exit 1
    fi
    sleep 0.1
done

exec "$@"
