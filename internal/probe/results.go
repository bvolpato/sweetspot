package probe

import "time"

// SpeedResult captures a completed or in-progress throughput + bufferbloat measurement.
type SpeedResult struct {
	Timestamp    time.Time `json:"timestamp"`
	Source       string    `json:"source"` // e.g. "apple-cdn", "cloudflare", "continuous", "networkQuality"
	Phase        string    `json:"phase"`  // "idle", "download", "upload", "networkQuality", "done"
	DownloadMbps float64   `json:"download_mbps"`
	UploadMbps   float64   `json:"upload_mbps"`
	IdleRTTMs    float64   `json:"idle_rtt_ms"`
	LoadedRTTMs  float64   `json:"loaded_rtt_ms"`
	BloatDeltaMs float64   `json:"bloat_delta_ms"`
	RPM          int       `json:"rpm"`
	ECNEnabled   bool      `json:"ecn_enabled"`
	L4SEnabled   bool      `json:"l4s_enabled"`
	TestEndpoint string    `json:"test_endpoint,omitempty"`
	Err          string    `json:"error,omitempty"`
}

// SpeedProgress is emitted periodically during active probes so the UI animates live.
type SpeedProgress struct {
	Phase        string
	DownloadMbps float64
	UploadMbps   float64
}

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
