package collector

import (
	"fmt"
	"net"
	"os"
	"strings"
	"sync"
	"time"

	vnl "github.com/vishvananda/netlink"
)

type InterfaceStat struct {
	Name            string   `json:"name"`
	IfaceType       string   `json:"iface_type"`
	OperState       string   `json:"oper_state"`
	Addrs           []string `json:"addrs,omitempty"`
	VPNRouting      bool     `json:"vpn_routing"`
	VPNRoutingSince string   `json:"vpn_routing_since,omitempty"`
	VPNTracked      bool     `json:"vpn_tracked"`
	RxBytes         uint64   `json:"rx_bytes"`
	TxBytes         uint64   `json:"tx_bytes"`
	RxPackets       uint64   `json:"rx_packets"`
	TxPackets       uint64   `json:"tx_packets"`
	RxErrors        uint64   `json:"rx_errors"`
	TxErrors        uint64   `json:"tx_errors"`
	RxDropped       uint64   `json:"rx_dropped"`
	TxDropped       uint64   `json:"tx_dropped"`
	RxRate          float64  `json:"rx_rate"`
	TxRate          float64  `json:"tx_rate"`
	Timestamp       int64    `json:"timestamp"`
}

type HistoryPoint struct {
	Timestamp int64   `json:"t"`
	RxRate    float64 `json:"rx"`
	TxRate    float64 `json:"tx"`
}

const (
	pollInterval   = 1 * time.Second
	historyMaxAge  = 24 * time.Hour
	historyPruneAt = 86400
)

type Collector struct {
	mu             sync.RWMutex
	current        map[string]*InterfaceStat
	previous       map[string]*rawStat
	history        map[string][]HistoryPoint
	ifaceTypeCache map[string]string
	vpnStatusFiles map[string]string // iface name → sentinel file path
	nlHandle       *vnl.Handle       // persistent netlink handle
	addrCache      map[int][]string  // cached addresses by link index
	addrCacheTime  time.Time         // last address refresh
	span           *spanOverlay      // nil when SPAN mode is disabled
	spanPrevRx     uint64
	spanPrevTx     uint64
	spanHasPrev    bool
	stopCh         chan struct{}
}

type rawStat struct {
	rxBytes   uint64
	txBytes   uint64
	rxPackets uint64
	txPackets uint64
	rxErrors  uint64
	txErrors  uint64
	rxDropped uint64
	txDropped uint64
	ts        time.Time
}

func New(vpnStatusFiles map[string]string) *Collector {
	if vpnStatusFiles == nil {
		vpnStatusFiles = make(map[string]string)
	}
	// Create a persistent netlink handle to avoid per-poll socket creation.
	// Falls back to package-level functions if handle creation fails.
	nlh, err := vnl.NewHandle(vnl.FAMILY_ALL)
	if err != nil {
		fmt.Fprintf(os.Stderr, "collector: failed to create persistent netlink handle: %v (will use per-call sockets)\n", err)
	}
	return &Collector{
		current:        make(map[string]*InterfaceStat),
		previous:       make(map[string]*rawStat),
		history:        make(map[string][]HistoryPoint),
		ifaceTypeCache: make(map[string]string),
		vpnStatusFiles: vpnStatusFiles,
		nlHandle:       nlh,
		addrCache:      make(map[int][]string),
		stopCh:         make(chan struct{}),
	}
}

// EnableSPAN activates pcap-based direction detection on a SPAN/mirror port.
// When enabled, the RX/TX rates and cumulative bytes for the named device are
// derived from packet inspection against localNets instead of kernel counters.
func (c *Collector) EnableSPAN(device string, promiscuous bool, localNets []*net.IPNet) {
	c.span = newSpanOverlay(device, promiscuous, localNets)
}

func (c *Collector) Run() {
	if c.span != nil {
		go c.span.run()
	}
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()
	c.poll()
	for {
		select {
		case <-ticker.C:
			c.poll()
		case <-c.stopCh:
			return
		}
	}
}

func (c *Collector) Stop() {
	if c.span != nil {
		c.span.stop()
	}
	if c.nlHandle != nil {
		c.nlHandle.Close()
	}
	close(c.stopCh)
}

func (c *Collector) GetAll() []InterfaceStat {
	c.mu.RLock()
	defer c.mu.RUnlock()
	stats := make([]InterfaceStat, 0, len(c.current))
	for _, s := range c.current {
		stats = append(stats, *s)
	}
	return stats
}

func (c *Collector) GetHistory() map[string][]HistoryPoint {
	return c.GetHistoryWindow(0)
}

// GetHistoryWindow returns rate history within the last `duration`, keyed by
// device name. A non-positive duration returns the full retained history.
// This backs the /api/interfaces/history endpoint, letting clients backfill
// the live chart after a disconnect, refresh, or hibernate.
func (c *Collector) GetHistoryWindow(duration time.Duration) map[string][]HistoryPoint {
	c.mu.RLock()
	defer c.mu.RUnlock()
	var cutoff int64
	if duration > 0 {
		cutoff = time.Now().Add(-duration).UnixMilli()
	}
	result := make(map[string][]HistoryPoint, len(c.history))
	for k, v := range c.history {
		start := 0
		for start < len(v) && v[start].Timestamp < cutoff {
			start++
		}
		cp := make([]HistoryPoint, len(v)-start)
		copy(cp, v[start:])
		result[k] = cp
	}
	return result
}

// SparkPoint is a lightweight rate pair for sparkline rendering.
type SparkPoint struct {
	RX float64 `json:"rx"`
	TX float64 `json:"tx"`
}

// GetSparklines returns the last `duration` of per-interface rate data,
// downsampled to at most `maxPoints` points.
func (c *Collector) GetSparklines(duration time.Duration, maxPoints int) map[string][]SparkPoint {
	c.mu.RLock()
	defer c.mu.RUnlock()
	cutoff := time.Now().Add(-duration).UnixMilli()
	result := make(map[string][]SparkPoint, len(c.history))

	for name, hist := range c.history {
		start := 0
		for start < len(hist) && hist[start].Timestamp < cutoff {
			start++
		}
		pts := hist[start:]
		if len(pts) == 0 {
			continue
		}

		if len(pts) <= maxPoints {
			sp := make([]SparkPoint, len(pts))
			for i, p := range pts {
				sp[i] = SparkPoint{RX: p.RxRate, TX: p.TxRate}
			}
			result[name] = sp
		} else {
			sp := make([]SparkPoint, maxPoints)
			step := float64(len(pts)) / float64(maxPoints)
			for i := 0; i < maxPoints; i++ {
				idx := int(float64(i) * step)
				if idx >= len(pts) {
					idx = len(pts) - 1
				}
				sp[i] = SparkPoint{RX: pts[idx].RxRate, TX: pts[idx].TxRate}
			}
			result[name] = sp
		}
	}
	return result
}

// linkInfo holds everything we extract from a single RTM_GETLINK response.
type linkInfo struct {
	name      string
	operState string
	ifType    string // classified type: physical, vpn, vlan, ppp, loopback, span
	encapType string // ARPHRD text form: "ether", "loopback", "none", "ppp", etc.
	linkKind  string // IFLA_INFO_KIND: wireguard, vlan, bridge, bond, gre, ...
	stats     *rawStat
	addrs     []string
}

const addrCacheTTL = 10 * time.Second

func (c *Collector) poll() {
	var links []vnl.Link
	var err error
	if c.nlHandle != nil {
		links, err = c.nlHandle.LinkList()
	} else {
		links, err = vnl.LinkList()
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "collector: netlink LinkList: %v\n", err)
		return
	}

	// Refresh address cache every 10 seconds (addresses rarely change)
	if time.Since(c.addrCacheTime) >= addrCacheTTL {
		var allAddrs []vnl.Addr
		if c.nlHandle != nil {
			allAddrs, err = c.nlHandle.AddrList(nil, vnl.FAMILY_ALL)
		} else {
			allAddrs, err = vnl.AddrList(nil, vnl.FAMILY_ALL)
		}
		if err != nil {
			fmt.Fprintf(os.Stderr, "collector: netlink AddrList: %v\n", err)
		} else {
			addrsByIndex := make(map[int][]string)
			for _, a := range allAddrs {
				addrsByIndex[a.LinkIndex] = append(addrsByIndex[a.LinkIndex], a.IPNet.String())
			}
			c.addrCache = addrsByIndex
			c.addrCacheTime = time.Now()
		}
	}

	// Build linkInfo map from netlink data
	infos := make(map[string]*linkInfo, len(links))
	for _, link := range links {
		attrs := link.Attrs()
		if attrs == nil {
			continue
		}
		name := attrs.Name

		li := &linkInfo{
			name:      name,
			operState: operStateStr(attrs.OperState),
			encapType: attrs.EncapType,
			addrs:     c.addrCache[attrs.Index],
		}

		// Extract IFLA_INFO_KIND via link type name
		li.linkKind = link.Type()

		// Get stats from IFLA_STATS64 (vishvananda populates attrs.Statistics)
		if s := attrs.Statistics; s != nil {
			li.stats = &rawStat{
				rxBytes:   s.RxBytes,
				txBytes:   s.TxBytes,
				rxPackets: s.RxPackets,
				txPackets: s.TxPackets,
				rxErrors:  s.RxErrors,
				txErrors:  s.TxErrors,
				rxDropped: s.RxDropped,
				txDropped: s.TxDropped,
			}
		} else {
			li.stats = &rawStat{}
		}

		// Classify interface type
		li.ifType = c.classifyLink(li)

		infos[name] = li
	}

	// Read VPN status files outside the lock (file I/O)
	vpnState := make(map[string]struct {
		routing bool
		since   string
	})
	for name := range infos {
		routing, since := c.checkVPNRouting(name)
		vpnState[name] = struct {
			routing bool
			since   string
		}{routing, since}
	}

	now := time.Now()
	c.mu.Lock()
	defer c.mu.Unlock()

	// Remove interfaces that no longer exist
	for name := range c.current {
		if _, exists := infos[name]; !exists {
			delete(c.current, name)
			delete(c.previous, name)
		}
	}

	for name, li := range infos {
		cur := li.stats
		prev, hasPrev := c.previous[name]
		vs := vpnState[name]
		_, vpnTracked := c.vpnStatusFiles[name]
		iface := &InterfaceStat{
			Name:            name,
			IfaceType:       li.ifType,
			OperState:       li.operState,
			Addrs:           li.addrs,
			VPNRouting:      vs.routing,
			VPNRoutingSince: vs.since,
			VPNTracked:      vpnTracked,
			RxBytes:         cur.rxBytes,
			TxBytes:         cur.txBytes,
			RxPackets:       cur.rxPackets,
			TxPackets:       cur.txPackets,
			RxErrors:        cur.rxErrors,
			TxErrors:        cur.txErrors,
			RxDropped:       cur.rxDropped,
			TxDropped:       cur.txDropped,
			Timestamp:       now.UnixMilli(),
		}

		if hasPrev {
			dt := now.Sub(prev.ts).Seconds()
			if dt > 0 {
				iface.RxRate = float64(cur.rxBytes-prev.rxBytes) / dt
				iface.TxRate = float64(cur.txBytes-prev.txBytes) / dt
			}
		}

		// SPAN overlay: override RX/TX with direction-aware pcap data
		if c.span != nil && name == c.span.device {
			rxB, txB, rxP, txP := c.span.snapshot()
			iface.RxBytes = rxB
			iface.TxBytes = txB
			iface.RxPackets = rxP
			iface.TxPackets = txP
			iface.IfaceType = "span"
			if c.spanHasPrev {
				dt := now.Sub(prev.ts).Seconds()
				if dt > 0 {
					iface.RxRate = float64(rxB-c.spanPrevRx) / dt
					iface.TxRate = float64(txB-c.spanPrevTx) / dt
				}
			}
			c.spanPrevRx = rxB
			c.spanPrevTx = txB
			c.spanHasPrev = true
		}

		c.current[name] = iface
		cur.ts = now
		c.previous[name] = cur

		if hasPrev {
			c.history[name] = append(c.history[name], HistoryPoint{
				Timestamp: now.UnixMilli(),
				RxRate:    iface.RxRate,
				TxRate:    iface.TxRate,
			})
			if len(c.history[name]) > historyPruneAt {
				cutoff := now.Add(-historyMaxAge).UnixMilli()
				idx := 0
				for idx < len(c.history[name]) && c.history[name][idx].Timestamp < cutoff {
					idx++
				}
				c.history[name] = c.history[name][idx:]
			}
		}
	}
}

// classifyLink determines the interface category from netlink data.
// Uses IFLA_INFO_KIND (linkKind), ARPHRD type (arphrd), and name-based
// heuristics as fallback. Returns one of:
// "physical", "vlan", "ppp", "vpn", "loopback".
func (c *Collector) classifyLink(li *linkInfo) string {
	// Check cache first
	if t, ok := c.ifaceTypeCache[li.name]; ok {
		return t
	}

	t := classifyLinkUncached(li)
	c.ifaceTypeCache[li.name] = t
	return t
}

func classifyLinkUncached(li *linkInfo) string {
	// 1. IFLA_INFO_KIND — the most reliable classifier
	switch li.linkKind {
	case "wireguard":
		return "vpn"
	case "vlan":
		return "vlan"
	case "bridge":
		return "physical" // group bridges with physical
	case "bond":
		return "physical" // group bonds with physical
	case "ppp", "pppoe":
		return "ppp"
	case "gre", "gretap", "ip6gre", "ip6gretap", "ip6tnl", "ipip", "sit", "vti", "vti6":
		return "vpn"
	case "tun", "tap":
		return "vpn"
	case "vxlan", "geneve":
		return "vpn"
	}

	// 2. ARPHRD encap type (string form from netlink)
	switch li.encapType {
	case "loopback":
		return "loopback"
	case "none": // ARPHRD_NONE — common for WireGuard and tunnels
		return "vpn"
	case "ppp":
		return "ppp"
	}

	// 3. Name-based fallback
	n := strings.ToLower(li.name)
	if strings.HasPrefix(n, "tun") || strings.HasPrefix(n, "tap") {
		return "vpn"
	}
	if strings.HasPrefix(n, "ppp") || strings.HasPrefix(n, "wwan") || strings.HasPrefix(n, "lte") {
		return "ppp"
	}
	if strings.Contains(n, ".") {
		return "vlan"
	}

	return "physical"
}

// operStateStr converts a netlink OperState to a human-readable string.
func operStateStr(state vnl.LinkOperState) string {
	switch state {
	case vnl.OperUp:
		return "up"
	case vnl.OperDown:
		return "down"
	case vnl.OperLowerLayerDown:
		return "lowerlayerdown"
	case vnl.OperTesting:
		return "testing"
	case vnl.OperDormant:
		return "dormant"
	case vnl.OperNotPresent:
		return "notpresent"
	default:
		return "unknown"
	}
}

// checkVPNRouting checks whether traffic is actively routed through a VPN interface
// by looking for a sentinel file configured via VPN_STATUS_FILES.
func (c *Collector) checkVPNRouting(name string) (bool, string) {
	path, ok := c.vpnStatusFiles[name]
	if !ok {
		return false, ""
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return false, ""
	}
	return true, strings.TrimSpace(string(data))
}
