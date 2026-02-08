package nextdns

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"sort"
	"sync"
	"time"

	"bandwidth-monitor/dns"
)

const apiBase = "https://api.nextdns.io"

// Client polls the NextDNS analytics API.
type Client struct {
	profile  string
	apiKey   string
	interval time.Duration

	mu    sync.RWMutex
	stats *snapshot

	stopCh chan struct{}
}

type snapshot struct {
	status   []statusEntry
	domains  []domainEntry
	blocked  []domainEntry
	clients  []clientEntry
	statusTS []statusTSEntry
}

type statusEntry struct {
	Status  string `json:"status"`
	Queries int    `json:"queries"`
}

type domainEntry struct {
	Domain  string `json:"domain"`
	Queries int    `json:"queries"`
}

type clientEntry struct {
	IP      string `json:"ip"`
	Queries int    `json:"queries"`
}

type statusTSEntry struct {
	Status  string `json:"status"`
	Queries []int  `json:"queries"`
}

// New creates a NextDNS API client.
func New(profile, apiKey string, pollInterval time.Duration) *Client {
	return &Client{
		profile:  profile,
		apiKey:   apiKey,
		interval: pollInterval,
		stopCh:   make(chan struct{}),
	}
}

// Run starts the polling loop. Call in a goroutine.
func (c *Client) Run() {
	c.poll()
	ticker := time.NewTicker(c.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			c.poll()
		case <-c.stopCh:
			return
		}
	}
}

// Stop terminates the polling loop.
func (c *Client) Stop() {
	select {
	case <-c.stopCh:
	default:
		close(c.stopCh)
	}
}

func (c *Client) poll() {
	snap := &snapshot{}
	var ok bool

	if snap.status, ok = fetchJSON[[]statusEntry](c, "/analytics/status?from=-24h&limit=500"); !ok {
		return
	}
	if snap.domains, ok = fetchJSON[[]domainEntry](c, "/analytics/domains?from=-24h&limit=10"); !ok {
		return
	}
	if snap.blocked, ok = fetchJSON[[]domainEntry](c, "/analytics/domains?from=-24h&status=blocked&limit=10"); !ok {
		return
	}
	if snap.clients, ok = fetchJSON[[]clientEntry](c, "/analytics/ips?from=-24h&limit=10"); !ok {
		return
	}
	if snap.statusTS, ok = fetchJSON[[]statusTSEntry](c, "/analytics/status;series?from=-24h&interval=1800"); !ok {
		return
	}

	c.mu.Lock()
	c.stats = snap
	c.mu.Unlock()
}

// GetSummary returns a frontend-friendly summary, or nil if no data yet.
func (c *Client) GetSummary() *dns.Summary {
	c.mu.RLock()
	snap := c.stats
	c.mu.RUnlock()
	if snap == nil {
		return nil
	}

	var totalQueries, blockedTotal int
	for _, s := range snap.status {
		totalQueries += s.Queries
		if s.Status == "blocked" {
			blockedTotal += s.Queries
		}
	}

	blockedPct := 0.0
	if totalQueries > 0 {
		blockedPct = float64(blockedTotal) / float64(totalQueries) * 100
	}

	var queriesSeries, blockedSeries []int
	for _, ts := range snap.statusTS {
		switch ts.Status {
		case "default", "allowed":
			if queriesSeries == nil {
				queriesSeries = make([]int, len(ts.Queries))
			}
			for i, v := range ts.Queries {
				if i < len(queriesSeries) {
					queriesSeries[i] += v
				}
			}
		case "blocked":
			blockedSeries = ts.Queries
		}
	}
	if blockedSeries != nil && queriesSeries != nil {
		for i, v := range blockedSeries {
			if i < len(queriesSeries) {
				queriesSeries[i] += v
			}
		}
	}

	return &dns.Summary{
		ProviderName:   "NextDNS",
		TotalQueries:   totalQueries,
		BlockedTotal:   blockedTotal,
		BlockedPercent: blockedPct,
		AvgLatencyMs:   0,
		TopQueried:     toDomainStats(snap.domains),
		TopBlocked:     toDomainStats(snap.blocked),
		TopClients:     toClientStats(snap.clients),
		QueriesSeries:  queriesSeries,
		BlockedSeries:  blockedSeries,
		TimeUnits:      "hours",
	}
}

// Available returns true if the client has fetched data at least once.
func (c *Client) Available() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.stats != nil
}

// String returns a debug string.
func (c *Client) String() string {
	return fmt.Sprintf("NextDNS[profile=%s]", c.profile)
}

type apiResponse[T any] struct {
	Data T `json:"data"`
}

func fetchJSON[T any](c *Client, path string) (T, bool) {
	var zero T
	url := fmt.Sprintf("%s/profiles/%s%s", apiBase, c.profile, path)
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		log.Printf("nextdns: build request: %v", err)
		return zero, false
	}
	req.Header.Set("X-Api-Key", c.apiKey)

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		log.Printf("nextdns: fetch %s: %v", path, err)
		return zero, false
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		log.Printf("nextdns: %s returned %d: %s", path, resp.StatusCode, string(body))
		return zero, false
	}

	var r apiResponse[T]
	if err := json.NewDecoder(resp.Body).Decode(&r); err != nil {
		log.Printf("nextdns: decode %s: %v", path, err)
		return zero, false
	}
	return r.Data, true
}

func toDomainStats(entries []domainEntry) []dns.DomainStat {
	out := make([]dns.DomainStat, len(entries))
	for i, e := range entries {
		out[i] = dns.DomainStat{Domain: e.Domain, Count: e.Queries}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Count > out[j].Count })
	return out
}

func toClientStats(entries []clientEntry) []dns.ClientStat {
	out := make([]dns.ClientStat, len(entries))
	for i, e := range entries {
		out[i] = dns.ClientStat{IP: e.IP, Count: e.Queries}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Count > out[j].Count })
	return out
}
