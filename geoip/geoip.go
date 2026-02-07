package geoip

import (
	"fmt"
	"net"
	"os"
	"sync"

	"github.com/oschwald/maxminddb-golang"
)

// Result holds the geo + ASN information for a single IP.
type Result struct {
	Country     string `json:"country"`      // ISO 3166-1 alpha-2
	CountryName string `json:"country_name"` // English name
	City        string `json:"city,omitempty"`
	ASN         uint   `json:"asn,omitempty"`
	ASOrg       string `json:"as_org,omitempty"`
}

// DB wraps the MaxMind MMDB readers with a lookup cache.
type DB struct {
	country *maxminddb.Reader
	asn     *maxminddb.Reader
	mu      sync.RWMutex
	cache   map[string]*Result
}

// cityRecord is the minimal struct for MMDB city/country lookups.
type cityRecord struct {
	Country struct {
		ISOCode string            `maxminddb:"iso_code"`
		Names   map[string]string `maxminddb:"names"`
	} `maxminddb:"country"`
	City struct {
		Names map[string]string `maxminddb:"names"`
	} `maxminddb:"city"`
}

// asnRecord is the minimal struct for MMDB ASN lookups.
type asnRecord struct {
	ASN uint   `maxminddb:"autonomous_system_number"`
	Org string `maxminddb:"autonomous_system_organization"`
}

// Open loads the MMDB files. Either or both paths may be empty — lookups
// will gracefully return partial results.
func Open(countryPath, asnPath string) (*DB, error) {
	db := &DB{
		cache: make(map[string]*Result, 4096),
	}

	if countryPath != "" {
		if _, err := os.Stat(countryPath); err == nil {
			r, err := maxminddb.Open(countryPath)
			if err != nil {
				return nil, fmt.Errorf("geoip: open country db: %w", err)
			}
			db.country = r
		}
	}

	if asnPath != "" {
		if _, err := os.Stat(asnPath); err == nil {
			r, err := maxminddb.Open(asnPath)
			if err != nil {
				return nil, fmt.Errorf("geoip: open ASN db: %w", err)
			}
			db.asn = r
		}
	}

	return db, nil
}

// Close releases the database readers.
func (db *DB) Close() {
	if db.country != nil {
		db.country.Close()
	}
	if db.asn != nil {
		db.asn.Close()
	}
}

// Available returns true if at least one database was loaded.
func (db *DB) Available() bool {
	return db.country != nil || db.asn != nil
}

// Lookup returns geo information for an IP address. Results are cached.
func (db *DB) Lookup(ipStr string) *Result {
	if db == nil || !db.Available() {
		return nil
	}

	db.mu.RLock()
	if r, ok := db.cache[ipStr]; ok {
		db.mu.RUnlock()
		return r
	}
	db.mu.RUnlock()

	ip := net.ParseIP(ipStr)
	if ip == nil {
		return nil
	}

	r := &Result{}

	if db.country != nil {
		var rec cityRecord
		if err := db.country.Lookup(ip, &rec); err == nil {
			r.Country = rec.Country.ISOCode
			r.CountryName = rec.Country.Names["en"]
			if name, ok := rec.City.Names["en"]; ok {
				r.City = name
			}
		}
	}

	if db.asn != nil {
		var rec asnRecord
		if err := db.asn.Lookup(ip, &rec); err == nil {
			r.ASN = rec.ASN
			r.ASOrg = rec.Org
		}
	}

	db.mu.Lock()
	db.cache[ipStr] = r
	db.mu.Unlock()

	return r
}
