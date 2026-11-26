//go:build !js

package probe

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os/exec"
	"runtime"
	"sync"
	"sync/atomic"
	"time"
)

type speedEndpoint struct {
	name   string
	url    string
	method string
}

var downloadEndpoints = []speedEndpoint{
	{name: "apple-cdn", url: "https://mensura.cdn-apple.com/api/v1/gm/large", method: http.MethodGet},
	{name: "cloudflare", url: "https://speed.cloudflare.com/__down?bytes=25000000", method: http.MethodGet},
	{name: "hetzner", url: "https://ash-speed.hetzner.com/100MB.bin", method: http.MethodGet},
}

var uploadEndpoints = []speedEndpoint{
	{name: "cloudflare", url: "https://speed.cloudflare.com/__up", method: http.MethodPost},
	{name: "apple-cdn", url: "https://mensura.cdn-apple.com/api/v1/gm/slurp", method: http.MethodPost},
}

// RunSpeedBurst runs a multi-stream HTTP download + upload probe with automatic mirror
// failover (avoiding HTTP 429 rate limits behind shared office NATs) while sampling
// loaded latency to quantify bufferbloat.
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
		Source:    "burst",
		Phase:     "download",
		IdleRTTMs: idleRTTMs,
	}
	if streams <= 0 {
		streams = 3
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

	// Phase 1: Download with automatic endpoint failover on 429 / non-2xx / error.
	var downMbps float64
	var downSrc string
	var lastDownErr error
	for _, ep := range downloadEndpoints {
		if ctx.Err() != nil {
			break
		}
		mbps, err := runDownloadEndpoint(ctx, client, ep, downDuration, streams, func(live float64) {
			recordLoaded()
			if onProgress != nil {
				onProgress(SpeedProgress{Phase: "download", DownloadMbps: live})
			}
		})
		if mbps > 0 {
			downMbps = mbps
			downSrc = ep.name
			lastDownErr = nil
			break
		}
		if err != nil {
			lastDownErr = err
		}
	}
	res.DownloadMbps = downMbps
	if lastDownErr != nil && downMbps == 0 {
		res.Err = "download: " + lastDownErr.Error()
	}

	// Phase 2: Upload with automatic endpoint failover.
	res.Phase = "upload"
	var upMbps float64
	var lastUpErr error
	for _, ep := range uploadEndpoints {
		if ctx.Err() != nil {
			break
		}
		mbps, err := runUploadEndpoint(ctx, client, ep, upDuration, max(streams-1, 2), func(live float64) {
			recordLoaded()
			if onProgress != nil {
				onProgress(SpeedProgress{Phase: "upload", DownloadMbps: downMbps, UploadMbps: live})
			}
		})
		if mbps > 0 {
			upMbps = mbps
			lastUpErr = nil
			break
		}
		if err != nil {
			lastUpErr = err
		}
	}
	res.UploadMbps = upMbps
	if lastUpErr != nil && upMbps == 0 && res.Err == "" {
		res.Err = "upload: " + lastUpErr.Error()
	}
	if downSrc != "" {
		res.Source = downSrc
	}

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

func runDownloadEndpoint(ctx context.Context, client *http.Client, ep speedEndpoint, dur time.Duration, streams int, onTick func(float64)) (float64, error) {
	pCtx, cancel := context.WithTimeout(ctx, dur)
	defer cancel()

	var totalBytes atomic.Int64
	var firstErr atomic.Value
	var aborted atomic.Bool
	start := time.Now()

	var wg sync.WaitGroup
	for i := 0; i < streams; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			buf := make([]byte, 64*1024)
			for pCtx.Err() == nil && !aborted.Load() {
				req, err := http.NewRequestWithContext(pCtx, ep.method, ep.url, nil)
				if err != nil {
					return
				}
				req.Header.Set("User-Agent", "sweetspot/0.1")
				req.Header.Set("Cache-Control", "no-store")
				resp, err := client.Do(req)
				if err != nil {
					if pCtx.Err() == nil {
						firstErr.Store(err)
					}
					return
				}
				if resp.StatusCode < 200 || resp.StatusCode >= 300 {
					_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
					_ = resp.Body.Close()
					firstErr.Store(fmt.Errorf("%s returned HTTP %d", ep.name, resp.StatusCode))
					aborted.Store(true)
					cancel()
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
			if elapsed > 0.2 && totalBytes.Load() > 0 && onTick != nil {
				onTick(float64(totalBytes.Load()) * 8.0 / (elapsed * 1e6))
			}
		}
	}

	elapsed := time.Since(start).Seconds()
	bytesRead := totalBytes.Load()
	if bytesRead == 0 || elapsed <= 0 {
		if v := firstErr.Load(); v != nil {
			return 0, v.(error)
		}
		return 0, fmt.Errorf("%s transferred 0 bytes", ep.name)
	}
	return float64(bytesRead) * 8.0 / (elapsed * 1e6), nil
}

func runUploadEndpoint(ctx context.Context, client *http.Client, ep speedEndpoint, dur time.Duration, streams int, onTick func(float64)) (float64, error) {
	pCtx, cancel := context.WithTimeout(ctx, dur)
	defer cancel()

	chunk := bytes.Repeat([]byte("S"), 2*1024*1024)
	var totalBytes atomic.Int64
	var firstErr atomic.Value
	var aborted atomic.Bool
	start := time.Now()

	var wg sync.WaitGroup
	for i := 0; i < streams; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for pCtx.Err() == nil && !aborted.Load() {
				pr := &countingReader{r: bytes.NewReader(chunk), counter: &totalBytes}
				req, err := http.NewRequestWithContext(pCtx, ep.method, ep.url, pr)
				if err != nil {
					return
				}
				req.Header.Set("User-Agent", "sweetspot/0.1")
				req.ContentLength = int64(len(chunk))
				resp, err := client.Do(req)
				if err != nil {
					if pCtx.Err() == nil {
						firstErr.Store(err)
					}
					return
				}
				_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
				_ = resp.Body.Close()
				if resp.StatusCode < 200 || resp.StatusCode >= 300 {
					firstErr.Store(fmt.Errorf("%s returned HTTP %d", ep.name, resp.StatusCode))
					aborted.Store(true)
					cancel()
					return
				}
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
			if elapsed > 0.2 && totalBytes.Load() > 0 && onTick != nil {
				onTick(float64(totalBytes.Load()) * 8.0 / (elapsed * 1e6))
			}
		}
	}

	elapsed := time.Since(start).Seconds()
	bytesSent := totalBytes.Load()
	if aborted.Load() && bytesSent <= int64(len(chunk)*streams) {
		if v := firstErr.Load(); v != nil {
			return 0, v.(error)
		}
	}
	if bytesSent == 0 || elapsed <= 0 {
		if v := firstErr.Load(); v != nil {
			return 0, v.(error)
		}
		return 0, fmt.Errorf("%s uploaded 0 bytes", ep.name)
	}
	return float64(bytesSent) * 8.0 / (elapsed * 1e6), nil
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
		DLThroughput   float64 `json:"dl_throughput"`
		ULThroughput   float64 `json:"ul_throughput"`
		Responsiveness float64 `json:"responsiveness"`
		BaseRTT        float64 `json:"base_rtt"`
		TestEndpoint   string  `json:"test_endpoint"`
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
