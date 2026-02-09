package collector

import (
	"fmt"
	"net"
	"os"
	"sync"
	"time"

	"github.com/gopacket/gopacket"
	"github.com/gopacket/gopacket/layers"
	"github.com/gopacket/gopacket/pcap"
)

const (
	snapshotLen    int32 = 128 // only need IP headers for direction + length
	capTimeout           = 100 * time.Millisecond
	rateInterval         = 1 * time.Second
	historyMaxAge        = 24 * time.Hour
	historyPruneAt       = 86400
)

// InterfaceStat reports aggregate bandwidth seen on the SPAN interface,
// classified as RX (remote → LOCAL_NETS) and TX (LOCAL_NETS → remote).
type InterfaceStat struct {
	Name      string   `json:"name"`
	IfaceType string   `json:"iface_type"`
	OperState string   `json:"oper_state"`
	Addrs     []string `json:"addrs,omitempty"`
	RxBytes   uint64   `json:"rx_bytes"`
	TxBytes   uint64   `json:"tx_bytes"`
	RxPackets uint64   `json:"rx_packets"`
	TxPackets uint64   `json:"tx_packets"`
	RxRate    float64  `json:"rx_rate"` // bytes/sec download
	TxRate    float64  `json:"tx_rate"` // bytes/sec upload
	Timestamp int64    `json:"timestamp"`
}

// HistoryPoint stores a single rate sample for the 24-hour history ring.
type HistoryPoint struct {
	Timestamp int64   `json:"t"`
	RxRate    float64 `json:"rx"`
	TxRate    float64 `json:"tx"`
}

// SparkPoint is a lightweight rate pair for sparkline rendering.
type SparkPoint struct {
	RX float64 `json:"rx"`
	TX float64 `json:"tx"`
}

// Collector captures packets on a SPAN/mirror port and classifies
// traffic direction using LOCAL_NETS, replacing the /proc/net/dev approach.
type Collector struct {
	device      string
	promiscuous bool
	localNets   []*net.IPNet

	mu      sync.RWMutex
	stat    InterfaceStat
	history []HistoryPoint

	// Packet-level accumulators (protected by accMu, updated per-packet)
	accMu     sync.Mutex
	rxBytes   uint64
	txBytes   uint64
	rxPackets uint64
	txPackets uint64

	stopCh chan struct{}
}

// New creates a Collector that sniffs the SPAN device and classifies each
// packet as download (RX) or upload (TX) based on whether the destination
// or source IP falls within the supplied localNets CIDRs.
func New(device string, promiscuous bool, localNets []*net.IPNet) *Collector {
	return &Collector{
		device:      device,
		promiscuous: promiscuous,
		localNets:   localNets,
		stat: InterfaceStat{
			Name:      device,
			IfaceType: "span",
			OperState: "up",
		},
		history: make([]HistoryPoint, 0, historyPruneAt),
		stopCh:  make(chan struct{}),
	}
}

// Run opens the capture device and begins classifying packets.
// It blocks until Stop() is called; intended to be launched as a goroutine.
func (c *Collector) Run() {
	if c.device == "" {
		fmt.Fprintln(os.Stderr, "collector: DEVICE not set — bandwidth collection disabled")
		return
	}
	if len(c.localNets) == 0 {
		fmt.Fprintln(os.Stderr, "collector: LOCAL_NETS not set — cannot determine traffic direction")
		return
	}

	handle, err := pcap.OpenLive(c.device, snapshotLen, c.promiscuous, capTimeout)
	if err != nil {
		fmt.Fprintf(os.Stderr, "collector: cannot open %s: %v\n", c.device, err)
		fmt.Fprintln(os.Stderr, "collector: pcap requires root or CAP_NET_RAW")
		return
	}
	defer handle.Close()

	if err := handle.SetBPFFilter("ip or ip6"); err != nil {
		fmt.Fprintf(os.Stderr, "collector: BPF filter error: %v\n", err)
	}

	fmt.Fprintf(os.Stderr, "collector: capturing on %s (promiscuous=%v)\n", c.device, c.promiscuous)

	go c.rateLoop()

	for {
		select {
		case <-c.stopCh:
			return
		default:
		}
		data, _, err := handle.ReadPacketData()
		if err != nil {
			if err == pcap.NextErrorTimeoutExpired {
				continue
			}
			fmt.Fprintf(os.Stderr, "collector: read error on %s: %v\n", c.device, err)
			return
		}
		pkt := gopacket.NewPacket(data, handle.LinkType(), gopacket.DecodeOptions{
			Lazy:   true,
			NoCopy: true,
		})
		c.processPacket(pkt)
	}
}

// Stop signals the collector to shut down.
func (c *Collector) Stop() {
	close(c.stopCh)
}

// GetAll returns a single-element slice with the current aggregate stats.
func (c *Collector) GetAll() []InterfaceStat {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return []InterfaceStat{c.stat}
}

// GetHistory returns the 24-hour rate history keyed by device name.
func (c *Collector) GetHistory() map[string][]HistoryPoint {
	c.mu.RLock()
	defer c.mu.RUnlock()
	cp := make([]HistoryPoint, len(c.history))
	copy(cp, c.history)
	return map[string][]HistoryPoint{c.device: cp}
}

// GetSparklines returns the last `duration` of rate data, downsampled to at
// most `maxPoints` points, keyed by device name.
func (c *Collector) GetSparklines(duration time.Duration, maxPoints int) map[string][]SparkPoint {
	c.mu.RLock()
	defer c.mu.RUnlock()

	cutoff := time.Now().Add(-duration).UnixMilli()
	start := 0
	for start < len(c.history) && c.history[start].Timestamp < cutoff {
		start++
	}
	pts := c.history[start:]
	if len(pts) == 0 {
		return nil
	}

	var sp []SparkPoint
	if len(pts) <= maxPoints {
		sp = make([]SparkPoint, len(pts))
		for i, p := range pts {
			sp[i] = SparkPoint{RX: p.RxRate, TX: p.TxRate}
		}
	} else {
		sp = make([]SparkPoint, maxPoints)
		step := float64(len(pts)) / float64(maxPoints)
		for i := 0; i < maxPoints; i++ {
			idx := int(float64(i) * step)
			if idx >= len(pts) {
				idx = len(pts) - 1
			}
			sp[i] = SparkPoint{RX: pts[idx].RxRate, TX: pts[idx].TxRate}
		}
	}
	return map[string][]SparkPoint{c.device: sp}
}

// ---------- internal ----------

// processPacket classifies a single captured packet as RX or TX based
// on whether its source / destination falls within LOCAL_NETS.
func (c *Collector) processPacket(pkt gopacket.Packet) {
	var srcIP, dstIP net.IP
	var pktLen uint64

	if ipLayer := pkt.Layer(layers.LayerTypeIPv4); ipLayer != nil {
		ip := ipLayer.(*layers.IPv4)
		srcIP = ip.SrcIP
		dstIP = ip.DstIP
		pktLen = uint64(ip.Length)
	} else if ipLayer := pkt.Layer(layers.LayerTypeIPv6); ipLayer != nil {
		ip := ipLayer.(*layers.IPv6)
		srcIP = ip.SrcIP
		dstIP = ip.DstIP
		pktLen = uint64(ip.Length) + 40 // IPv6 payload length excludes header
	} else {
		return
	}

	srcLocal := c.isLocal(srcIP)
	dstLocal := c.isLocal(dstIP)

	c.accMu.Lock()
	switch {
	case srcLocal && !dstLocal:
		// LOCAL_NETS → remote = upload (TX)
		c.txBytes += pktLen
		c.txPackets++
	case !srcLocal && dstLocal:
		// remote → LOCAL_NETS = download (RX)
		c.rxBytes += pktLen
		c.rxPackets++
	case srcLocal && dstLocal:
		// intra-LAN traffic — count as both
		c.rxBytes += pktLen
		c.rxPackets++
		c.txBytes += pktLen
		c.txPackets++
	}
	// both-remote packets (shouldn't appear on a properly-filtered SPAN) are ignored
	c.accMu.Unlock()
}

// isLocal returns true when ip falls within any of the configured LOCAL_NETS.
func (c *Collector) isLocal(ip net.IP) bool {
	for _, n := range c.localNets {
		if n.Contains(ip) {
			return true
		}
	}
	return false
}

// rateLoop wakes every second, computes the delta rates, and appends a
// history point.  It also prunes history older than 24 hours.
func (c *Collector) rateLoop() {
	ticker := time.NewTicker(rateInterval)
	defer ticker.Stop()

	var prevRx, prevTx uint64
	prevTime := time.Now()

	for {
		select {
		case <-ticker.C:
			now := time.Now()
			dt := now.Sub(prevTime).Seconds()
			if dt <= 0 {
				continue
			}

			c.accMu.Lock()
			curRx := c.rxBytes
			curTx := c.txBytes
			curRxPkt := c.rxPackets
			curTxPkt := c.txPackets
			c.accMu.Unlock()

			rxRate := float64(curRx-prevRx) / dt
			txRate := float64(curTx-prevTx) / dt

			c.mu.Lock()
			c.stat = InterfaceStat{
				Name:      c.device,
				IfaceType: "span",
				OperState: "up",
				RxBytes:   curRx,
				TxBytes:   curTx,
				RxPackets: curRxPkt,
				TxPackets: curTxPkt,
				RxRate:    rxRate,
				TxRate:    txRate,
				Timestamp: now.UnixMilli(),
			}
			c.history = append(c.history, HistoryPoint{
				Timestamp: now.UnixMilli(),
				RxRate:    rxRate,
				TxRate:    txRate,
			})
			if len(c.history) > historyPruneAt {
				cutoff := now.Add(-historyMaxAge).UnixMilli()
				idx := 0
				for idx < len(c.history) && c.history[idx].Timestamp < cutoff {
					idx++
				}
				c.history = c.history[idx:]
			}
			c.mu.Unlock()

			prevRx = curRx
			prevTx = curTx
			prevTime = now

		case <-c.stopCh:
			return
		}
	}
}
