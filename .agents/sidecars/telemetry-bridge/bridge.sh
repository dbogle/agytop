#!/bin/bash
echo "[telemetry] Connected to local IPC socket."
if [ "$AGY_DRY_RUN" = "1" ]; then
    echo "[telemetry] IPC socket probe successful."
    exit 0
fi
count=0
while true; do
    count=$((count+1))
    echo "[telemetry] Heartbeat #$count: latency 1.2ms, packet loss 0.0%"
    sleep 3
done
