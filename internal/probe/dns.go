package probe

import (
	"context"
	"fmt"
	"math/rand"
	"net"
	"sort"
	"sync"
	"time"
)

// DNSBenchmark captures cached and uncached lookup latency for a single DNS resolver.
type DNSBenchmark struct {
	Name       string  `json:"name"`
	Server     string  `json:"server"`
	CachedMs   float64 `json:"cached_ms"`
	UncachedMs float64 `json:"uncached_ms"`
	Failed     bool    `json:"failed"`
	IsSystem   bool    `json:"is_system"`
}

// DNSRaceResult holds the full DNS comparison across common resolvers and the active system resolver.
type DNSRaceResult struct {
	Timestamp time.Time      `json:"timestamp"`
	Entries   []DNSBenchmark `json:"entries"`
	Fastest   DNSBenchmark   `json:"fastest"`
	System    DNSBenchmark   `json:"system"`
}

// RunDNSRace compares System DNS, Cloudflare, Quad9, Google, and the local router in parallel.
func RunDNSRace(ctx context.Context, gateway string) DNSRaceResult {
	type target struct {
		name     string
		server   string
		isSystem bool
	}
	targets := []target{
		{name: "System DNS", server: "system", isSystem: true},
		{name: "Cloudflare", server: "1.1.1.1"},
		{name: "Quad9", server: "9.9.9.9"},
		{name: "Google", server: "8.8.8.8"},
	}
	if gateway != "" {
		targets = append(targets, target{name: "Router", server: gateway})
	}

	results := make([]DNSBenchmark, len(targets))
	var wg sync.WaitGroup
	for i, t := range targets {
		wg.Add(1)
		go func(idx int, tgt target) {
			defer wg.Done()
			results[idx] = benchmarkResolver(ctx, tgt.name, tgt.server, tgt.isSystem)
		}(i, t)
	}
	wg.Wait()

	var sys DNSBenchmark
	for _, r := range results {
		if r.IsSystem {
			sys = r
		}
	}

	sort.Slice(results, func(i, j int) bool {
		if results[i].Failed != results[j].Failed {
			return !results[i].Failed
		}
		return results[i].UncachedMs < results[j].UncachedMs
	})

	var fastest DNSBenchmark
	for _, r := range results {
		if !r.Failed && !r.IsSystem {
			fastest = r
			break
		}
	}

	return DNSRaceResult{
		Timestamp: time.Now(),
		Entries:   results,
		Fastest:   fastest,
		System:    sys,
	}
}

func benchmarkResolver(ctx context.Context, name, server string, isSystem bool) DNSBenchmark {
	res := DNSBenchmark{
		Name:     name,
		Server:   server,
		IsSystem: isSystem,
	}

	var r *net.Resolver
	if isSystem {
		r = net.DefaultResolver
	} else {
		r = &net.Resolver{
			PreferGo: true,
			Dial: func(dCtx context.Context, network, _ string) (net.Conn, error) {
				d := net.Dialer{Timeout: 2 * time.Second}
				return d.DialContext(dCtx, "udp", net.JoinHostPort(server, "53"))
			},
		}
	}

	cachedDomains := []string{"cloudflare.com", "github.com", "apple.com"}
	var cachedTimes []float64
	for _, dom := range cachedDomains {
		qCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
		t0 := time.Now()
		_, err := r.LookupHost(qCtx, dom)
		cancel()
		if err == nil {
			cachedTimes = append(cachedTimes, float64(time.Since(t0).Microseconds())/1000.0)
		}
	}

	var uncachedTimes []float64
	rng := rand.New(rand.NewSource(time.Now().UnixNano() + int64(len(server)*31)))
	for i := 0; i < 3; i++ {
		host := fmt.Sprintf("ss-%d-%d.example.com", rng.Intn(900000)+100000, i)
		qCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
		t0 := time.Now()
		_, err := r.LookupHost(qCtx, host)
		cancel()
		elapsed := float64(time.Since(t0).Microseconds()) / 1000.0
		// NXDOMAIN from authoritative resolution is a valid timing measurement; timeout is a failure.
		if err == nil || isNXDomain(err) {
			uncachedTimes = append(uncachedTimes, elapsed)
		}
	}

	if len(cachedTimes) == 0 && len(uncachedTimes) == 0 {
		res.Failed = true
		return res
	}
	if len(cachedTimes) > 0 {
		res.CachedMs = median(cachedTimes)
	}
	if len(uncachedTimes) > 0 {
		res.UncachedMs = median(uncachedTimes)
	} else {
		res.UncachedMs = res.CachedMs
	}
	return res
}

func isNXDomain(err error) bool {
	if dnsErr, ok := err.(*net.DNSError); ok {
		return dnsErr.IsNotFound
	}
	return false
}

func median(vals []float64) float64 {
	if len(vals) == 0 {
		return 0
	}
	s := make([]float64, len(vals))
	copy(s, vals)
	sort.Float64s(s)
	mid := len(s) / 2
	if len(s)%2 == 0 {
		return (s[mid-1] + s[mid]) / 2.0
	}
	return s[mid]
}
