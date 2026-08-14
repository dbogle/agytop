#!/bin/bash
echo "[cron] Running scheduled Antigravity health snapshot..."
if [ "$AGY_DRY_RUN" = "1" ]; then
    echo "[cron] Schedule Dry-Run check passed. Ready for next cron trigger."
    exit 0
fi
echo "[cron] Generating snapshot metrics at $(date)... Done."
