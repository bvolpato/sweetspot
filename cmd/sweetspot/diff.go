package main

import (
	"fmt"
	"strings"
	"time"

	"github.com/bvolpato/sweetspot/internal/advisor"
	"github.com/bvolpato/sweetspot/internal/probe"
	"github.com/bvolpato/sweetspot/internal/wifi"
)

// DiagnosticReport is the canonical JSON schema emitted by `sweetspot diagnose --json`
// and consumed by `sweetspot diff before.json after.json`.
type DiagnosticReport struct {
	SchemaVersion string                  `json:"schema_version"`
	ToolVersion   string                  `json:"tool_version"`
	Timestamp     time.Time               `json:"timestamp"`
	Score         advisor.ScoreBreakdown  `json:"score"`
	Link          wifi.LinkInfo           `json:"link"`
	GatewayPing   probe.PingStats         `json:"gateway_ping"`
	WANPing       probe.PingStats         `json:"wan_ping"`
	Speed         probe.SpeedResult       `json:"speed"`
	DNS           probe.DNSRaceResult     `json:"dns"`
	Scan          wifi.ScanResult         `json:"scan"`
	Modem         advisor.ModemReport     `json:"modem"`
	Advice        []advisor.Advice        `json:"advice"`
	Playbook      []advisor.ChecklistItem `json:"playbook,omitempty"`
}

// DiffMetric compares a single metric between two DiagnosticReports.
type DiffMetric struct {
	Metric string `json:"metric"`
	Before string `json:"before"`
	After  string `json:"after"`
	Delta  string `json:"delta"`
}

// DiffReport summarizes the before/after comparison of two diagnostic snapshots.
type DiffReport struct {
	BeforeTime time.Time    `json:"before_timestamp"`
	AfterTime  time.Time    `json:"after_timestamp"`
	ScoreDelta int          `json:"score_delta"`
	Metrics    []DiffMetric `json:"metrics"`
}

// ComputeDiff builds a structured Before vs. After comparison between two reports.
func ComputeDiff(before, after DiagnosticReport) DiffReport {
	rep := DiffReport{
		BeforeTime: before.Timestamp,
		AfterTime:  after.Timestamp,
		ScoreDelta: after.Score.Total - before.Score.Total,
	}

	rep.Metrics = append(rep.Metrics, DiffMetric{
		Metric: "SweetSpot Score",
		Before: fmt.Sprintf("%d/100 (%s)", before.Score.Total, before.Score.Grade),
		After:  fmt.Sprintf("%d/100 (%s)", after.Score.Total, after.Score.Grade),
		Delta:  fmt.Sprintf("%+d pts", after.Score.Total-before.Score.Total),
	})

	rep.Metrics = append(rep.Metrics, DiffMetric{
		Metric: "Wi-Fi Link",
		Before: fmt.Sprintf("%s (%.0f Mbps PHY)", before.Link.ChannelLabel(), before.Link.TxRateMbps),
		After:  fmt.Sprintf("%s (%.0f Mbps PHY)", after.Link.ChannelLabel(), after.Link.TxRateMbps),
		Delta:  fmt.Sprintf("%+.0f Mbps PHY", after.Link.TxRateMbps-before.Link.TxRateMbps),
	})

	if before.Link.RSSI != 0 || after.Link.RSSI != 0 {
		rep.Metrics = append(rep.Metrics, DiffMetric{
			Metric: "Signal / SNR",
			Before: fmt.Sprintf("%d dBm / %d dB", before.Link.RSSI, before.Link.SNR),
			After:  fmt.Sprintf("%d dBm / %d dB", after.Link.RSSI, after.Link.SNR),
			Delta:  fmt.Sprintf("%+d dBm / %+d dB", after.Link.RSSI-before.Link.RSSI, after.Link.SNR-before.Link.SNR),
		})
	}

	if before.Link.HasCCA || after.Link.HasCCA {
		rep.Metrics = append(rep.Metrics, DiffMetric{
			Metric: "Channel Airtime (CCA)",
			Before: fmt.Sprintf("%d%%", before.Link.CCAPct),
			After:  fmt.Sprintf("%d%%", after.Link.CCAPct),
			Delta:  fmt.Sprintf("%+d%%", after.Link.CCAPct-before.Link.CCAPct),
		})
	}

	if before.Speed.DownloadMbps > 0 || after.Speed.DownloadMbps > 0 {
		delta := fmt.Sprintf("%+.1f Mbps", after.Speed.DownloadMbps-before.Speed.DownloadMbps)
		if before.Speed.DownloadMbps > 0 && after.Speed.DownloadMbps > 0 {
			ratio := after.Speed.DownloadMbps / before.Speed.DownloadMbps
			delta = fmt.Sprintf("%.2fx (%+.1f Mbps)", ratio, after.Speed.DownloadMbps-before.Speed.DownloadMbps)
		}
		rep.Metrics = append(rep.Metrics, DiffMetric{
			Metric: "Downlink",
			Before: fmt.Sprintf("%.1f Mbps", before.Speed.DownloadMbps),
			After:  fmt.Sprintf("%.1f Mbps", after.Speed.DownloadMbps),
			Delta:  delta,
		})
	}

	if before.Speed.UploadMbps > 0 || after.Speed.UploadMbps > 0 {
		delta := fmt.Sprintf("%+.1f Mbps", after.Speed.UploadMbps-before.Speed.UploadMbps)
		if before.Speed.UploadMbps > 0 && after.Speed.UploadMbps > 0 {
			pct := (after.Speed.UploadMbps - before.Speed.UploadMbps) * 100.0 / before.Speed.UploadMbps
			delta = fmt.Sprintf("%+.0f%% (%+.1f Mbps)", pct, after.Speed.UploadMbps-before.Speed.UploadMbps)
		}
		rep.Metrics = append(rep.Metrics, DiffMetric{
			Metric: "Uplink",
			Before: fmt.Sprintf("%.1f Mbps", before.Speed.UploadMbps),
			After:  fmt.Sprintf("%.1f Mbps", after.Speed.UploadMbps),
			Delta:  delta,
		})
	}

	if before.Speed.RPM > 0 || after.Speed.RPM > 0 {
		rep.Metrics = append(rep.Metrics, DiffMetric{
			Metric: "Loaded Responsiveness",
			Before: fmt.Sprintf("%d RPM (%.0f ms)", before.Speed.RPM, before.Speed.LoadedRTTMs),
			After:  fmt.Sprintf("%d RPM (%.0f ms)", after.Speed.RPM, after.Speed.LoadedRTTMs),
			Delta:  fmt.Sprintf("%+d RPM (%+.0f ms)", after.Speed.RPM-before.Speed.RPM, after.Speed.LoadedRTTMs-before.Speed.LoadedRTTMs),
		})
	}

	if before.GatewayPing.Received > 0 || after.GatewayPing.Received > 0 {
		rep.Metrics = append(rep.Metrics, DiffMetric{
			Metric: "Router Ping (avg / jitter)",
			Before: fmt.Sprintf("%.1f ms / %.1f ms", before.GatewayPing.AvgMs, before.GatewayPing.JitterMs),
			After:  fmt.Sprintf("%.1f ms / %.1f ms", after.GatewayPing.AvgMs, after.GatewayPing.JitterMs),
			Delta:  fmt.Sprintf("%+.1f ms / %+.1f ms", after.GatewayPing.AvgMs-before.GatewayPing.AvgMs, after.GatewayPing.JitterMs-before.GatewayPing.JitterMs),
		})
	}

	if before.WANPing.Received > 0 || after.WANPing.Received > 0 {
		rep.Metrics = append(rep.Metrics, DiffMetric{
			Metric: "WAN Ping (avg / max)",
			Before: fmt.Sprintf("%.1f ms / %.1f ms", before.WANPing.AvgMs, before.WANPing.MaxMs),
			After:  fmt.Sprintf("%.1f ms / %.1f ms", after.WANPing.AvgMs, after.WANPing.MaxMs),
			Delta:  fmt.Sprintf("%+.1f ms / %+.1f ms", after.WANPing.AvgMs-before.WANPing.AvgMs, after.WANPing.MaxMs-before.WANPing.MaxMs),
		})
	}

	if before.DNS.System.UncachedMs > 0 || after.DNS.System.UncachedMs > 0 {
		rep.Metrics = append(rep.Metrics, DiffMetric{
			Metric: "System DNS (uncached)",
			Before: fmt.Sprintf("%.1f ms", before.DNS.System.UncachedMs),
			After:  fmt.Sprintf("%.1f ms", after.DNS.System.UncachedMs),
			Delta:  fmt.Sprintf("%+.1f ms", after.DNS.System.UncachedMs-before.DNS.System.UncachedMs),
		})
	}

	return rep
}

func (d DiffReport) Markdown() string {
	var b strings.Builder
	b.WriteString("| Metric | Before | After | Change |\n")
	b.WriteString("|---|---|---|---|\n")
	for _, m := range d.Metrics {
		b.WriteString(fmt.Sprintf("| **%s** | %s | **%s** | %s |\n", m.Metric, m.Before, m.After, m.Delta))
	}
	return b.String()
}
