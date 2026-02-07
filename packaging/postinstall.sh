#!/bin/sh
set -e

# Create working directory for GeoIP databases
mkdir -p /var/lib/bandwidth-monitor
chmod 0755 /var/lib/bandwidth-monitor

# Reload systemd
if command -v systemctl >/dev/null 2>&1; then
    systemctl daemon-reload
fi

echo ""
echo "bandwidth-monitor installed."
echo "  1. Edit /etc/bandwidth-monitor/env with your settings"
echo "  2. (Optional) Place GeoLite2-*.mmdb in /var/lib/bandwidth-monitor/"
echo "  3. systemctl enable --now bandwidth-monitor"
echo ""
