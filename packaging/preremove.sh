#!/bin/sh
set -e

if command -v systemctl >/dev/null 2>&1; then
    systemctl stop bandwidth-monitor 2>/dev/null || true
    systemctl disable bandwidth-monitor 2>/dev/null || true
    systemctl daemon-reload
fi
