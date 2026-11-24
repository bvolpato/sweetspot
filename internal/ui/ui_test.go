package ui

import (
	"strings"
	"testing"

	"github.com/bvolpato/sweetspot/internal/probe"
	"github.com/bvolpato/sweetspot/internal/wifi"
)

func TestRenderSparklineNegativeRSSIDoesNotRenderLossMarkers(t *testing.T) {
	out := RenderSparkline([]float64{-62, -58, -45, -40}, 10, -85, -30, true, nil)
	if strings.Contains(out, "×") {
		t.Fatalf("negative RSSI with allowNegative=true should not render loss marker ×: %q", out)
	}
	lossOut := RenderSparkline([]float64{5.0, -1.0, 6.0}, 10, 0, 20, false, nil)
	if !strings.Contains(lossOut, "×") {
		t.Fatalf("negative ping with allowNegative=false should render loss marker ×: %q", lossOut)
	}
}

func TestBrailleChartSoftScalesOutlierSpike(t *testing.T) {
	vals := []float64{5, 6, 5, 7, 6, 5, 6, 5, 402, 5, 6, 5}
	chart := BrailleChart(vals, 50, 4, 0, 0, false, "ms", nil)
	lines := strings.Split(chart, "\n")
	if len(lines) != 4 {
		t.Fatalf("expected 4 chart rows, got %d", len(lines))
	}
	// Soft Y-axis cap should keep top label well below the raw 402ms outlier.
	if strings.Contains(lines[0], "462") || strings.Contains(lines[0], "402") {
		t.Fatalf("expected soft Y-axis cap below 402ms outlier, got top row %q", lines[0])
	}
}

func TestAllTabsRenderWithoutPanicAndCleanSpinnerPill(t *testing.T) {
	m := NewModel(Options{WANTarget: "1.1.1.1"})
	defer m.cancel()
	m.width = 140
	m.height = 40
	m.link = wifi.LinkInfo{
		Connected:   true,
		Interface:   "en0",
		Band:        wifi.Band5GHz,
		Channel:     48,
		WidthMHz:    80,
		RSSI:        -42,
		Noise:       -92,
		SNR:         50,
		TxRateMbps:  960,
		PHYMode:     "802.11ax (Wi-Fi 6)",
		Security:    "WPA2 Personal",
		HasCCA:      true,
		CCAPct:      22,
		LinkQuality: 100,
		EffDownMbps: 650,
		Privileged:  true,
	}
	m.gwStats = probe.PingStats{Target: "192.168.1.1", Sent: 10, Received: 10, LastMs: 4.5, AvgMs: 5.0, P95Ms: 7.0, MaxMs: 8.0, JitterMs: 0.9, History: []float64{4, 5, 6}}
	m.wanStats = probe.PingStats{Target: "1.1.1.1", Sent: 10, Received: 10, LastMs: 12.0, AvgMs: 14.0, P95Ms: 18.0, MaxMs: 20.0, JitterMs: 2.0, History: []float64{12, 14, 15}}
	m.speedRunning = true
	m.speedProgress = probe.SpeedProgress{Phase: "download", DownloadMbps: 420}
	m.recompute()

	// Spinner itself must not carry nested ANSI escapes that break pill backgrounds.
	if strings.Contains(m.spinner.View(), "\x1b[") {
		t.Fatalf("spinner should not emit nested ANSI codes inside pills: %q", m.spinner.View())
	}

	for _, tab := range []Tab{TabRadar, TabSpots, TabSpectrum, TabAdvisor} {
		m.tab = tab
		out := m.View()
		if len(out) < 200 {
			t.Fatalf("tab %d rendered unexpectedly short view (%d bytes)", tab, len(out))
		}
	}
}
