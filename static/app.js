(function() {
    'use strict';

    // ── Theme management ──
    (function initTheme() {
        var saved = localStorage.getItem('bw-theme') || 'auto';
        document.documentElement.setAttribute('data-theme', saved);
        var toggle = document.getElementById('themeToggle');
        if (toggle) {
            var btns = toggle.querySelectorAll('.theme-btn');
            btns.forEach(function(b) {
                b.classList.toggle('active', b.getAttribute('data-theme-val') === saved);
            });
            toggle.addEventListener('click', function(e) {
                var btn = e.target.closest('.theme-btn');
                if (!btn) return;
                var val = btn.getAttribute('data-theme-val');
                document.documentElement.setAttribute('data-theme', val);
                localStorage.setItem('bw-theme', val);
                btns.forEach(function(b) { b.classList.toggle('active', b.getAttribute('data-theme-val') === val); });
                if (window._updateChartsForTheme) window._updateChartsForTheme();
            });
        }
        if (window.matchMedia) {
            window.matchMedia('(prefers-color-scheme: dark)').addEventListener('change', function() {
                if ((localStorage.getItem('bw-theme') || 'auto') === 'auto' && window._updateChartsForTheme) window._updateChartsForTheme();
            });
        }
    })();

    // ── Tab navigation ──
    window._switchTab = function(tab) {
        var panels = { traffic: 'tabTraffic', dns: 'tabDns', wifi: 'tabWifi' };
        for (var k in panels) {
            var p = document.getElementById(panels[k]);
            if (p) p.classList.toggle('active', k === tab);
        }
        document.querySelectorAll('.main-nav-tab').forEach(function(t) {
            t.classList.toggle('active', t.getAttribute('data-tab') === tab);
        });
    };

    function formatBytes(bytes, dec) {
        if (dec === undefined) dec = 1;
        if (bytes === 0) return '0 B';
        var k = 1024, s = ['B','KB','MB','GB','TB'];
        var i = Math.floor(Math.log(bytes) / Math.log(k));
        return (bytes / Math.pow(k, i)).toFixed(dec) + ' ' + s[i];
    }

    function formatRate(bps) {
        var mbps = (bps * 8) / 1e6;
        if (mbps < 0.01 && mbps > 0) return mbps.toFixed(4) + ' Mbit/s';
        if (mbps < 1) return mbps.toFixed(2) + ' Mbit/s';
        if (mbps < 100) return mbps.toFixed(1) + ' Mbit/s';
        return mbps.toFixed(0) + ' Mbit/s';
    }

    function rankClass(i) { return i === 0 ? 'rank rank-1' : 'rank'; }

    // Convert ISO 3166-1 alpha-2 to flag emoji
    function countryFlag(cc) {
        if (!cc || cc.length !== 2) return '';
        var a = cc.toUpperCase();
        return String.fromCodePoint(0x1F1E6 + a.charCodeAt(0) - 65, 0x1F1E6 + a.charCodeAt(1) - 65);
    }

    var chartColors = [
        { rx: '#22d3ee', tx: '#a78bfa', rxBg: 'rgba(34,211,238,0.08)', txBg: 'rgba(167,139,250,0.08)' },
        { rx: '#34d399', tx: '#fb923c', rxBg: 'rgba(52,211,153,0.08)', txBg: 'rgba(251,146,60,0.08)' },
        { rx: '#60a5fa', tx: '#f472b6', rxBg: 'rgba(96,165,250,0.08)', txBg: 'rgba(244,114,182,0.08)' },
        { rx: '#fbbf24', tx: '#e879f9', rxBg: 'rgba(251,191,36,0.08)', txBg: 'rgba(232,121,249,0.08)' },
    ];

    var ctx = document.getElementById('trafficChart').getContext('2d');
    var zeroLinePlugin = {
        id: 'zeroLine',
        afterDraw: function(chart) {
            if (chart.canvas.id !== 'trafficChart') return;
            var yScale = chart.scales.y;
            if (!yScale) return;
            var y = yScale.getPixelForValue(0);
            if (y < yScale.top || y > yScale.bottom) return;
            var ctx = chart.ctx;
            ctx.save();
            ctx.beginPath();
            ctx.moveTo(chart.chartArea.left, y);
            ctx.lineTo(chart.chartArea.right, y);
            ctx.lineWidth = 1;
            ctx.strokeStyle = getComputedStyle(document.documentElement).getPropertyValue('--text-2').trim() || '#71717a';
            ctx.setLineDash([4, 3]);
            ctx.stroke();
            ctx.restore();
        }
    };

    var trafficChart = new Chart(ctx, {
        type: 'line',
        data: { datasets: [] },
        plugins: [zeroLinePlugin],
        options: {
            responsive: true,
            maintainAspectRatio: false,
            animation: false,
            interaction: { mode: 'index', intersect: false },
            plugins: {
                legend: {
                    labels: { color: '#71717a', font: { size: 11, family: 'Inter' }, boxWidth: 12, padding: 16 }
                },
                tooltip: {
                    backgroundColor: '#18181b',
                    titleColor: '#fafafa',
                    bodyColor: '#a1a1aa',
                    borderColor: '#27272a',
                    borderWidth: 1,
                    padding: 10,
                    cornerRadius: 6,
                    titleFont: { size: 12 },
                    bodyFont: { size: 12, family: 'JetBrains Mono' },
                    callbacks: {
                        label: function(c) { return c.dataset.label + ': ' + formatRate(Math.abs(c.raw.y)); }
                    }
                }
            },
            scales: {
                x: {
                    type: 'time',
                    time: { unit: 'minute', displayFormats: { minute: 'HH:mm' } },
                    min: function() { return Date.now() - 3600000; },
                    max: function() { return Date.now(); },
                    grid: { color: '#1f1f23' },
                    ticks: { color: '#52525b', font: { size: 10 }, maxTicksLimit: 12, source: 'auto' },
                    border: { color: '#27272a' }
                },
                y: {
                    grid: { color: '#1f1f23' },
                    ticks: { color: '#52525b', font: { size: 10 }, callback: function(v) { return formatRate(Math.abs(v)); } },
                    border: { color: '#27272a' }
                }
            }
        }
    });

    var protoColors = { 'TCP': '#3b82f6', 'UDP': '#22d3ee', 'ICMP': '#f59e0b', 'Other': '#71717a' };
    var geoChartPalette = ['#3b82f6','#22d3ee','#a78bfa','#34d399','#f59e0b','#f472b6','#60a5fa','#e879f9','#fb923c','#4ade80','#818cf8','#fbbf24','#c084fc','#2dd4bf','#f87171','#71717a'];

    function makeDoughnut(id) {
        return new Chart(document.getElementById(id).getContext('2d'), {
            type: 'doughnut',
            data: { labels: [], datasets: [{ data: [], backgroundColor: [], borderWidth: 0 }] },
            options: {
                responsive: true, maintainAspectRatio: true, cutout: '65%', animation: false,
                plugins: {
                    legend: { display: false },
                    tooltip: {
                        backgroundColor: '#18181b', titleColor: '#fafafa', bodyColor: '#a1a1aa',
                        borderColor: '#27272a', borderWidth: 1,
                        callbacks: { label: function(c) { return c.label + ': ' + (c.raw >= 1024 ? formatBytes(c.raw) : c.raw.toLocaleString()); } }
                    }
                }
            }
        });
    }

    var protoChart = makeDoughnut('protoChart');
    var countryChart = makeDoughnut('countryChart');
    var asnChart = makeDoughnut('asnChart');
    var ipvChart = makeDoughnut('ipvChart');
    var ssidChart = makeDoughnut('ssidChart');
    var apClientsChart = makeDoughnut('apClientsChart');
    var apTrafficChart = makeDoughnut('apTrafficChart');
    var ssidTrafficChart = makeDoughnut('ssidTrafficChart');
    var dnsClientsChart = makeDoughnut('dnsClientsChart');
    var dnsDomainsChart = makeDoughnut('dnsDomainsChart');
    var dnsBlockedDomainsChart = makeDoughnut('dnsBlockedDomainsChart');

    // ── Chart theme sync ──
    window._updateChartsForTheme = function() {
        var s = getComputedStyle(document.documentElement);
        var grid = s.getPropertyValue('--bg-3').trim();
        var tick = s.getPropertyValue('--text-2').trim();
        var bdr = s.getPropertyValue('--border').trim();
        var bg2 = s.getPropertyValue('--bg-2').trim();
        var t0 = s.getPropertyValue('--text-0').trim();
        var t1 = s.getPropertyValue('--text-1').trim();
        trafficChart.options.scales.x.grid.color = grid;
        trafficChart.options.scales.x.ticks.color = tick;
        trafficChart.options.scales.x.border.color = bdr;
        trafficChart.options.scales.y.grid.color = grid;
        trafficChart.options.scales.y.ticks.color = tick;
        trafficChart.options.scales.y.border.color = bdr;
        trafficChart.options.plugins.legend.labels.color = tick;
        trafficChart.options.plugins.tooltip.backgroundColor = bg2;
        trafficChart.options.plugins.tooltip.titleColor = t0;
        trafficChart.options.plugins.tooltip.bodyColor = t1;
        trafficChart.options.plugins.tooltip.borderColor = bdr;
        trafficChart.update('none');
        [protoChart, countryChart, asnChart, ipvChart, ssidChart, apClientsChart, apTrafficChart, ssidTrafficChart, dnsClientsChart, dnsDomainsChart, dnsBlockedDomainsChart].forEach(function(ch) {
            ch.options.plugins.tooltip.backgroundColor = bg2;
            ch.options.plugins.tooltip.titleColor = t0;
            ch.options.plugins.tooltip.bodyColor = t1;
            ch.options.plugins.tooltip.borderColor = bdr;
            ch.update('none');
        });
    };
    window._updateChartsForTheme();

    window._toggleDetail = function(which) {
        var detail = document.getElementById(which + 'Detail');
        var toggle = document.getElementById(which + 'Toggle');
        var isOpen = detail.classList.contains('open');
        detail.classList.toggle('open');
        toggle.classList.toggle('open');
        toggle.querySelector('span').textContent = isOpen ? 'Show details' : 'Hide details';
    };

    var selectedIface = null;
    var knownIfaces = new Set();
    var MAX_PTS = 3600;
    var chartData = {};
    var sparklineData = {};

    function updateChart() {
        var ds = [], ci = 0;
        var list = selectedIface ? [selectedIface] : Array.from(knownIfaces);
        for (var n of list) {
            if (!chartData[n]) continue;
            var c = chartColors[ci % chartColors.length];
            ds.push({ label: n + ' RX', data: chartData[n].rx, borderColor: c.rx, backgroundColor: c.rxBg, fill: 'origin', tension: 0, pointRadius: 0, borderWidth: 1.5 });
            ds.push({ label: n + ' TX', data: chartData[n].tx, borderColor: c.tx, backgroundColor: c.txBg, fill: 'origin', tension: 0, pointRadius: 0, borderWidth: 1.5 });
            ci++;
        }
        trafficChart.data.datasets = ds;
        trafficChart.update('none');
    }

    function renderIfaceTabs() {
        var el = document.getElementById('ifaceTabs');
        var h = '<div class="iface-tab' + (selectedIface === null ? ' active' : '') + '" onclick="window._si(null)">All</div>';
        knownIfaces.forEach(function(n) {
            h += '<div class="iface-tab' + (selectedIface === n ? ' active' : '') + '" onclick="window._si(\'' + n + '\')">' + n + '</div>';
        });
        el.innerHTML = h;
    }

    window._si = function(n) { selectedIface = n; renderIfaceTabs(); updateChart(); };

    // Use the backend-provided iface_type; fall back to name heuristics
    function classifyIface(f) {
        if (f.iface_type) return f.iface_type;
        var n = (f.name || '').toLowerCase();
        if (/^(tun|tap|wg|ipsec|gre|vti|ovpn)/.test(n)) return 'vpn';
        if (/^(ppp|wwan|wwp|lte|qmi|mbim)/.test(n)) return 'ppp';
        if (/\.\d+$/.test(n) || /^vlan/.test(n)) return 'vlan';
        return 'physical';
    }

    var groupMeta = {
        physical: { label: 'Physical', order: 0 },
        loopback: { label: 'Loopback', order: 1 },
        vlan:     { label: 'VLAN', order: 2 },
        ppp:      { label: 'PPP / WAN', order: 3 },
        vpn:      { label: 'VPN', order: 4 }
    };

    function renderStatsRow(ifaces) {
        var groups = {};
        for (var f of ifaces) {
            var g = classifyIface(f);
            if (!groups[g]) groups[g] = { rx: 0, tx: 0, count: 0 };
            groups[g].rx += f.rx_rate || 0;
            groups[g].tx += f.tx_rate || 0;
            groups[g].count++;
        }
        var keys = Object.keys(groups).sort(function(a, b) {
            return (groupMeta[a] ? groupMeta[a].order : 99) - (groupMeta[b] ? groupMeta[b].order : 99);
        });
        var h = '';
        for (var i = 0; i < keys.length; i++) {
            var k = keys[i], meta = groupMeta[k] || { label: k }, d = groups[k];
            h += '<div class="stats-group">';
            h += '<div class="stats-group-header">' + meta.label + '<span>' + d.count + '</span></div>';
            h += '<div class="stats-group-body">';
            h += '<div><div class="stat-mini-label">RX</div><div class="stat-mini-value rx">' + formatRate(d.rx) + '</div></div>';
            h += '<div><div class="stat-mini-label">TX</div><div class="stat-mini-value tx">' + formatRate(d.tx) + '</div></div>';
            h += '</div></div>';
        }
        document.getElementById('statsRow').innerHTML = h;
    }

    function renderIfaceCard(f, groupLabel) {
        var hasErr = (f.rx_errors || 0) + (f.tx_errors || 0) > 0;
        var hasDrop = (f.rx_dropped || 0) + (f.tx_dropped || 0) > 0;
        var os = f.oper_state || 'unknown';
        var dotClass = (os === 'up') ? 'up' : (os === 'down' ? 'down' : 'unknown');
        var stateLabel = os === 'up' ? 'Up' : (os === 'down' ? 'Down' : os);
        var badge = groupLabel ? '<span class="iface-group-badge">' + groupLabel + '</span>' : '';
        var h = '<div class="iface-card"><div class="iface-name"><span>' + f.name + ' ' + badge + '</span><span class="iface-status"><span class="iface-status-dot ' + dotClass + '"></span>' + stateLabel + '</span></div>';
        h += '<div class="sparkline-wrap"><canvas class="sparkline-canvas" data-iface="' + f.name + '"></canvas></div>';
        if (f.vpn_routing) {
            h += '<div class="vpn-routing active"><span class="iface-status-dot up"></span>Routing' + (f.vpn_routing_since ? ' since ' + f.vpn_routing_since : '') + '</div>';
        } else if (f.iface_type === 'vpn' && f.vpn_tracked) {
            h += '<div class="vpn-routing inactive">Not routing</div>';
        }
        h += '<div class="iface-stats">';
        h += '<div><div class="iface-stat-label label-rx">RX Rate</div><div class="iface-stat-value" style="color:var(--rx)">' + formatRate(f.rx_rate || 0) + '</div></div>';
        h += '<div><div class="iface-stat-label label-tx">TX Rate</div><div class="iface-stat-value" style="color:var(--tx)">' + formatRate(f.tx_rate || 0) + '</div></div>';
        h += '<div><div class="iface-stat-label">RX Total</div><div class="iface-stat-value">' + formatBytes(f.rx_bytes || 0) + '</div></div>';
        h += '<div><div class="iface-stat-label">TX Total</div><div class="iface-stat-value">' + formatBytes(f.tx_bytes || 0) + '</div></div>';
        h += '<div><div class="iface-stat-label">RX Pkts</div><div class="iface-stat-value">' + (f.rx_packets || 0).toLocaleString() + '</div></div>';
        h += '<div><div class="iface-stat-label">TX Pkts</div><div class="iface-stat-value">' + (f.tx_packets || 0).toLocaleString() + '</div></div>';
        if (hasErr) h += '<div><div class="iface-stat-label label-err">Errors RX/TX</div><div class="iface-stat-value" style="color:var(--danger)">' + f.rx_errors + ' / ' + f.tx_errors + '</div></div>';
        if (hasDrop) h += '<div><div class="iface-stat-label">Drops RX/TX</div><div class="iface-stat-value" style="color:var(--warning)">' + f.rx_dropped + ' / ' + f.tx_dropped + '</div></div>';
        h += '</div>';
        if (f.addrs && f.addrs.length) {
            var v4 = [], v6 = [];
            for (var ai = 0; ai < f.addrs.length; ai++) {
                if (f.addrs[ai].indexOf(':') !== -1) v6.push(f.addrs[ai]); else v4.push(f.addrs[ai]);
            }
            h += '<div class="iface-addrs">';
            if (v4.length) h += '<div class="iface-addr-row"><span class="iface-addr-tag v4">IPv4</span><span class="iface-addr-list">' + v4.join(', ') + '</span></div>';
            if (v6.length) h += '<div class="iface-addr-row"><span class="iface-addr-tag v6">IPv6</span><span class="iface-addr-list">' + v6.join(', ') + '</span></div>';
            h += '</div>';
        }
        h += '</div>';
        return h;
    }

    function renderIfaceCards(ifaces) {
        var el = document.getElementById('ifaceGroups');
        if (!ifaces || !ifaces.length) { el.innerHTML = ''; return; }

        // Group interfaces by type
        var groups = {};
        for (var f of ifaces) {
            var g = classifyIface(f);
            if (!groups[g]) groups[g] = [];
            groups[g].push(f);
        }

        // Sort each group internally (lo last)
        for (var k in groups) {
            groups[k].sort(function(a, b) {
                if (a.name === 'lo') return 1;
                if (b.name === 'lo') return -1;
                return a.name.localeCompare(b.name);
            });
        }

        // Render all cards in a single grid with group badges
        var keys = Object.keys(groups).sort(function(a, b) {
            return (groupMeta[a] ? groupMeta[a].order : 99) - (groupMeta[b] ? groupMeta[b].order : 99);
        });

        var h = '<div class="iface-cards">';
        for (var gi = 0; gi < keys.length; gi++) {
            var gk = keys[gi];
            var meta = groupMeta[gk] || { label: gk };
            for (var fi = 0; fi < groups[gk].length; fi++) {
                h += renderIfaceCard(groups[gk][fi], meta.label);
            }
        }
        h += '</div>';
        el.innerHTML = h;
    }

    function renderTalkers(tid, talkers, vk, fmt, cls) {
        var tb = document.getElementById(tid);
        if (!talkers || !talkers.length) { tb.innerHTML = '<tr><td colspan="5" class="empty-state">No data &mdash; requires root / CAP_NET_RAW</td></tr>'; return; }

        // Detect if LOCAL_NETS direction data is present
        var hasDirection = false;
        for (var di = 0; di < talkers.length; di++) {
            if ((talkers[di].rx_bytes || 0) > 0 || (talkers[di].tx_bytes || 0) > 0) { hasDirection = true; break; }
        }

        var mx = talkers[0][vk] || 1, h = '';
        var isRate = (vk === 'rate_bytes');
        talkers.forEach(function(t, i) {
            var pct = ((t[vk] / mx) * 100).toFixed(1);
            var flag = t.country ? countryFlag(t.country) + ' ' : '';
            var geo = '';
            if (t.as_org) geo = '<span class="hostname">' + flag + (t.country_name || '') + ' &middot; AS' + (t.asn || '') + ' ' + t.as_org + '</span>';
            else if (t.country_name) geo = '<span class="hostname">' + flag + t.country_name + '</span>';
            var host = t.hostname && t.hostname !== t.ip
                ? '<span class="ip-cell">' + t.ip + '</span><span class="hostname">' + t.hostname + '</span>' + geo
                : '<span class="ip-cell">' + t.ip + '</span>' + geo;
            h += '<tr><td><span class="' + rankClass(i) + '">' + (i + 1) + '</span></td>';
            h += '<td>' + host + '</td>';
            if (hasDirection) {
                if (isRate) {
                    h += '<td style="white-space:nowrap;color:var(--rx)">' + formatRate(t.rx_rate || 0) + '</td>';
                    h += '<td style="white-space:nowrap;color:var(--tx)">' + formatRate(t.tx_rate || 0) + '</td>';
                } else {
                    h += '<td style="white-space:nowrap;color:var(--rx)">' + formatBytes(t.rx_bytes || 0) + '</td>';
                    h += '<td style="white-space:nowrap;color:var(--tx)">' + formatBytes(t.tx_bytes || 0) + '</td>';
                }
            }
            h += '<td style="white-space:nowrap">' + fmt(t[vk]) + '</td>';
            h += '<td class="bar-cell"><div class="bar-bg"></div><div class="bar-fill ' + cls + '" style="width:' + pct + '%"></div></td></tr>';
        });

        // Update table header dynamically
        var thead = tb.parentElement.querySelector('thead tr');
        if (thead) {
            if (hasDirection) {
                if (isRate) {
                    thead.innerHTML = '<th>#</th><th>Host</th><th>RX</th><th>TX</th><th>Total</th><th style="width:22%"></th>';
                } else {
                    thead.innerHTML = '<th>#</th><th>Host</th><th>RX</th><th>TX</th><th>Total</th><th style="width:22%"></th>';
                }
            } else {
                thead.innerHTML = '<th>#</th><th>Host</th><th>' + (isRate ? 'Rate' : 'Total') + '</th><th style="width:28%"></th>';
            }
        }
        tb.innerHTML = h;
    }

    function drawSparkline(canvas, points) {
        if (!points || points.length < 2 || !canvas) return;
        var dpr = window.devicePixelRatio || 1;
        var dw = canvas.offsetWidth, dh = canvas.offsetHeight;
        if (!dw || !dh) return;
        canvas.width = dw * dpr; canvas.height = dh * dpr;
        var c = canvas.getContext('2d'); c.scale(dpr, dpr);
        var maxVal = 0;
        for (var i = 0; i < points.length; i++) { if (points[i].rx > maxVal) maxVal = points[i].rx; if (points[i].tx > maxVal) maxVal = points[i].tx; }
        if (maxVal === 0) return;
        var stepX = dw / (points.length - 1), pad = 2, usableH = dh - pad * 2;
        function drawArea(key, fill, stroke) {
            c.beginPath(); c.moveTo(0, dh);
            for (var j = 0; j < points.length; j++) c.lineTo(j * stepX, dh - pad - (points[j][key] / maxVal) * usableH);
            c.lineTo((points.length - 1) * stepX, dh); c.closePath(); c.fillStyle = fill; c.fill();
            c.beginPath();
            for (var j = 0; j < points.length; j++) { var y = dh - pad - (points[j][key] / maxVal) * usableH; j === 0 ? c.moveTo(0, y) : c.lineTo(j * stepX, y); }
            c.strokeStyle = stroke; c.lineWidth = 1; c.stroke();
        }
        drawArea('tx', 'rgba(167,139,250,0.15)', 'rgba(167,139,250,0.5)');
        drawArea('rx', 'rgba(34,211,238,0.15)', 'rgba(34,211,238,0.5)');
    }

    function drawAllSparklines() {
        var els = document.querySelectorAll('.sparkline-canvas');
        for (var i = 0; i < els.length; i++) drawSparkline(els[i], sparklineData[els[i].getAttribute('data-iface')]);
    }

    function updateProtoChart(data) {
        if (!data) return;
        var order = ['TCP', 'UDP', 'ICMP', 'Other'], labels = [], values = [], colors = [];
        for (var i = 0; i < order.length; i++) { if (data[order[i]]) { labels.push(order[i]); values.push(data[order[i]]); colors.push(protoColors[order[i]]); } }
        for (var k in data) { if (order.indexOf(k) === -1) { labels.push(k); values.push(data[k]); colors.push('#71717a'); } }
        protoChart.data.labels = labels;
        protoChart.data.datasets[0].data = values;
        protoChart.data.datasets[0].backgroundColor = colors;
        protoChart.update('none');
        var total = 0; for (var i = 0; i < values.length; i++) total += values[i];
        var h = '';
        for (var i = 0; i < labels.length; i++) {
            var pct = total > 0 ? ((values[i] / total) * 100).toFixed(1) : '0.0';
            h += '<div style="display:flex;align-items:center;gap:10px;margin-bottom:10px">';
            h += '<div style="width:10px;height:10px;border-radius:2px;background:' + colors[i] + ';flex-shrink:0"></div>';
            h += '<div><div style="font-size:13px;font-weight:600;color:var(--text-0)">' + labels[i] + '</div>';
            h += '<div style="font-size:11px;color:var(--text-2)">' + formatBytes(values[i]) + ' &middot; ' + pct + '%</div></div></div>';
        }
        document.getElementById('protoLegend').innerHTML = h;
    }

    function updateCountries(countries) {
        var tb = document.getElementById('countryTable');
        var legend = document.getElementById('countryLegend');
        if (!countries || !countries.length) {
            tb.innerHTML = '<tr><td colspan="5" class="empty-state">No GeoIP data &mdash; place MMDB files next to binary</td></tr>';
            countryChart.data.labels = []; countryChart.data.datasets[0].data = []; countryChart.update('none');
            legend.innerHTML = '<div style="text-align:center;color:var(--text-2);font-size:12px;padding:8px">No data</div>';
            return;
        }
        // Chart: top 8 + rest
        var chartMax = 8, chartLabels = [], chartValues = [], chartColors = [], rest = 0;
        var total = 0;
        for (var i = 0; i < countries.length; i++) total += countries[i].bytes;
        for (var i = 0; i < countries.length; i++) {
            if (i < chartMax) {
                var flag = countryFlag(countries[i].country);
                chartLabels.push(flag + ' ' + (countries[i].country_name || countries[i].country));
                chartValues.push(countries[i].bytes);
                chartColors.push(geoChartPalette[i % geoChartPalette.length]);
            } else { rest += countries[i].bytes; }
        }
        if (rest > 0) { chartLabels.push('Others'); chartValues.push(rest); chartColors.push('#52525b'); }
        countryChart.data.labels = chartLabels;
        countryChart.data.datasets[0].data = chartValues;
        countryChart.data.datasets[0].backgroundColor = chartColors;
        countryChart.update('none');
        // Legend: top entries
        var lh = '';
        var legendCount = Math.min(countries.length, chartMax);
        for (var i = 0; i < legendCount; i++) {
            var pct = total > 0 ? ((countries[i].bytes / total) * 100).toFixed(1) : '0.0';
            var flag = countryFlag(countries[i].country);
            lh += '<div style="display:flex;align-items:center;gap:8px;margin-bottom:6px">';
            lh += '<div style="width:8px;height:8px;border-radius:2px;background:' + chartColors[i] + ';flex-shrink:0"></div>';
            lh += '<div style="flex:1;min-width:0"><div style="font-size:12px;font-weight:500;color:var(--text-0);white-space:nowrap;overflow:hidden;text-overflow:ellipsis">' + flag + ' ' + (countries[i].country_name || countries[i].country) + '</div>';
            lh += '<div style="font-size:10px;color:var(--text-2)">' + formatBytes(countries[i].bytes) + ' &middot; ' + pct + '%</div></div></div>';
        }
        if (rest > 0) {
            var rpct = total > 0 ? ((rest / total) * 100).toFixed(1) : '0.0';
            lh += '<div style="display:flex;align-items:center;gap:8px;margin-bottom:6px">';
            lh += '<div style="width:8px;height:8px;border-radius:2px;background:#52525b;flex-shrink:0"></div>';
            lh += '<div style="flex:1"><div style="font-size:12px;font-weight:500;color:var(--text-0)">Others</div>';
            lh += '<div style="font-size:10px;color:var(--text-2)">' + formatBytes(rest) + ' &middot; ' + rpct + '%</div></div></div>';
        }
        legend.innerHTML = lh;
        // Detail table
        var mx = countries[0].bytes || 1, h = '';
        for (var i = 0; i < countries.length; i++) {
            var c = countries[i];
            var pct = ((c.bytes / mx) * 100).toFixed(1);
            var flag = countryFlag(c.country);
            h += '<tr><td><span class="' + rankClass(i) + '">' + (i + 1) + '</span></td>';
            h += '<td>' + flag + ' <span style="font-weight:500">' + (c.country_name || c.country) + '</span> <span class="hostname" style="display:inline">' + c.country + '</span></td>';
            h += '<td style="font-variant-numeric:tabular-nums">' + c.connections + '</td>';
            h += '<td style="white-space:nowrap">' + formatBytes(c.bytes) + '</td>';
            h += '<td class="bar-cell"><div class="bar-bg"></div><div class="bar-fill bw" style="width:' + pct + '%"></div></td></tr>';
        }
        tb.innerHTML = h;
    }

    var ipvColors = { 'IPv4': '#60a5fa', 'IPv6': '#a855f7' };

    function updateIPVersions(data) {
        var legend = document.getElementById('ipvLegend');
        if (!data) {
            ipvChart.data.labels = []; ipvChart.data.datasets[0].data = []; ipvChart.update('none');
            legend.innerHTML = '<div style="text-align:center;color:var(--text-2);font-size:12px;padding:8px">No data</div>';
            return;
        }
        var labels = [], values = [], colors = [], total = 0;
        if (data['IPv4']) { labels.push('IPv4'); values.push(data['IPv4']); colors.push(ipvColors['IPv4']); total += data['IPv4']; }
        if (data['IPv6']) { labels.push('IPv6'); values.push(data['IPv6']); colors.push(ipvColors['IPv6']); total += data['IPv6']; }
        ipvChart.data.labels = labels;
        ipvChart.data.datasets[0].data = values;
        ipvChart.data.datasets[0].backgroundColor = colors;
        ipvChart.update('none');
        var h = '';
        for (var i = 0; i < labels.length; i++) {
            var pct = total > 0 ? ((values[i] / total) * 100).toFixed(1) : '0.0';
            h += '<div style="display:flex;align-items:center;gap:10px;margin-bottom:10px">';
            h += '<div style="width:10px;height:10px;border-radius:2px;background:' + colors[i] + ';flex-shrink:0"></div>';
            h += '<div><div style="font-size:13px;font-weight:600;color:var(--text-0)">' + labels[i] + '</div>';
            h += '<div style="font-size:11px;color:var(--text-2)">' + formatBytes(values[i]) + ' &middot; ' + pct + '%</div></div></div>';
        }
        legend.innerHTML = h;
    }

    function updateASNs(asns) {
        var tb = document.getElementById('asnTable');
        var legend = document.getElementById('asnLegend');
        if (!asns || !asns.length) {
            tb.innerHTML = '<tr><td colspan="5" class="empty-state">No ASN data &mdash; place GeoLite2-ASN.mmdb next to binary</td></tr>';
            asnChart.data.labels = []; asnChart.data.datasets[0].data = []; asnChart.update('none');
            legend.innerHTML = '<div style="text-align:center;color:var(--text-2);font-size:12px;padding:8px">No data</div>';
            return;
        }
        // Chart: top 8 + rest
        var chartMax = 8, chartLabels = [], chartValues = [], chartColors = [], rest = 0;
        var total = 0;
        for (var i = 0; i < asns.length; i++) total += asns[i].bytes;
        for (var i = 0; i < asns.length; i++) {
            if (i < chartMax) {
                chartLabels.push((asns[i].as_org || 'AS' + asns[i].asn));
                chartValues.push(asns[i].bytes);
                chartColors.push(geoChartPalette[i % geoChartPalette.length]);
            } else { rest += asns[i].bytes; }
        }
        if (rest > 0) { chartLabels.push('Others'); chartValues.push(rest); chartColors.push('#52525b'); }
        asnChart.data.labels = chartLabels;
        asnChart.data.datasets[0].data = chartValues;
        asnChart.data.datasets[0].backgroundColor = chartColors;
        asnChart.update('none');
        // Legend: top entries
        var lh = '';
        var legendCount = Math.min(asns.length, chartMax);
        for (var i = 0; i < legendCount; i++) {
            var pct = total > 0 ? ((asns[i].bytes / total) * 100).toFixed(1) : '0.0';
            lh += '<div style="display:flex;align-items:center;gap:8px;margin-bottom:6px">';
            lh += '<div style="width:8px;height:8px;border-radius:2px;background:' + chartColors[i] + ';flex-shrink:0"></div>';
            lh += '<div style="flex:1;min-width:0"><div style="font-size:12px;font-weight:500;color:var(--text-0);white-space:nowrap;overflow:hidden;text-overflow:ellipsis">' + (asns[i].as_org || 'Unknown') + '</div>';
            lh += '<div style="font-size:10px;color:var(--text-2)">AS' + asns[i].asn + ' &middot; ' + formatBytes(asns[i].bytes) + ' &middot; ' + pct + '%</div></div></div>';
        }
        if (rest > 0) {
            var rpct = total > 0 ? ((rest / total) * 100).toFixed(1) : '0.0';
            lh += '<div style="display:flex;align-items:center;gap:8px;margin-bottom:6px">';
            lh += '<div style="width:8px;height:8px;border-radius:2px;background:#52525b;flex-shrink:0"></div>';
            lh += '<div style="flex:1"><div style="font-size:12px;font-weight:500;color:var(--text-0)">Others</div>';
            lh += '<div style="font-size:10px;color:var(--text-2)">' + formatBytes(rest) + ' &middot; ' + rpct + '%</div></div></div>';
        }
        legend.innerHTML = lh;
        // Detail table
        var mx = asns[0].bytes || 1, h = '';
        for (var i = 0; i < asns.length; i++) {
            var a = asns[i];
            var pct = ((a.bytes / mx) * 100).toFixed(1);
            h += '<tr><td><span class="' + rankClass(i) + '">' + (i + 1) + '</span></td>';
            h += '<td><span style="font-weight:500">' + (a.as_org || 'Unknown') + '</span> <span class="hostname" style="display:inline">AS' + a.asn + '</span></td>';
            h += '<td style="font-variant-numeric:tabular-nums">' + a.connections + '</td>';
            h += '<td style="white-space:nowrap">' + formatBytes(a.bytes) + '</td>';
            h += '<td class="bar-cell"><div class="bar-bg"></div><div class="bar-fill vol" style="width:' + pct + '%"></div></td></tr>';
        }
        tb.innerHTML = h;
    }

    var ws, rd = 1000;

    // DNS mini-bar charts
    function makeBarChart(canvasId, color) {
        return new Chart(document.getElementById(canvasId).getContext('2d'), {
            type: 'bar',
            data: { labels: [], datasets: [{ data: [], backgroundColor: color, borderWidth: 0, borderRadius: 1 }] },
            options: {
                responsive: true, maintainAspectRatio: false, animation: false,
                plugins: { legend: { display: false }, tooltip: { enabled: false } },
                scales: {
                    x: { display: false },
                    y: { display: false, beginAtZero: true }
                }
            }
        });
    }
    var dnsQChart = null, dnsBChart = null;

    function updateDNS(dns) {
        if (!dns) return;
        document.getElementById('dnsNoData').style.display = 'none';
        document.getElementById('dnsHasData').style.display = '';

        document.getElementById('dnsTotalQueries').textContent = dns.total_queries.toLocaleString();
        document.getElementById('dnsBlocked').textContent = dns.blocked_total.toLocaleString();
        document.getElementById('dnsBlockPct').textContent = dns.blocked_pct.toFixed(1) + '%';
        document.getElementById('dnsLatency').textContent = dns.avg_latency_ms.toFixed(1) + ' ms';

        // Queries time-series bar chart
        if (dns.queries_series && dns.queries_series.length) {
            if (!dnsQChart) dnsQChart = makeBarChart('dnsQueriesChart', 'rgba(59,130,246,0.6)');
            var labels = [];
            for (var i = 0; i < dns.queries_series.length; i++) labels.push(i);
            dnsQChart.data.labels = labels;
            dnsQChart.data.datasets[0].data = dns.queries_series;
            dnsQChart.update('none');
        }

        // Blocked time-series bar chart
        if (dns.blocked_series && dns.blocked_series.length) {
            if (!dnsBChart) dnsBChart = makeBarChart('dnsBlockedChart', 'rgba(239,68,68,0.6)');
            var labels2 = [];
            for (var i = 0; i < dns.blocked_series.length; i++) labels2.push(i);
            dnsBChart.data.labels = labels2;
            dnsBChart.data.datasets[0].data = dns.blocked_series;
            dnsBChart.update('none');
        }

        // ── Top DNS Clients pie chart ──
        fillDoughnut(dnsClientsChart, document.getElementById('dnsClientsLegend'),
            dns.top_clients || [],
            function(c) { return c.ip || 'Unknown'; },
            function(c) { return c.count || 0; },
            function(v) { return v.toLocaleString() + ' queries'; }
        );
        fillDetailTable('dnsClientsTable', dns.top_clients || [],
            function(c) { return c.ip || 'Unknown'; },
            function(c) { return c.count || 0; },
            function(v) { return v.toLocaleString(); }, 'bw'
        );

        // ── Top Queried Domains pie chart ──
        fillDoughnut(dnsDomainsChart, document.getElementById('dnsDomainsLegend'),
            dns.top_queried || [],
            function(d) { return d.domain || '—'; },
            function(d) { return d.count || 0; },
            function(v) { return v.toLocaleString() + ' queries'; }
        );
        fillDetailTable('dnsDomainsTable', dns.top_queried || [],
            function(d) { return d.domain || '—'; },
            function(d) { return d.count || 0; },
            function(v) { return v.toLocaleString(); }, 'bw'
        );

        // ── Top Blocked Domains pie chart ──
        fillDoughnut(dnsBlockedDomainsChart, document.getElementById('dnsBlockedDomainsLegend'),
            dns.top_blocked || [],
            function(d) { return d.domain || '—'; },
            function(d) { return d.count || 0; },
            function(v) { return v.toLocaleString() + ' blocked'; }
        );
        fillDetailTable('dnsBlockedDomainsTable', dns.top_blocked || [],
            function(d) { return d.domain || '—'; },
            function(d) { return d.count || 0; },
            function(v) { return v.toLocaleString(); }, 'vol'
        );

        // ── Upstream DNS Servers table ──
        var upTb = document.getElementById('dnsUpstreamTable');
        var ups = dns.upstreams || [];
        if (!ups.length) {
            upTb.innerHTML = '<tr><td colspan="5" class="empty-state">No upstream data</td></tr>';
        } else {
            var totalResp = 0;
            for (var i = 0; i < ups.length; i++) totalResp += ups[i].responses || 0;
            var uh = '';
            for (var i = 0; i < ups.length; i++) {
                var u = ups[i];
                var pct = totalResp > 0 ? ((u.responses / totalResp) * 100) : 0;
                var latStr = u.avg_ms > 0 ? u.avg_ms.toFixed(1) + ' ms' : '—';
                var barCls = u.avg_ms <= 20 ? 'bw' : (u.avg_ms <= 100 ? 'vol' : 'vol');
                uh += '<tr><td class="rank-cell">' + (i + 1) + '</td>';
                uh += '<td class="host-cell" title="' + u.address + '"><code>' + u.address + '</code></td>';
                uh += '<td>' + (u.responses || 0).toLocaleString() + '</td>';
                uh += '<td>' + latStr + '</td>';
                uh += '<td class="bar-cell"><div class="bar-bg"></div><div class="bar-fill ' + barCls + '" style="width:' + pct.toFixed(1) + '%"></div></td></tr>';
            }
            upTb.innerHTML = uh;
        }
    }

    // Helper: populate a doughnut chart + legend + optional detail table
    function fillDoughnut(chart, legendEl, items, labelFn, valueFn, fmtFn) {
        if (!items || !items.length) {
            chart.data.labels = []; chart.data.datasets[0].data = []; chart.update('none');
            legendEl.innerHTML = '<div style="text-align:center;color:var(--text-2);font-size:12px;padding:8px">No data</div>';
            return;
        }
        var chartMax = 8, chartLabels = [], chartValues = [], chartColors = [], rest = 0;
        var total = 0;
        for (var i = 0; i < items.length; i++) total += valueFn(items[i]);
        for (var i = 0; i < items.length; i++) {
            if (i < chartMax) {
                chartLabels.push(labelFn(items[i]));
                chartValues.push(valueFn(items[i]));
                chartColors.push(geoChartPalette[i % geoChartPalette.length]);
            } else { rest += valueFn(items[i]); }
        }
        if (rest > 0) { chartLabels.push('Others'); chartValues.push(rest); chartColors.push('#52525b'); }
        chart.data.labels = chartLabels;
        chart.data.datasets[0].data = chartValues;
        chart.data.datasets[0].backgroundColor = chartColors;
        chart.update('none');
        var lh = '', legendCount = Math.min(items.length, chartMax);
        for (var i = 0; i < legendCount; i++) {
            var pct = total > 0 ? ((valueFn(items[i]) / total) * 100).toFixed(1) : '0.0';
            lh += '<div style="display:flex;align-items:center;gap:8px;margin-bottom:6px">';
            lh += '<div style="width:8px;height:8px;border-radius:2px;background:' + chartColors[i] + ';flex-shrink:0"></div>';
            lh += '<div style="flex:1;min-width:0"><div style="font-size:12px;font-weight:500;color:var(--text-0);white-space:nowrap;overflow:hidden;text-overflow:ellipsis">' + labelFn(items[i]) + '</div>';
            lh += '<div style="font-size:10px;color:var(--text-2)">' + fmtFn(valueFn(items[i])) + ' &middot; ' + pct + '%</div></div></div>';
        }
        if (rest > 0) {
            var rpct = total > 0 ? ((rest / total) * 100).toFixed(1) : '0.0';
            lh += '<div style="display:flex;align-items:center;gap:8px;margin-bottom:6px">';
            lh += '<div style="width:8px;height:8px;border-radius:2px;background:#52525b;flex-shrink:0"></div>';
            lh += '<div style="flex:1"><div style="font-size:12px;font-weight:500;color:var(--text-0)">Others</div>';
            lh += '<div style="font-size:10px;color:var(--text-2)">' + fmtFn(rest) + ' &middot; ' + rpct + '%</div></div></div>';
        }
        legendEl.innerHTML = lh;
    }

    function fillDetailTable(tbId, items, labelFn, valueFn, fmtFn, cls) {
        var tb = document.getElementById(tbId);
        if (!items || !items.length) { tb.innerHTML = '<tr><td colspan="4" class="empty-state">No data</td></tr>'; return; }
        var mx = valueFn(items[0]) || 1, h = '';
        for (var i = 0; i < items.length; i++) {
            var pct = mx > 0 ? ((valueFn(items[i]) / mx) * 100).toFixed(1) : '0';
            h += '<tr><td><span class="' + rankClass(i) + '">' + (i + 1) + '</span></td>';
            h += '<td><span class="ip-cell">' + labelFn(items[i]) + '</span></td>';
            h += '<td style="white-space:nowrap;font-variant-numeric:tabular-nums">' + fmtFn(valueFn(items[i])) + '</td>';
            h += '<td class="bar-cell"><div class="bar-bg"></div><div class="bar-fill ' + cls + '" style="width:' + pct + '%"></div></td></tr>';
        }
        tb.innerHTML = h;
    }

    function fillTrafficDetailTable(tbId, items, labelFn) {
        var tb = document.getElementById(tbId);
        if (!items || !items.length) { tb.innerHTML = '<tr><td colspan="7" class="empty-state">No data</td></tr>'; return; }
        var mx = 1;
        for (var i = 0; i < items.length; i++) { var t = (items[i].tx_bytes || 0) + (items[i].rx_bytes || 0); if (t > mx) mx = t; }
        var h = '';
        for (var i = 0; i < items.length; i++) {
            var it = items[i], total = (it.tx_bytes || 0) + (it.rx_bytes || 0);
            var pct = mx > 0 ? ((total / mx) * 100).toFixed(1) : '0';
            h += '<tr><td><span class="' + rankClass(i) + '">' + (i + 1) + '</span></td>';
            h += '<td><span class="ip-cell">' + labelFn(it) + '</span></td>';
            h += '<td style="white-space:nowrap;font-variant-numeric:tabular-nums">' + formatBytes(it.rx_bytes || 0) + '</td>';
            h += '<td style="white-space:nowrap;font-variant-numeric:tabular-nums">' + formatBytes(it.tx_bytes || 0) + '</td>';
            h += '<td style="white-space:nowrap;font-variant-numeric:tabular-nums;color:var(--rx)">' + formatRate(it.rx_rate || 0) + '</td>';
            h += '<td style="white-space:nowrap;font-variant-numeric:tabular-nums;color:var(--tx)">' + formatRate(it.tx_rate || 0) + '</td>';
            h += '<td class="bar-cell"><div class="bar-bg"></div><div class="bar-fill bw" style="width:' + pct + '%"></div></td></tr>';
        }
        tb.innerHTML = h;
    }

    // ── WiFi (UniFi) ──
    function updateWiFi(wifi) {
        if (!wifi) return;
        document.getElementById('wifiNoData').style.display = 'none';
        document.getElementById('wifiHasData').style.display = '';

        document.getElementById('wifiTotalAPs').textContent = wifi.total_aps || 0;
        document.getElementById('wifiTotalClients').textContent = wifi.total_clients || 0;
        document.getElementById('wifiTotalSSIDs').textContent = (wifi.ssids ? wifi.ssids.length : 0);

        // AP cards
        var apEl = document.getElementById('wifiAPCards');
        if (wifi.aps && wifi.aps.length) {
            var h = '';
            for (var i = 0; i < wifi.aps.length; i++) {
                var ap = wifi.aps[i];
                var st = ap.status === 'connected' ? 'up' : (ap.status === 'disconnected' ? 'down' : 'unknown');
                var stLabel = ap.status === 'connected' ? 'Online' : (ap.status === 'disconnected' ? 'Offline' : (ap.status || 'Unknown'));
                h += '<div class="wifi-ap-card">';
                h += '<div class="wifi-ap-name"><span>' + (ap.name || ap.mac || 'AP') + ' <span class="wifi-ap-model">' + (ap.model || '') + '</span></span>';
                h += '<span class="iface-status"><span class="iface-status-dot ' + st + '"></span>' + stLabel + '</span></div>';
                h += '<div class="wifi-ap-stats">';
                h += '<div><div class="wifi-ap-stat-label">Clients</div><div class="wifi-ap-stat-value" style="color:var(--rx)">' + (ap.num_clients || 0) + '</div></div>';
                h += '<div><div class="wifi-ap-stat-label">Firmware</div><div class="wifi-ap-stat-value" style="font-size:11px">' + (ap.version || '—') + '</div></div>';
                h += '<div><div class="wifi-ap-stat-label label-rx">RX Rate</div><div class="wifi-ap-stat-value" style="font-size:11px;color:var(--rx)">' + formatRate(ap.rx_rate || 0) + '</div></div>';
                h += '<div><div class="wifi-ap-stat-label label-tx">TX Rate</div><div class="wifi-ap-stat-value" style="font-size:11px;color:var(--tx)">' + formatRate(ap.tx_rate || 0) + '</div></div>';
                h += '<div><div class="wifi-ap-stat-label">RX Total</div><div class="wifi-ap-stat-value" style="font-size:11px">' + formatBytes(ap.rx_bytes || 0) + '</div></div>';
                h += '<div><div class="wifi-ap-stat-label">TX Total</div><div class="wifi-ap-stat-value" style="font-size:11px">' + formatBytes(ap.tx_bytes || 0) + '</div></div>';
                if (ap.ip) {
                    h += '<div><div class="wifi-ap-stat-label">IP</div><div class="wifi-ap-stat-value" style="font-size:11px;font-family:JetBrains Mono,monospace">' + ap.ip + '</div></div>';
                }
                if (ap.mac) {
                    h += '<div><div class="wifi-ap-stat-label">MAC</div><div class="wifi-ap-stat-value" style="font-size:11px;font-family:JetBrains Mono,monospace">' + ap.mac + '</div></div>';
                }
                if (ap.uptime) {
                    var days = Math.floor(ap.uptime / 86400);
                    var hrs = Math.floor((ap.uptime % 86400) / 3600);
                    h += '<div><div class="wifi-ap-stat-label">Uptime</div><div class="wifi-ap-stat-value" style="font-size:11px">' + days + 'd ' + hrs + 'h</div></div>';
                }
                h += '</div></div>';
            }
            apEl.innerHTML = h;
        } else {
            apEl.innerHTML = '<div class="empty-state">No access points found</div>';
        }

        // ── Clients per AP chart ──
        var apsByClients = (wifi.aps || []).slice().sort(function(a, b) { return (b.num_clients || 0) - (a.num_clients || 0); });
        fillDoughnut(apClientsChart, document.getElementById('apClientsLegend'),
            apsByClients,
            function(a) { return a.name || a.mac || 'AP'; },
            function(a) { return a.num_clients || 0; },
            function(v) { return v + ' clients'; }
        );
        fillDetailTable('apClientsTable', apsByClients,
            function(a) { return a.name || a.mac || 'AP'; },
            function(a) { return a.num_clients || 0; },
            function(v) { return v.toLocaleString(); }, 'bw'
        );

        // ── Traffic per AP chart ──
        var apsByTraffic = (wifi.aps || []).slice().sort(function(a, b) {
            return ((b.tx_bytes || 0) + (b.rx_bytes || 0)) - ((a.tx_bytes || 0) + (a.rx_bytes || 0));
        });
        fillDoughnut(apTrafficChart, document.getElementById('apTrafficLegend'),
            apsByTraffic,
            function(a) { return a.name || a.mac || 'AP'; },
            function(a) { return (a.tx_bytes || 0) + (a.rx_bytes || 0); },
            formatBytes
        );
        fillTrafficDetailTable('apTrafficTable', apsByTraffic, function(a) { return a.name || a.mac || 'AP'; });

        // ── Clients per SSID chart ──
        fillDoughnut(ssidChart, document.getElementById('ssidLegend'),
            wifi.ssids || [],
            function(s) { return s.name || '(hidden)'; },
            function(s) { return s.num_clients || 0; },
            function(v) { return v + ' clients'; }
        );
        fillDetailTable('wifiSSIDTable', wifi.ssids || [],
            function(s) { return s.name || '—'; },
            function(s) { return s.num_clients || 0; },
            function(v) { return v.toLocaleString(); }, 'bw'
        );

        // ── Traffic per SSID chart ──
        var ssidsByTraffic = (wifi.ssids || []).slice().sort(function(a, b) {
            return ((b.tx_bytes || 0) + (b.rx_bytes || 0)) - ((a.tx_bytes || 0) + (a.rx_bytes || 0));
        });
        fillDoughnut(ssidTrafficChart, document.getElementById('ssidTrafficLegend'),
            ssidsByTraffic,
            function(s) { return s.name || '(hidden)'; },
            function(s) { return (s.tx_bytes || 0) + (s.rx_bytes || 0); },
            formatBytes
        );
        fillTrafficDetailTable('ssidTrafficTable', ssidsByTraffic, function(s) { return s.name || '—'; });

        // ── Client traffic table ──
        var clients = (wifi.clients || []).slice();
        var sortKey = (document.getElementById('wifiClientSort') || {}).value || 'traffic';
        if (sortKey === 'name') {
            clients.sort(function(a, b) { return (a.hostname || a.mac || '').localeCompare(b.hostname || b.mac || ''); });
        } else if (sortKey === 'signal') {
            clients.sort(function(a, b) { return (a.signal || -100) - (b.signal || -100); });
        } else if (sortKey === 'rate') {
            clients.sort(function(a, b) {
                return ((b.rx_rate || 0) + (b.tx_rate || 0)) - ((a.rx_rate || 0) + (a.tx_rate || 0));
            });
        } // default: already sorted by traffic descending from backend

        var filter = ((document.getElementById('wifiClientSearch') || {}).value || '').toLowerCase();
        if (filter) {
            clients = clients.filter(function(cl) {
                return (cl.hostname || '').toLowerCase().indexOf(filter) !== -1 ||
                       (cl.ip || '').indexOf(filter) !== -1 ||
                       (cl.mac || '').toLowerCase().indexOf(filter) !== -1 ||
                       (cl.ssid || '').toLowerCase().indexOf(filter) !== -1 ||
                       (cl.ap_name || '').toLowerCase().indexOf(filter) !== -1;
            });
        }

        var ctb = document.getElementById('wifiClientTable');
        if (!clients.length) {
            ctb.innerHTML = '<tr><td colspan="9" class="empty-state">' + (filter ? 'No matching clients' : 'No wireless clients') + '</td></tr>';
        } else {
            var maxBw = 1;
            for (var i = 0; i < clients.length; i++) {
                var t = (clients[i].tx_bytes || 0) + (clients[i].rx_bytes || 0);
                if (t > maxBw) maxBw = t;
            }
            var ch = '';
            for (var i = 0; i < clients.length; i++) {
                var cl = clients[i];
                var total = (cl.tx_bytes || 0) + (cl.rx_bytes || 0);
                var pct = maxBw > 0 ? ((total / maxBw) * 100).toFixed(1) : '0';
                var name = cl.hostname || cl.ip || cl.mac || '—';
                var sub = cl.ip && cl.hostname ? cl.ip : (cl.mac || '');
                var sig = cl.signal || 0;
                var sigClass = sig >= -50 ? 'sig-great' : sig >= -65 ? 'sig-good' : sig >= -75 ? 'sig-ok' : 'sig-weak';
                ch += '<tr>';
                ch += '<td><span class="' + rankClass(i) + '">' + (i + 1) + '</span></td>';
                ch += '<td><span class="ip-cell">' + name + '</span>';
                if (sub && sub !== name) ch += '<div style="font-size:10px;color:var(--text-2);font-family:JetBrains Mono,monospace">' + sub + '</div>';
                ch += '</td>';
                ch += '<td style="font-size:12px">' + (cl.ssid || '—') + '</td>';
                ch += '<td style="font-size:12px">' + (cl.ap_name || '—') + '</td>';
                ch += '<td><span class="signal-badge ' + sigClass + '">' + sig + ' dBm</span></td>';
                ch += '<td style="white-space:nowrap;font-variant-numeric:tabular-nums">' + formatBytes(cl.rx_bytes || 0) + '</td>';
                ch += '<td style="white-space:nowrap;font-variant-numeric:tabular-nums">' + formatBytes(cl.tx_bytes || 0) + '</td>';
                var clRate = (cl.rx_rate || 0) + (cl.tx_rate || 0);
                ch += '<td style="white-space:nowrap;font-variant-numeric:tabular-nums;font-size:11px">';
                ch += '<span style="color:var(--rx)">' + formatRate(cl.rx_rate || 0) + '</span>';
                ch += ' <span style="color:var(--text-2)">/</span> ';
                ch += '<span style="color:var(--tx)">' + formatRate(cl.tx_rate || 0) + '</span></td>';
                ch += '<td class="bar-cell"><div class="bar-bg"></div><div class="bar-fill bw" style="width:' + pct + '%"></div></td>';
                ch += '</tr>';
            }
            ctb.innerHTML = ch;
        }
    }

    function connect() {
        var p = location.protocol === 'https:' ? 'wss:' : 'ws:';
        ws = new WebSocket(p + '//' + location.host + '/api/ws');
        ws.onopen = function() {
            rd = 1000;
            document.getElementById('statusDot').className = 'status-dot';
            document.getElementById('statusText').textContent = 'Live';
        };
        ws.onclose = function() {
            document.getElementById('statusDot').className = 'status-dot error';
            document.getElementById('statusText').textContent = 'Reconnecting';
            setTimeout(connect, rd);
            rd = Math.min(rd * 1.5, 10000);
        };
        ws.onerror = function() { ws.close(); };
        ws.onmessage = function(e) {
            try { process(JSON.parse(e.data)); } catch(ex) { console.error(ex); }
        };
    }

    function process(d) {
        var ifaces = d.interfaces || [], bw = d.top_bandwidth || [], vol = d.top_volume || [];
        var rx = 0, tx = 0;
        for (var f of ifaces) { rx += f.rx_rate || 0; tx += f.tx_rate || 0; knownIfaces.add(f.name); }

        renderStatsRow(ifaces);

        // VPN routing banner
        var vpnActive = false, vpnSince = '', vpnName = '';
        for (var f of ifaces) {
            if (f.vpn_routing) {
                vpnActive = true;
                vpnName = f.name;
                vpnSince = f.vpn_routing_since || '';
                break;
            }
        }
        var banner = document.getElementById('vpnBanner');
        if (vpnActive) {
            banner.className = 'vpn-banner active';
            var txt = 'Traffic routed via ' + vpnName;
            document.getElementById('vpnBannerSince').textContent = vpnSince ? '(since ' + vpnSince + ')' : '';
            banner.querySelector('.vpn-banner-text').firstChild.textContent = txt + ' ';
        } else {
            banner.className = 'vpn-banner inactive';
        }

        var now = new Date();
        for (var f of ifaces) {
            if (!chartData[f.name]) chartData[f.name] = { rx: [], tx: [] };
            chartData[f.name].rx.push({ x: now, y: f.rx_rate || 0 });
            chartData[f.name].tx.push({ x: now, y: -(f.tx_rate || 0) });
            if (chartData[f.name].rx.length > MAX_PTS) { chartData[f.name].rx.shift(); chartData[f.name].tx.shift(); }
        }

        renderIfaceCards(ifaces);
        sparklineData = d.sparklines || {};
        drawAllSparklines();
        renderIfaceTabs();
        updateChart();
        updateProtoChart(d.protocols);
        updateIPVersions(d.ip_versions);
        updateCountries(d.countries);
        updateASNs(d.asns);
        renderTalkers('bwTable', bw, 'rate_bytes', formatRate, 'bw');
        renderTalkers('volTable', vol, 'total_bytes', formatBytes, 'vol');
        updateDNS(d.dns || null);
        updateWiFi(d.wifi || null);
    }

    function tick() { document.getElementById('clock').textContent = new Date().toLocaleTimeString(); }
    setInterval(tick, 1000); tick();

    // Wire search/sort on WiFi client table to re-render
    var _lastWiFi = null;
    var _origUpdateWiFi = updateWiFi;
    updateWiFi = function(wifi) {
        if (wifi) _lastWiFi = wifi;
        _origUpdateWiFi(wifi);
    };
    ['wifiClientSearch', 'wifiClientSort'].forEach(function(id) {
        var el = document.getElementById(id);
        if (el) el.addEventListener(id === 'wifiClientSearch' ? 'input' : 'change', function() {
            if (_lastWiFi) updateWiFi(_lastWiFi);
        });
    });

    connect();
})();
