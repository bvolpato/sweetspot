package probe

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestParsePingLineAcrossPlatforms(t *testing.T) {
	cases := []struct {
		line string
		want float64
		ok   bool
	}{
		{"64 bytes from 192.168.1.1: icmp_seq=0 ttl=64 time=5.416 ms", 5.416, true},
		{"64 bytes from 1.1.1.1: icmp_seq=1 ttl=54 time=12.4 ms", 12.4, true},
		{"Reply from 1.1.1.1: bytes=32 time=14ms TTL=54", 14.0, true},
		{"Reply from 192.168.1.1: bytes=32 time<1ms TTL=64", 0.5, true},
		{"Request timeout for icmp_seq 3", 0, false},
	}
	for _, tc := range cases {
		got, ok := parsePingLineMs(tc.line)
		if ok != tc.ok || (ok && got != tc.want) {
			t.Errorf("parsePingLineMs(%q) = (%v, %v), want (%v, %v)", tc.line, got, ok, tc.want, tc.ok)
		}
	}
	for _, line := range []string{"Request timeout for icmp_seq 3", "Request timed out.", "Destination host unreachable"} {
		if !containsTimeout(line) {
			t.Errorf("expected containsTimeout(%q) == true", line)
		}
	}
}

func TestPingRingStatsAndRollover(t *testing.T) {
	ring := NewPingRing("192.168.1.1", 5, 40.0)
	samples := []PingSample{
		{RTTMs: 100.0}, // will roll out
		{RTTMs: 5.0},
		{RTTMs: 10.0},
		{RTTMs: 50.0}, // spike
		{Lost: true},
		{RTTMs: 15.0},
	}
	for _, s := range samples {
		ring.Add(s)
	}
	st := ring.Stats()
	if st.Sent != 5 || st.Received != 4 {
		t.Fatalf("expected sent=5 received=4, got sent=%d received=%d", st.Sent, st.Received)
	}
	if st.LossPct != 20.0 {
		t.Fatalf("expected 20%% loss, got %.1f%%", st.LossPct)
	}
	if st.MinMs != 5.0 || st.MaxMs != 50.0 || st.AvgMs != 20.0 {
		t.Fatalf("unexpected min/avg/max: %.1f/%.1f/%.1f", st.MinMs, st.AvgMs, st.MaxMs)
	}
	if st.SpikeCount != 1 {
		t.Fatalf("expected 1 spike, got %d", st.SpikeCount)
	}
	if st.JitterMs <= 0 {
		t.Fatalf("expected positive jitter, got %.2f", st.JitterMs)
	}
}

func TestSpeedFailoverOnHTTP429(t *testing.T) {
	// Server 1 simulates Cloudflare returning HTTP 429 with a 1-byte body behind a shared NAT.
	rateLimited := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte("1"))
	}))
	defer rateLimited.Close()

	// Server 2 serves real download bytes and accepts uploads.
	payload := bytes.Repeat([]byte("A"), 512*1024)
	healthy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			_, _ = io.Copy(io.Discard, r.Body)
			w.WriteHeader(http.StatusOK)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(payload)
	}))
	defer healthy.Close()

	origDown := downloadEndpoints
	origUp := uploadEndpoints
	downloadEndpoints = []speedEndpoint{
		{name: "rate-limited", url: rateLimited.URL, method: http.MethodGet},
		{name: "healthy-cdn", url: healthy.URL, method: http.MethodGet},
	}
	uploadEndpoints = []speedEndpoint{
		{name: "rate-limited", url: rateLimited.URL, method: http.MethodPost},
		{name: "healthy-cdn", url: healthy.URL, method: http.MethodPost},
	}
	t.Cleanup(func() {
		downloadEndpoints = origDown
		uploadEndpoints = origUp
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	res := RunSpeedBurst(ctx, 450*time.Millisecond, 350*time.Millisecond, 2, 15.0, func() float64 { return 60.0 }, nil)
	if res.DownloadMbps <= 0 {
		t.Fatalf("expected failover download > 0 Mbps, got %+v", res)
	}
	if res.UploadMbps <= 0 {
		t.Fatalf("expected failover upload > 0 Mbps, got %+v", res)
	}
	if res.Source != "healthy-cdn" {
		t.Fatalf("expected failover source 'healthy-cdn', got %q", res.Source)
	}
	if res.BloatDeltaMs != 45.0 || res.RPM != 1000 {
		t.Fatalf("expected bloat=45ms rpm=1000, got bloat=%.1f rpm=%d", res.BloatDeltaMs, res.RPM)
	}
}
