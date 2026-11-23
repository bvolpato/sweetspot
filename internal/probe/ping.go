package probe

import (
	"bufio"
	"context"
	"math"
	"os/exec"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"sync"
	"time"
)

// PingSample represents a single ICMP echo observation.
type PingSample struct {
	Timestamp time.Time
	RTTMs     float64
	Lost      bool
}

// PingStats summarizes rolling latency, jitter, and loss for a target host.
type PingStats struct {
	Target     string    `json:"target"`
	Sent       int       `json:"sent"`
	Received   int       `json:"received"`
	LossPct    float64   `json:"loss_pct"`
	LastMs     float64   `json:"last_ms"`
	MinMs      float64   `json:"min_ms"`
	AvgMs      float64   `json:"avg_ms"`
	P95Ms      float64   `json:"p95_ms"`
	MaxMs      float64   `json:"max_ms"`
	JitterMs   float64   `json:"jitter_ms"`
	SpikeCount int       `json:"spike_count"`
	History    []float64 `json:"-"`
}

// PingRing maintains a rolling history of ping samples for one target.
type PingRing struct {
	mu             sync.RWMutex
	target         string
	maxLen         int
	spikeThreshold float64
	samples        []PingSample
}

// NewPingRing creates a rolling ring buffer for ping telemetry.
func NewPingRing(target string, maxLen int, spikeThresholdMs float64) *PingRing {
	return &PingRing{
		target:         target,
		maxLen:         maxLen,
		spikeThreshold: spikeThresholdMs,
		samples:        make([]PingSample, 0, maxLen),
	}
}

// Add appends a sample to the ring buffer.
func (r *PingRing) Add(s PingSample) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.samples) >= r.maxLen {
		copy(r.samples, r.samples[1:])
		r.samples[len(r.samples)-1] = s
	} else {
		r.samples = append(r.samples, s)
	}
}

// Reset clears the ring and updates the target host.
func (r *PingRing) Reset(target string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.target = target
	r.samples = r.samples[:0]
}

// Stats computes rolling latency, p95, jitter (standard deviation), and loss.
func (r *PingRing) Stats() PingStats {
	r.mu.RLock()
	defer r.mu.RUnlock()

	st := PingStats{Target: r.target}
	if len(r.samples) == 0 {
		return st
	}

	st.Sent = len(r.samples)
	rtts := make([]float64, 0, len(r.samples))
	st.History = make([]float64, 0, len(r.samples))

	var sum float64
	st.MinMs = math.MaxFloat64
	for _, s := range r.samples {
		if s.Lost {
			st.History = append(st.History, -1)
			continue
		}
		st.Received++
		st.LastMs = s.RTTMs
		rtts = append(rtts, s.RTTMs)
		st.History = append(st.History, s.RTTMs)
		sum += s.RTTMs
		if s.RTTMs < st.MinMs {
			st.MinMs = s.RTTMs
		}
		if s.RTTMs > st.MaxMs {
			st.MaxMs = s.RTTMs
		}
		if r.spikeThreshold > 0 && s.RTTMs >= r.spikeThreshold {
			st.SpikeCount++
		}
	}

	st.LossPct = float64(st.Sent-st.Received) * 100.0 / float64(st.Sent)
	if len(rtts) == 0 {
		st.MinMs = 0
		return st
	}

	st.AvgMs = sum / float64(len(rtts))
	var variance float64
	for _, v := range rtts {
		d := v - st.AvgMs
		variance += d * d
	}
	st.JitterMs = math.Sqrt(variance / float64(len(rtts)))

	sorted := make([]float64, len(rtts))
	copy(sorted, rtts)
	sort.Float64s(sorted)
	p95Idx := int(math.Ceil(0.95*float64(len(sorted)))) - 1
	if p95Idx < 0 {
		p95Idx = 0
	}
	if p95Idx >= len(sorted) {
		p95Idx = len(sorted) - 1
	}
	st.P95Ms = sorted[p95Idx]
	return st
}

var rePingTime = regexp.MustCompile(`time([=<])([0-9.]+)\s*ms`)

func parsePingLineMs(line string) (float64, bool) {
	m := rePingTime.FindStringSubmatch(line)
	if m == nil {
		return 0, false
	}
	ms, err := strconv.ParseFloat(m[2], 64)
	if err != nil {
		return 0, false
	}
	if m[1] == "<" && ms <= 1 {
		ms = 0.5
	}
	return ms, true
}

// StreamPing runs an OS ping subprocess and delivers each sample to onSample.
// Using the system ping binary avoids raw-socket root privileges on macOS, Linux, and Windows.
func StreamPing(ctx context.Context, target string, interval time.Duration, onSample func(PingSample)) {
	if target == "" {
		return
	}
	intervalSec := interval.Seconds()
	if intervalSec < 0.2 {
		intervalSec = 0.2
	}

	pingBin := "ping"
	if runtime.GOOS == "darwin" {
		pingBin = "/sbin/ping"
	}

	for {
		if ctx.Err() != nil {
			return
		}
		var args []string
		if runtime.GOOS == "windows" {
			args = []string{"-t", target}
		} else {
			args = []string{"-i", strconv.FormatFloat(intervalSec, 'f', 2, 64), target}
		}
		cmd := exec.CommandContext(ctx, pingBin, args...)
		stdout, err := cmd.StdoutPipe()
		if err != nil {
			time.Sleep(time.Second)
			continue
		}
		if err := cmd.Start(); err != nil {
			time.Sleep(time.Second)
			continue
		}

		scanner := bufio.NewScanner(stdout)
		for scanner.Scan() {
			line := scanner.Text()
			if ms, ok := parsePingLineMs(line); ok {
				onSample(PingSample{Timestamp: time.Now(), RTTMs: ms})
			} else if containsTimeout(line) {
				onSample(PingSample{Timestamp: time.Now(), Lost: true})
			}
		}
		_ = cmd.Wait()

		select {
		case <-ctx.Done():
			return
		case <-time.After(1 * time.Second):
		}
	}
}

func containsTimeout(line string) bool {
	return regexp.MustCompile(`(?i)timeout|timed out|unreachable|no route|100(\.0)?% packet loss`).MatchString(line)
}

// OneShotPing sends count quick ICMP pings and returns aggregated stats.
func OneShotPing(ctx context.Context, target string, count int, spikeThresholdMs float64) PingStats {
	ring := NewPingRing(target, count+4, spikeThresholdMs)
	if target == "" || count <= 0 {
		return ring.Stats()
	}
	pingBin := "ping"
	if runtime.GOOS == "darwin" {
		pingBin = "/sbin/ping"
	}
	timeout := time.Duration(count)*600*time.Millisecond + 4*time.Second
	if runtime.GOOS == "windows" {
		timeout = time.Duration(count)*1200*time.Millisecond + 4*time.Second
	}
	c, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	var args []string
	if runtime.GOOS == "windows" {
		args = []string{"-n", strconv.Itoa(count), target}
	} else {
		args = []string{"-c", strconv.Itoa(count), "-i", "0.2", target}
	}
	out, _ := exec.CommandContext(c, pingBin, args...).CombinedOutput()
	for _, line := range regexp.MustCompile(`\r?\n`).Split(string(out), -1) {
		if ms, ok := parsePingLineMs(line); ok {
			ring.Add(PingSample{Timestamp: time.Now(), RTTMs: ms})
		} else if containsTimeout(line) {
			ring.Add(PingSample{Timestamp: time.Now(), Lost: true})
		}
	}
	return ring.Stats()
}
