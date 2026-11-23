package advisor

import (
	"math"

	"github.com/bvolpato/sweetspot/internal/probe"
	"github.com/bvolpato/sweetspot/internal/wifi"
)

// ScoreBreakdown details the 0-100 SweetSpot score across RF, latency, speed, and responsiveness.
type ScoreBreakdown struct {
	Total       int    `json:"total"`
	Grade       string `json:"grade"`
	SignalScore int    `json:"signal_score"` // out of 35
	LatScore    int    `json:"lat_score"`    // out of 35
	SpeedScore  int    `json:"speed_score"`  // out of 30
	Summary     string `json:"summary"`
}

// ComputeScore computes an intuitive 0-100 SweetSpot score for the current spot.
func ComputeScore(link wifi.LinkInfo, gw probe.PingStats, wan probe.PingStats, speed probe.SpeedResult) ScoreBreakdown {
	if !link.Connected && !link.IsEthernet {
		return ScoreBreakdown{Total: 0, Grade: "F", Summary: "Disconnected"}
	}

	// 1. Signal & PHY quality (0-35)
	sig := 0.0
	if link.IsEthernet {
		sig = 35
	} else {
		// RSSI: -45 dBm or better -> 20 pts; -85 dBm -> 0 pts
		rssiNorm := clamp((float64(link.RSSI)+85.0)/40.0, 0, 1) * 20.0
		// SNR: 35 dB or better -> 10 pts; 10 dB -> 0 pts
		snrVal := float64(link.SNR)
		if snrVal == 0 && link.RSSI != 0 {
			snrVal = float64(link.RSSI + 92)
		}
		snrNorm := clamp((snrVal-10.0)/25.0, 0, 1) * 10.0
		// PHY rate: 800+ Mbps -> 5 pts
		phyNorm := clamp(link.TxRateMbps/800.0, 0, 1) * 5.0
		sig = rssiNorm + snrNorm + phyNorm
	}

	// 2. Latency, jitter, and packet loss (0-35)
	lat := 28.0
	if gw.Received > 0 || wan.Received > 0 {
		gwAvg := gw.AvgMs
		if gw.Received == 0 {
			gwAvg = 5
		}
		wanAvg := wan.AvgMs
		if wan.Received == 0 {
			wanAvg = 20
		}
		// Router latency: <= 3ms full 12 pts, >= 40ms 0 pts
		gwPts := clamp((40.0-gwAvg)/37.0, 0, 1) * 12.0
		// WAN latency: <= 15ms full 10 pts, >= 120ms 0 pts
		wanPts := clamp((120.0-wanAvg)/105.0, 0, 1) * 10.0
		// Jitter penalty: <= 2ms full 8 pts, >= 30ms 0 pts
		jitter := math.Max(gw.JitterMs, wan.JitterMs*0.5)
		jitPts := clamp((30.0-jitter)/28.0, 0, 1) * 8.0
		// Loss: 0% -> 5 pts, >= 5% -> 0 pts
		loss := math.Max(gw.LossPct, wan.LossPct)
		lossPts := clamp((5.0-loss)/5.0, 0, 1) * 5.0
		lat = gwPts + wanPts + jitPts + lossPts
	}

	// 3. Speed & bufferbloat (0-30)
	// Before an active speed burst runs, normalize the measured RF + Latency components (70 pts)
	// so a strong, low-jitter spot is not penalized before pressing [b].
	spd := 0.0
	var total int
	if speed.DownloadMbps > 0 {
		// Downlink: log curve; 500+ Mbps -> 15 pts, 100 Mbps -> ~10.5 pts, 25 Mbps -> ~6 pts
		dlPts := clamp(math.Log10(math.Max(speed.DownloadMbps, 1))/math.Log10(500.0), 0, 1) * 15.0
		// Uplink: 50+ Mbps -> 7 pts
		ulPts := clamp(speed.UploadMbps/50.0, 0, 1) * 7.0
		// Responsiveness / bufferbloat: <= 25ms bloat or >= 1000 RPM -> 8 pts; >= 250ms -> 0 pts
		bloatPts := 6.0
		if speed.RPM > 0 {
			bloatPts = clamp(float64(speed.RPM)/1000.0, 0, 1) * 8.0
		} else if speed.BloatDeltaMs > 0 {
			bloatPts = clamp((250.0-speed.BloatDeltaMs)/225.0, 0, 1) * 8.0
		}
		spd = dlPts + ulPts + bloatPts
		total = int(math.Round(sig + lat + spd))
	} else {
		est := link.TxRateMbps * 0.55
		if link.IsEthernet {
			est = 500
		}
		spd = clamp(est/400.0, 0, 1) * 30.0
		total = int(math.Round((sig+lat)/70.0*85.0 + (spd/30.0)*15.0))
	}
	if total > 100 {
		total = 100
	}
	if total < 0 {
		total = 0
	}

	return ScoreBreakdown{
		Total:       total,
		Grade:       gradeFor(total),
		SignalScore: int(math.Round(sig)),
		LatScore:    int(math.Round(lat)),
		SpeedScore:  int(math.Round(spd)),
		Summary:     summaryFor(total),
	}
}

func gradeFor(score int) string {
	switch {
	case score >= 93:
		return "A+"
	case score >= 85:
		return "A"
	case score >= 76:
		return "B"
	case score >= 65:
		return "C"
	case score >= 50:
		return "D"
	default:
		return "F"
	}
}

func summaryFor(score int) string {
	switch {
	case score >= 90:
		return "Sweet spot -- fast, clean, low jitter"
	case score >= 78:
		return "Solid spot for calls and dev work"
	case score >= 65:
		return "Usable, with noticeable headroom to tune"
	case score >= 50:
		return "Degraded -- expect jitter or slowdowns"
	default:
		return "Poor spot -- move closer or switch channels"
	}
}

func clamp(v, lo, hi float64) float64 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
