#!/bin/bash
# <xbar.title>Bandwidth Monitor</xbar.title>
# <xbar.version>v1.0</xbar.version>
# <xbar.author>bandwidth-monitor</xbar.author>
# <xbar.desc>Shows live network stats from the bandwidth-monitor server</xbar.desc>
# <xbar.dependencies>curl,jq</xbar.dependencies>
# <swiftbar.hideAbout>true</swiftbar.hideAbout>
# <swiftbar.hideRunInTerminal>true</swiftbar.hideRunInTerminal>
# <swiftbar.hideDisablePlugin>true</swiftbar.hideDisablePlugin>

# ── Configuration ──
# Comma-separated list of servers to try in order (first reachable wins)
SERVERS="${BW_SERVERS:-${BW_SERVER:-http://localhost:8080}}"
# Default preferred interface (used if no per-server override matches)
PREFER_IFACE="${BW_PREFER_IFACE:-}"
# Per-server preferred interface overrides: "url=iface,url=iface"
# Example: BW_PREFER_IFACE_MAP="http://198.51.100.1:8080=eth0,http://203.0.113.1:8080=ppp0"
PREFER_IFACE_MAP="${BW_PREFER_IFACE_MAP:-}"
# ────────────────────

export PATH="/opt/homebrew/bin:/usr/local/bin:$PATH"

# Get local IP addresses to determine which subnet we're on
LOCAL_IPS=$(ifconfig 2>/dev/null | grep 'inet ' | awk '{print $2}' | grep -v '^127\.')

# Sort server list: servers on a reachable subnet first
IFS=',' read -ra SERVER_LIST <<< "$SERVERS"
SORTED_SERVERS=()
REMAINING_SERVERS=()
for s in "${SERVER_LIST[@]}"; do
    s=$(echo "$s" | xargs)
    # Extract IP from URL (http://1.2.3.4:8080 -> 1.2.3.4)
    srv_ip=$(echo "$s" | sed -E 's|https?://([0-9]+\.[0-9]+\.[0-9]+)\.[0-9]+(:[0-9]+)?/?.*|\1|')
    matched=false
    for lip in $LOCAL_IPS; do
        local_prefix=$(echo "$lip" | sed -E 's|([0-9]+\.[0-9]+\.[0-9]+)\.[0-9]+|\1|')
        if [ "$srv_ip" = "$local_prefix" ]; then
            SORTED_SERVERS+=("$s")
            matched=true
            break
        fi
    done
    $matched || REMAINING_SERVERS+=("$s")
done
SORTED_SERVERS+=("${REMAINING_SERVERS[@]}")

# Try each server in subnet-priority order
SERVER=""
DATA=""
for s in "${SORTED_SERVERS[@]}"; do
    DATA=$(curl -sf --max-time 1 -w '' "${s}/api/summary" 2>/dev/null)
    if [ -n "$DATA" ]; then
        SERVER="$s"
        break
    fi
done

# Resolve per-server preferred interface
if [ -n "$PREFER_IFACE_MAP" ]; then
    IFS=',' read -ra IFACE_PAIRS <<< "$PREFER_IFACE_MAP"
    for pair in "${IFACE_PAIRS[@]}"; do
        pair=$(echo "$pair" | xargs)
        map_server="${pair%%=*}"
        map_iface="${pair#*=}"
        if [ "$map_server" = "$SERVER" ]; then
            PREFER_IFACE="$map_iface"
            break
        fi
    done
fi

if [ -z "$DATA" ]; then
    echo "⚡ --"
    echo "---"
    echo "Server unreachable | color=red"
    for s in "${SERVER_LIST[@]}"; do echo "  $(echo "$s" | xargs) | color=#888888 size=11"; done
    echo "---"
    echo "Open Dashboard | href=${SERVER_LIST[0]}"
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
