package main

import (
	"embed"
	"fmt"
	"io/fs"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"bandwidth-monitor/adguard"
	"bandwidth-monitor/collector"
	"bandwidth-monitor/dns"
	"bandwidth-monitor/geoip"
	"bandwidth-monitor/handler"
	"bandwidth-monitor/nextdns"
	"bandwidth-monitor/talkers"
	"bandwidth-monitor/unifi"
)

//go:embed static/*
var staticFiles embed.FS

// env returns the value of the environment variable named by key,
// or fallback if the variable is empty/unset.
func env(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func main() {
	listenAddr := env("LISTEN", ":8080")
	captureDevice := env("DEVICE", "")
	promiscuous := env("PROMISCUOUS", "true")
	promiscuousBool, _ := strconv.ParseBool(promiscuous)

	// Parse LOCAL_NETS: comma-separated CIDRs for SPAN port direction detection
	// e.g. LOCAL_NETS=192.0.2.0/24,2001:db8::/48
	// If not set, auto-discovers from local interface addresses.
	var localNets []*net.IPNet
	if raw := os.Getenv("LOCAL_NETS"); raw != "" {
		for _, cidr := range strings.Split(raw, ",") {
			cidr = strings.TrimSpace(cidr)
			if cidr == "" {
				continue
			}
			_, ipnet, err := net.ParseCIDR(cidr)
			if err != nil {
				log.Printf("LOCAL_NETS: invalid CIDR %q: %v", cidr, err)
				continue
			}
			localNets = append(localNets, ipnet)
		}
		log.Printf("LOCAL_NETS: %d network(s) from configuration", len(localNets))
	} else {
		// Auto-discover from local interfaces
		ifaces, err := net.Interfaces()
		if err == nil {
			for _, iface := range ifaces {
				if iface.Flags&net.FlagLoopback != 0 {
					continue
				}
				addrs, err := iface.Addrs()
				if err != nil {
					continue
				}
				for _, addr := range addrs {
					ipnet, ok := addr.(*net.IPNet)
					if !ok {
						continue
					}
					// Skip link-local
					if ipnet.IP.IsLinkLocalUnicast() || ipnet.IP.IsLinkLocalMulticast() {
						continue
					}
					localNets = append(localNets, ipnet)
				}
			}
		}
		if len(localNets) > 0 {
			log.Printf("LOCAL_NETS: auto-discovered %d network(s) from interfaces", len(localNets))
			for _, n := range localNets {
				log.Printf("  %s", n.String())
			}
		}
	}

	geoCountry := env("GEO_COUNTRY", "GeoLite2-Country.mmdb")
	geoASN := env("GEO_ASN", "GeoLite2-ASN.mmdb")
	adguardURL := env("ADGUARD_URL", "")
	adguardUser := env("ADGUARD_USER", "")
	adguardPass := env("ADGUARD_PASS", "")
	nextdnsProfile := env("NEXTDNS_PROFILE", "")
	nextdnsAPIKey := env("NEXTDNS_API_KEY", "")
	unifiURL := env("UNIFI_URL", "")
	unifiUser := env("UNIFI_USER", "")
	unifiPass := env("UNIFI_PASS", "")
	unifiSite := env("UNIFI_SITE", "default")

	geoDB, err := geoip.Open(geoCountry, geoASN)
	if err != nil {
		log.Printf("GeoIP: %v (continuing without geo)", err)
		geoDB = nil
	} else if geoDB.Available() {
		log.Println("GeoIP databases loaded")
		defer geoDB.Close()
	} else {
		log.Println("GeoIP: no MMDB files found (continuing without geo)")
	}

	statsCollector := collector.New(captureDevice, promiscuousBool, localNets)
	go statsCollector.Run()

	talkerTracker := talkers.New(captureDevice, promiscuousBool, localNets, geoDB)
	go talkerTracker.Run()

	// DNS provider: AdGuard Home or NextDNS (mutually exclusive, AdGuard takes priority)
	var dnsProvider dns.Provider
	if adguardURL != "" {
		ac := adguard.New(adguardURL, adguardUser, adguardPass, 10*time.Second)
		go ac.Run()
		dnsProvider = ac
		log.Printf("DNS integration: AdGuard Home (%s)", adguardURL)
	} else if nextdnsProfile != "" && nextdnsAPIKey != "" {
		nc := nextdns.New(nextdnsProfile, nextdnsAPIKey, 30*time.Second)
		go nc.Run()
		dnsProvider = nc
		log.Printf("DNS integration: NextDNS (profile %s)", nextdnsProfile)
	}

	var unifiClient *unifi.Client
	if unifiURL != "" {
		unifiClient = unifi.New(unifiURL, unifiUser, unifiPass, unifiSite, 15*time.Second)
		go unifiClient.Run()
		log.Printf("UniFi controller integration enabled: %s", unifiURL)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/api/interfaces", handler.InterfaceStats(statsCollector))
	mux.HandleFunc("/api/interfaces/history", handler.InterfaceHistory(statsCollector))
	mux.HandleFunc("/api/talkers/bandwidth", handler.TopTalkersBandwidth(talkerTracker))
	mux.HandleFunc("/api/talkers/volume", handler.TopTalkersVolume(talkerTracker))
	mux.HandleFunc("/api/dns", handler.DNSSummary(dnsProvider))
	mux.HandleFunc("/api/wifi", handler.WiFiSummary(unifiClient))
	mux.HandleFunc("/api/summary", handler.MenuBarSummary(statsCollector, talkerTracker, dnsProvider, unifiClient))
	mux.HandleFunc("/api/ws", handler.WebSocket(statsCollector, talkerTracker, dnsProvider, unifiClient))
	staticSub, err := fs.Sub(staticFiles, "static")
	if err != nil {
		log.Fatalf("Failed to create sub filesystem: %v", err)
	}
	mux.Handle("/", http.FileServer(http.FS(staticSub)))

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		fmt.Println("\nShutting down...")
		statsCollector.Stop()
		talkerTracker.Stop()
		if dnsProvider != nil {
			dnsProvider.Stop()
		}
		if unifiClient != nil {
			unifiClient.Stop()
		}
		os.Exit(0)
	}()

	log.Printf("Bandwidth Monitor starting on %s", listenAddr)
	log.Printf("Open http://localhost%s in your browser", listenAddr)
	if err := http.ListenAndServe(listenAddr, mux); err != nil {
		log.Fatalf("Server failed: %v", err)
	}
}
