package handler

import (
	"encoding/json"
	"log"
	"net/http"
	"time"

	"bandwidth-web-go/adguard"
	"bandwidth-web-go/collector"
	"bandwidth-web-go/talkers"
	"bandwidth-web-go/unifi"

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
		json.NewEncoder(w).Encode(c.GetHistory())
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

func DNSSummary(ag *adguard.Client) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if ag == nil {
			w.Write([]byte("null"))
			return
		}
		json.NewEncoder(w).Encode(ag.GetSummary())
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

// MenuBarSummary returns a compact JSON snapshot for menu-bar widgets.
func MenuBarSummary(c *collector.Collector, t *talkers.Tracker, ag *adguard.Client, uf *unifi.Client) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		type ifaceBrief struct {
			Name   string  `json:"name"`
			RxRate float64 `json:"rx_rate"`
			TxRate float64 `json:"tx_rate"`
			State  string  `json:"state"`
		}
		type dnsBrief struct {
			TotalQueries int     `json:"total_queries"`
			Blocked      int     `json:"blocked"`
			BlockPct     float64 `json:"block_pct"`
			LatencyMs    float64 `json:"latency_ms"`
		}
		type wifiBrief struct {
			APs     int `json:"aps"`
			Clients int `json:"clients"`
		}
		type summary struct {
			Interfaces []ifaceBrief `json:"interfaces"`
			VPN        bool         `json:"vpn"`
			VPNIface   string       `json:"vpn_iface,omitempty"`
			DNS        *dnsBrief    `json:"dns,omitempty"`
			WiFi       *wifiBrief   `json:"wifi,omitempty"`
			Timestamp  int64        `json:"timestamp"`
		}

		var out summary
		out.Timestamp = time.Now().UnixMilli()

		for _, iface := range c.GetAll() {
			out.Interfaces = append(out.Interfaces, ifaceBrief{
				Name:   iface.Name,
				RxRate: iface.RxRate,
				TxRate: iface.TxRate,
				State:  iface.OperState,
			})
			if iface.VPNRouting {
				out.VPN = true
				out.VPNIface = iface.Name
			}
		}
		if ag != nil {
			if ds := ag.GetSummary(); ds != nil {
				out.DNS = &dnsBrief{
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

		json.NewEncoder(w).Encode(out)
	}
}

func WebSocket(c *collector.Collector, t *talkers.Tracker, ag *adguard.Client, uf *unifi.Client) http.HandlerFunc {
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

		for {
			select {
			case <-doneCh:
				return
			case <-pingTicker.C:
				conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
				if err := conn.WriteMessage(websocket.PingMessage, nil); err != nil {
					return
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
				if ag != nil {
					payload["dns"] = ag.GetSummary()
				}
				if uf != nil {
					payload["wifi"] = uf.GetSummary()
				}
				conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
				if err := conn.WriteJSON(payload); err != nil {
					return
				}
			}
		}
	}
}
