package probe

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os/exec"
	"runtime"
	"sync"
	"sync/atomic"
	"time"
)

const (
	cloudflareDownURL = "https://speed.cloudflare.com/__down?bytes=25000000"
	cloudflareUpURL   = "https://speed.cloudflare.com/__up"
)

// SpeedResult captures a completed or in-progress throughput + bufferbloat measurement.
type SpeedResult struct {
	Timestamp     time.Time `json:"timestamp"`
	Source        string    `json:"source"` // "cloudflare", "continuous", or "networkQuality"
	Phase         string    `json:"phase"`  // "idle", "download", "upload", "networkQuality", "done"
	DownloadMbps  float64   `json:"download_mbps"`
	UploadMbps    float64   `json:"upload_mbps"`
	IdleRTTMs     float64   `json:"idle_rtt_ms"`
	LoadedRTTMs   float64   `json:"loaded_rtt_ms"`
	BloatDeltaMs  float64   `json:"bloat_delta_ms"`
	RPM           int       `json:"rpm"`
	ECNEnabled    bool      `json:"ecn_enabled"`
	L4SEnabled    bool      `json:"l4s_enabled"`
	TestEndpoint  string    `json:"test_endpoint,omitempty"`
	Err           string    `json:"error,omitempty"`
}

// SpeedProgress is emitted periodically during active probes so the UI animates live.
type SpeedProgress struct {
	Phase        string
	DownloadMbps float64
	UploadMbps   float64
}

// RunSpeedBurst runs a fast parallel Cloudflare download + upload probe while sampling
// loaded latency from wanRing to quantify bufferbloat.
func RunSpeedBurst(
	ctx context.Context,
	downDuration time.Duration,
	upDuration time.Duration,
	streams int,
	idleRTTMs float64,
	sampleLoadedRTT func() float64,
	onProgress func(SpeedProgress),
) SpeedResult {
	res := SpeedResult{
		Timestamp: time.Now(),
		Source:    "cloudflare",
		Phase:     "download",
		IdleRTTMs: idleRTTMs,
	}
	if streams <= 0 {
		streams = 4
	}

	client := &http.Client{
		Transport: &http.Transport{
			MaxIdleConns:        streams * 2,
			MaxIdleConnsPerHost: streams * 2,
			DisableCompression:  true,
		},
	}

	var loadedSamples []float64
	var loadedMu sync.Mutex
	recordLoaded := func() {
		if sampleLoadedRTT == nil {
			return
		}
		if v := sampleLoadedRTT(); v > 0 {
			loadedMu.Lock()
			loadedSamples = append(loadedSamples, v)
			loadedMu.Unlock()
		}
	}

	// Phase 1: Download
	downMbps, err := runDownloadPhase(ctx, client, downDuration, streams, func(mbps float64) {
		recordLoaded()
		if onProgress != nil {
			onProgress(SpeedProgress{Phase: "download", DownloadMbps: mbps})
		}
	})
	if err != nil && downMbps == 0 {
		res.Err = err.Error()
		res.Phase = "done"
		return res
	}
	res.DownloadMbps = downMbps

	// Phase 2: Upload
	res.Phase = "upload"
	upMbps, err := runUploadPhase(ctx, client, upDuration, max(streams/2, 2), func(mbps float64) {
		recordLoaded()
		if onProgress != nil {
			onProgress(SpeedProgress{Phase: "upload", DownloadMbps: downMbps, UploadMbps: mbps})
		}
	})
	if err != nil && upMbps == 0 && res.Err == "" {
		res.Err = err.Error()
	}
	res.UploadMbps = upMbps

	loadedMu.Lock()
	if len(loadedSamples) > 0 {
		var sum float64
		for _, v := range loadedSamples {
			sum += v
		}
		res.LoadedRTTMs = sum / float64(len(loadedSamples))
		if res.IdleRTTMs > 0 {
			res.BloatDeltaMs = res.LoadedRTTMs - res.IdleRTTMs
			if res.BloatDeltaMs < 0 {
				res.BloatDeltaMs = 0
			}
		}
		if res.LoadedRTTMs > 0 {
			res.RPM = int(60000.0 / res.LoadedRTTMs)
		}
	}
	loadedMu.Unlock()

	res.Timestamp = time.Now()
	res.Phase = "done"
	return res
}

func runDownloadPhase(ctx context.Context, client *http.Client, dur time.Duration, streams int, onTick func(float64)) (float64, error) {
	pCtx, cancel := context.WithTimeout(ctx, dur)
	defer cancel()

	var totalBytes atomic.Int64
	var firstErr atomic.Value
	start := time.Now()

	var wg sync.WaitGroup
	for i := 0; i < streams; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			buf := make([]byte, 64*1024)
			for pCtx.Err() == nil {
				req, err := http.NewRequestWithContext(pCtx, http.MethodGet, cloudflareDownURL, nil)
				if err != nil {
					return
				}
				resp, err := client.Do(req)
				if err != nil {
					if pCtx.Err() == nil {
						firstErr.Store(err)
					}
					return
				}
				for {
					n, rerr := resp.Body.Read(buf)
					if n > 0 {
						totalBytes.Add(int64(n))
					}
					if rerr != nil {
						break
					}
				}
				_ = resp.Body.Close()
			}
		}()
	}

	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

loop:
	for {
		select {
		case <-done:
			break loop
		case <-ticker.C:
			elapsed := time.Since(start).Seconds()
			if elapsed > 0.2 && onTick != nil {
				onTick(float64(totalBytes.Load()) * 8.0 / (elapsed * 1e6))
			}
		}
	}

	elapsed := time.Since(start).Seconds()
	var mbps float64
	if elapsed > 0 {
		mbps = float64(totalBytes.Load()) * 8.0 / (elapsed * 1e6)
	}
	if v := firstErr.Load(); v != nil && mbps == 0 {
		return 0, v.(error)
	}
	return mbps, nil
}

func runUploadPhase(ctx context.Context, client *http.Client, dur time.Duration, streams int, onTick func(float64)) (float64, error) {
	pCtx, cancel := context.WithTimeout(ctx, dur)
	defer cancel()

	chunk := bytes.Repeat([]byte("S"), 2*1024*1024)
	var totalBytes atomic.Int64
	var firstErr atomic.Value
	start := time.Now()

	var wg sync.WaitGroup
	for i := 0; i < streams; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for pCtx.Err() == nil {
				pr := &countingReader{r: bytes.NewReader(chunk), counter: &totalBytes}
				req, err := http.NewRequestWithContext(pCtx, http.MethodPost, cloudflareUpURL, pr)
				if err != nil {
					return
				}
				req.ContentLength = int64(len(chunk))
				resp, err := client.Do(req)
				if err != nil {
					if pCtx.Err() == nil {
						firstErr.Store(err)
					}
					return
				}
				_, _ = io.Copy(io.Discard, resp.Body)
				_ = resp.Body.Close()
			}
		}()
	}

	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

loop:
	for {
		select {
		case <-done:
			break loop
		case <-ticker.C:
			elapsed := time.Since(start).Seconds()
			if elapsed > 0.2 && onTick != nil {
				onTick(float64(totalBytes.Load()) * 8.0 / (elapsed * 1e6))
			}
		}
	}

	elapsed := time.Since(start).Seconds()
	var mbps float64
	if elapsed > 0 {
		mbps = float64(totalBytes.Load()) * 8.0 / (elapsed * 1e6)
	}
	if v := firstErr.Load(); v != nil && mbps == 0 {
		return 0, v.(error)
	}
	return mbps, nil
}

type countingReader struct {
	r       io.Reader
	counter *atomic.Int64
}

func (c *countingReader) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	if n > 0 {
		c.counter.Add(int64(n))
	}
	return n, err
}

// RunMacOSNetworkQuality invokes Apple's built-in /usr/bin/networkQuality -c JSON test on macOS.
func RunMacOSNetworkQuality(ctx context.Context) SpeedResult {
	res := SpeedResult{
		Timestamp: time.Now(),
		Source:    "networkQuality",
		Phase:     "done",
	}
	if runtime.GOOS != "darwin" {
		res.Err = "networkQuality is only available on macOS"
		return res
	}
	c, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()

	out, err := exec.CommandContext(c, "/usr/bin/networkQuality", "-c").Output()
	if err != nil {
		res.Err = err.Error()
		return res
	}

	var parsed struct {
		DLThroughput     float64 `json:"dl_throughput"`
		ULThroughput     float64 `json:"ul_throughput"`
		Responsiveness   float64 `json:"responsiveness"`
		BaseRTT          float64 `json:"base_rtt"`
		TestEndpoint     string  `json:"test_endpoint"`
		ILH2             float64 `json:"il_h2_req_resp"`
		LoadedRPM        float64 `json:"dl_responsiveness"`
	}
	if err := json.Unmarshal(out, &parsed); err != nil {
		res.Err = err.Error()
		return res
	}

	res.DownloadMbps = parsed.DLThroughput / 1e6
	res.UploadMbps = parsed.ULThroughput / 1e6
	res.IdleRTTMs = parsed.BaseRTT
	res.TestEndpoint = parsed.TestEndpoint
	if parsed.Responsiveness > 0 {
		res.RPM = int(parsed.Responsiveness)
		res.LoadedRTTMs = 60000.0 / parsed.Responsiveness
		if res.IdleRTTMs > 0 && res.LoadedRTTMs > res.IdleRTTMs {
			res.BloatDeltaMs = res.LoadedRTTMs - res.IdleRTTMs
		}
	}
	res.Timestamp = time.Now()
	return res
}
