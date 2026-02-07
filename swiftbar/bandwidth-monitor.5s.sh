#!/bin/bash
# <xbar.title>Bandwidth Monitor</xbar.title>
# <xbar.version>v1.0</xbar.version>
# <xbar.author>bandwidth-web-go</xbar.author>
# <xbar.desc>Shows live network stats from the bandwidth-web-go server</xbar.desc>
# <xbar.dependencies>curl,jq</xbar.dependencies>
# <swiftbar.hideAbout>true</swiftbar.hideAbout>
# <swiftbar.hideRunInTerminal>true</swiftbar.hideRunInTerminal>
# <swiftbar.hideDisablePlugin>true</swiftbar.hideDisablePlugin>

# ── Configuration ──
SERVER="${BW_SERVER:-http://localhost:8080}"
PREFER_IFACE="${BW_PREFER_IFACE:-}"
# ────────────────────

export PATH="/opt/homebrew/bin:/usr/local/bin:$PATH"

DATA=$(curl -sf --max-time 1 -w '' "${SERVER}/api/summary" 2>/dev/null)

if [ -z "$DATA" ]; then
    echo "⚡ --"
    echo "---"
    echo "Server unreachable | color=red"
    echo "${SERVER} | color=#888888 size=11"
    echo "---"
    echo "Open Dashboard | href=${SERVER}"
    exit 0
fi

# Single jq call produces the entire SwiftBar output
echo "$DATA" | jq -r --arg server "$SERVER" --arg prefer "$PREFER_IFACE" '
def fmt_rate:
    (. * 8 / 1000000) as $mbps |
    if ($mbps | fabs) >= 1 then
        "\($mbps | fabs * 10 | round / 10) Mb/s"
    else
        "\((. | fabs) * 8 / 1000 | round) Kb/s"
    end;

# Separate up/active and truly down interfaces (unknown is not down)
([.interfaces[] | select(.state == "up" or .state == "unknown")] | sort_by(-(.rx_rate + .tx_rate))) as $active |
([.interfaces[] | select(.state != "up" and .state != "unknown")]) as $down |

# Menu bar title: prefer $prefer iface if active, otherwise highest combined rate
([$active[] | select(.name == $prefer)] | .[0]) as $pref |
(if ($prefer != "") and $pref then $pref else ($active[0] // {rx_rate: 0, tx_rate: 0}) end) as $pri |
(if .vpn then "🔒" else "" end) as $vpn |
"\($vpn)↓\($pri.rx_rate | fmt_rate)  ↑\($pri.tx_rate | fmt_rate) | size=12 font=JetBrainsMono-Regular",
"---",
"Traffic | size=11 color=#888888",

# Active interfaces
($active[] | "  \(.name): ↓\(.rx_rate | fmt_rate)  ↑\(.tx_rate | fmt_rate) | font=JetBrainsMono-Regular size=12"),

# Down interfaces
($down[] | "  \(.name): down | color=#888888 font=JetBrainsMono-Regular size=12"),

# DNS section (only if present)
(if .dns then
    "---",
    "DNS — AdGuard Home | size=11 color=#888888",
    "  Queries:  \(.dns.total_queries) | font=JetBrainsMono-Regular size=12",
    "  Blocked:  \(.dns.blocked) (\(.dns.block_pct * 10 | round / 10)%) | font=JetBrainsMono-Regular size=12 color=#ef4444",
    "  Latency:  \(.dns.latency_ms * 10 | round / 10) ms | font=JetBrainsMono-Regular size=12"
else empty end),

# WiFi section (only if present)
(if .wifi then
    "---",
    "WiFi — UniFi | size=11 color=#888888",
    "  APs:      \(.wifi.aps) | font=JetBrainsMono-Regular size=12",
    "  Clients:  \(.wifi.clients) | font=JetBrainsMono-Regular size=12"
else empty end),

# Footer
"---",
"Open Dashboard | href=\($server)",
"Server: \($server) | color=#888888 size=10"
'
