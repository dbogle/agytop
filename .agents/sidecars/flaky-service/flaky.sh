#!/bin/bash
echo "[flaky] Starting transient background check..."
if [ "$AGY_DRY_RUN" = "1" ]; then
    echo "[flaky] Dry run check succeeded."
    exit 0
fi
sleep 1
echo "[flaky] ERROR: Connection to remote daemon timed out after 1000ms" >&2
exit 42
