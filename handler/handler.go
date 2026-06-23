package handler

import (
	"encoding/json"
	"log"
	"net/http"
	"strconv"
	"time"

	"bandwidth-monitor/collector"
	"bandwidth-monitor/conntrack"
	"bandwidth-monitor/dns"
	"bandwidth-monitor/talkers"
	"bandwidth-monitor/unifi"

	"github.com/gorilla/websocket"
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

func InterfaceStats(c *collector.Collector) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(c.GetAll())
	}
}

func InterfaceHistory(c *collector.Collector) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		// Optional ?minutes=N limits the returned window (default: full 24h).
		var window time.Duration
		if m := r.URL.Query().Get("minutes"); m != "" {
			if n, err := strconv.Atoi(m); err == nil && n > 0 {
				window = time.Duration(n) * time.Minute
			}
		}
		json.NewEncoder(w).Encode(c.GetHistoryWindow(window))
	}
}

func TopTalkersBandwidth(t *talkers.Tracker) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(t.TopByBandwidth(10))
	}
}

func TopTalkersVolume(t *talkers.Tracker) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(t.TopByVolume(10))
	}
}

func DNSSummary(dp dns.Provider) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if dp == nil {
			w.Write([]byte("null"))
			return
		}
		json.NewEncoder(w).Encode(dp.GetSummary())
	}
}

func WiFiSummary(uf *unifi.Client) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if uf == nil {
			w.Write([]byte("null"))
			return
		}
		json.NewEncoder(w).Encode(uf.GetSummary())
	}
}

func ConntrackSummary(ct *conntrack.Tracker) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if ct == nil {
			w.Write([]byte("null"))
			return
		}
		json.NewEncoder(w).Encode(ct.GetSummary())
	}
}

// MenuBarSummary returns a compact JSON snapshot for menu-bar widgets.
func MenuBarSummary(c *collector.Collector, t *talkers.Tracker, dp dns.Provider, uf *unifi.Client, ctr *conntrack.Tracker) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		type ifaceBrief struct {
			Name   string  `json:"name"`
			Type   string  `json:"type"`
			RxRate float64 `json:"rx_rate"`
			TxRate float64 `json:"tx_rate"`
			State  string  `json:"state"`
		}
		type dnsBrief struct {
			Provider     string  `json:"provider_name"`
			TotalQueries int     `json:"total_queries"`
			Blocked      int     `json:"blocked"`
			BlockPct     float64 `json:"block_pct"`
			LatencyMs    float64 `json:"latency_ms"`
		}
		type wifiBrief struct {
			APs     int `json:"aps"`
			Clients int `json:"clients"`
		}
		type natBrief struct {
			Total    int     `json:"total"`
			Max      int     `json:"max"`
			UsagePct float64 `json:"usage_pct"`
			IPv4     int     `json:"ipv4"`
			IPv6     int     `json:"ipv6"`
			SNAT     int     `json:"snat"`
			DNAT     int     `json:"dnat"`
		}
		type summary struct {
			App        string       `json:"app"`
			Interfaces []ifaceBrief `json:"interfaces"`
			VPN        bool         `json:"vpn"`
			VPNIface   string       `json:"vpn_iface,omitempty"`
			DNS        *dnsBrief    `json:"dns,omitempty"`
			WiFi       *wifiBrief   `json:"wifi,omitempty"`
			NAT        *natBrief    `json:"nat,omitempty"`
			Timestamp  int64        `json:"timestamp"`
		}

		var out summary
		out.App = "bandwidth-monitor"
		out.Timestamp = time.Now().UnixMilli()

		for _, iface := range c.GetAll() {
			out.Interfaces = append(out.Interfaces, ifaceBrief{
				Name:   iface.Name,
				Type:   iface.IfaceType,
				RxRate: iface.RxRate,
				TxRate: iface.TxRate,
				State:  iface.OperState,
			})
			if iface.VPNRouting {
				out.VPN = true
				out.VPNIface = iface.Name
			}
		}
		if dp != nil {
			if ds := dp.GetSummary(); ds != nil {
				out.DNS = &dnsBrief{
					Provider:     ds.ProviderName,
					TotalQueries: ds.TotalQueries,
					Blocked:      ds.BlockedTotal,
					BlockPct:     ds.BlockedPercent,
					LatencyMs:    ds.AvgLatencyMs,
				}
			}
		}
		if uf != nil {
			if ws := uf.GetSummary(); ws != nil {
				totalClients := 0
				for _, ap := range ws.APs {
					totalClients += ap.NumClients
				}
				out.WiFi = &wifiBrief{
					APs:     len(ws.APs),
					Clients: totalClients,
				}
			}
		}
		if ctr != nil {
			if ns := ctr.GetSummary(); ns != nil {
				out.NAT = &natBrief{
					Total:    ns.Total,
					Max:      ns.Max,
					UsagePct: ns.UsagePct,
					IPv4:     ns.IPv4,
					IPv6:     ns.IPv6,
					SNAT:     ns.NATTypes["snat"] + ns.NATTypes["both"],
					DNAT:     ns.NATTypes["dnat"] + ns.NATTypes["both"],
				}
			}
		}

		json.NewEncoder(w).Encode(out)
	}
}

func WebSocket(c *collector.Collector, t *talkers.Tracker, dp dns.Provider, uf *unifi.Client, ct *conntrack.Tracker) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			log.Printf("websocket upgrade: %v", err)
			return
		}
		defer conn.Close()

		// Read pump — drain incoming messages so the connection
		// can process control frames (close, ping/pong).
		doneCh := make(chan struct{})
		go func() {
			defer close(doneCh)
			conn.SetReadDeadline(time.Now().Add(60 * time.Second))
			conn.SetPongHandler(func(string) error {
				conn.SetReadDeadline(time.Now().Add(60 * time.Second))
				return nil
			})
			for {
				if _, _, err := conn.ReadMessage(); err != nil {
					return
				}
			}
		}()

		ticker := time.NewTicker(1 * time.Second)
		defer ticker.Stop()

		pingTicker := time.NewTicker(30 * time.Second)
		defer pingTicker.Stop()

		// All writes go through sendCh to avoid concurrent writes on the
		// websocket connection (gorilla/websocket is not safe for concurrent writers).
		// A nil payload signals a ping; a non-nil payload is a JSON message.
		sendCh := make(chan map[string]interface{}, 1)

		// Writer goroutine — serialises all writes to the connection.
		writerDone := make(chan struct{})
		go func() {
			defer close(writerDone)
			for msg := range sendCh {
				if msg == nil {
					// Ping
					conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
					if err := conn.WriteMessage(websocket.PingMessage, nil); err != nil {
						return
					}
				} else {
					conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
					if err := conn.WriteJSON(msg); err != nil {
						return
					}
				}
			}
		}()

		for {
			select {
			case <-doneCh:
				close(sendCh)
				return
			case <-writerDone:
				return
			case <-pingTicker.C:
				// Route ping through writer goroutine to avoid concurrent writes
				select {
				case sendCh <- nil:
				default:
					// Writer is backed up — skip ping, the write timeout will catch dead connections
				}
			case <-ticker.C:
				payload := map[string]interface{}{
					"interfaces":    c.GetAll(),
					"sparklines":    c.GetSparklines(5*time.Minute, 50),
					"protocols":     t.GetProtocolBreakdown(),
					"ip_versions":   t.GetIPVersionBreakdown(),
					"countries":     t.GetCountryBreakdown(),
					"asns":          t.GetASNBreakdown(),
					"top_bandwidth": t.TopByBandwidth(10),
					"top_volume":    t.TopByVolume(10),
					"timestamp":     time.Now().UnixMilli(),
				}
				if dp != nil {
					payload["dns"] = dp.GetSummary()
				}
				if uf != nil {
					payload["wifi"] = uf.GetSummary()
				}
				if ct != nil {
					if s := ct.GetSummary(); s != nil {
						payload["conntrack"] = s
					}
				}
				// Non-blocking send: drop the old message if backed up
				select {
				case sendCh <- payload:
				default:
					// Channel full — drain stale message, enqueue fresh one
					select {
					case <-sendCh:
					default:
					}
					sendCh <- payload
				}
			}
		}
	}
}
