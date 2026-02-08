package collector

import (
	"bufio"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
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
	return &Collector{
		current:        make(map[string]*InterfaceStat),
		previous:       make(map[string]*rawStat),
		history:        make(map[string][]HistoryPoint),
		ifaceTypeCache: make(map[string]string),
		vpnStatusFiles: vpnStatusFiles,
		stopCh:         make(chan struct{}),
	}
}

func (c *Collector) Run() {
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
	c.mu.RLock()
	defer c.mu.RUnlock()
	result := make(map[string][]HistoryPoint, len(c.history))
	for k, v := range c.history {
		cp := make([]HistoryPoint, len(v))
		copy(cp, v)
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

func (c *Collector) poll() {
	// Auto-discovers all interfaces by reading /proc/net/dev
	stats, err := readProcNetDev()
	if err != nil {
		fmt.Fprintf(os.Stderr, "collector: %v\n", err)
		return
	}

	now := time.Now()
	c.mu.Lock()
	defer c.mu.Unlock()

	// Remove interfaces that no longer exist in /proc/net/dev
	for name := range c.current {
		if _, exists := stats[name]; !exists {
			delete(c.current, name)
			delete(c.previous, name)
		}
	}

	for name, cur := range stats {
		prev, hasPrev := c.previous[name]
		ifType := c.getIfaceType(name)
		vpnRouting, vpnSince := c.checkVPNRouting(name)
		_, vpnTracked := c.vpnStatusFiles[name]
		iface := &InterfaceStat{
			Name:            name,
			IfaceType:       ifType,
			OperState:       readOperState(name),
			Addrs:           getIfaceAddrs(name),
			VPNRouting:      vpnRouting,
			VPNRoutingSince: vpnSince,
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

// readProcNetDev parses /proc/net/dev and returns stats for every interface.
func readProcNetDev() (map[string]*rawStat, error) {
	f, err := os.Open("/proc/net/dev")
	if err != nil {
		return nil, err
	}
	defer f.Close()

	result := make(map[string]*rawStat)
	scanner := bufio.NewScanner(f)
	lineNum := 0
	for scanner.Scan() {
		lineNum++
		if lineNum <= 2 {
			continue // skip header lines
		}
		line := scanner.Text()
		colonIdx := strings.Index(line, ":")
		if colonIdx < 0 {
			continue
		}
		name := strings.TrimSpace(line[:colonIdx])
		fields := strings.Fields(line[colonIdx+1:])
		if len(fields) < 16 {
			continue
		}

		stat := &rawStat{
			rxBytes:   parseUint(fields[0]),
			rxPackets: parseUint(fields[1]),
			rxErrors:  parseUint(fields[2]),
			rxDropped: parseUint(fields[3]),
			txBytes:   parseUint(fields[8]),
			txPackets: parseUint(fields[9]),
			txErrors:  parseUint(fields[10]),
			txDropped: parseUint(fields[11]),
		}
		result[name] = stat
	}
	return result, scanner.Err()
}

func parseUint(s string) uint64 {
	v, _ := strconv.ParseUint(strings.TrimSpace(s), 10, 64)
	return v
}

// getIfaceType returns a cached interface type, detecting on first call.
func (c *Collector) getIfaceType(name string) string {
	if t, ok := c.ifaceTypeCache[name]; ok {
		return t
	}
	t := detectIfaceType(name)
	c.ifaceTypeCache[name] = t
	return t
}

// readOperState reads the operational state from sysfs.
func readOperState(name string) string {
	data, err := os.ReadFile("/sys/class/net/" + name + "/operstate")
	if err != nil {
		return "unknown"
	}
	return strings.TrimSpace(string(data))
}

// getIfaceAddrs returns the IP addresses (with CIDR prefix) assigned to an interface.
func getIfaceAddrs(name string) []string {
	iface, err := net.InterfaceByName(name)
	if err != nil {
		return nil
	}
	addrs, err := iface.Addrs()
	if err != nil {
		return nil
	}
	var result []string
	for _, a := range addrs {
		result = append(result, a.String())
	}
	return result
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

// detectIfaceType determines the interface category by inspecting sysfs and
// falling back to name-based heuristics. Returns one of:
// "physical", "vlan", "ppp", "vpn", "bridge", "bond", "loopback".
func detectIfaceType(name string) string {
	base := "/sys/class/net/" + name

	// 1. Check if WireGuard by looking for the wireguard sysfs directory
	if _, err := os.Stat(filepath.Join(base, "wireguard")); err == nil {
		return "vpn"
	}

	// 2. Read uevent for DEVTYPE
	if data, err := os.ReadFile(filepath.Join(base, "uevent")); err == nil {
		for _, line := range strings.Split(string(data), "\n") {
			if strings.HasPrefix(line, "DEVTYPE=") {
				dt := strings.TrimPrefix(line, "DEVTYPE=")
				switch dt {
				case "vlan":
					return "vlan"
				case "bridge":
					return "physical" // group bridges with physical
				case "bond":
					return "physical" // group bonds with physical
				case "ppp":
					return "ppp"
				case "wireguard":
					return "vpn"
				case "gre", "gretap", "ip6gre", "ip6tnl", "ipip", "sit", "vti":
					return "vpn"
				}
			}
		}
	}

	// 3. Check /sys/class/net/<name>/type (ARPHRD_* constants)
	if data, err := os.ReadFile(filepath.Join(base, "type")); err == nil {
		ifType := strings.TrimSpace(string(data))
		switch ifType {
		case "772": // ARPHRD_LOOPBACK
			return "loopback"
		case "65534": // ARPHRD_NONE — common for WireGuard and tunnels
			// Already checked for WireGuard above; remaining NONE types are tunnels
			return "vpn"
		case "512": // ARPHRD_PPP
			return "ppp"
		}
	}

	// 4. Name-based fallback
	n := strings.ToLower(name)
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
