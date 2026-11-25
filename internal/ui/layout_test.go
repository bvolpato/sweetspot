package ui

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/lipgloss"

	"github.com/bvolpato/sweetspot/internal/advisor"
	"github.com/bvolpato/sweetspot/internal/probe"
	"github.com/bvolpato/sweetspot/internal/spots"
	"github.com/bvolpato/sweetspot/internal/wifi"
)

// layoutModel returns a model with realistic office telemetry and no background probes.
func layoutModel(t *testing.T) *Model {
	t.Helper()
	m := NewModel(Options{WANTarget: "1.1.1.1"})
	t.Cleanup(m.cancel)
	m.spotsStore = spots.NewStore()
	m.link = wifi.LinkInfo{
		Connected: true, Interface: "en0", DefaultIface: "en0", Gateway: "10.43.16.1",
		Band: wifi.Band5GHz, Channel: 132, WidthMHz: 20, RSSI: -49, Noise: -95, SNR: 46,
		TxRateMbps: 286, EffDownMbps: 192, LinkQuality: 100, Scheduler: "FQ_CODEL", QoSMode: "fastlane",
		PHYMode: "802.11ax (Wi-Fi 6)", Security: "WPA3 Enterprise", LiveRxMbps: 2.15, LiveTxMbps: 3.08,
		DNSServers: []string{"1.1.1.1", "1.0.0.1", "8.8.8.8"},
	}
	var rssi, rtt, rx []float64
	for i := 0; i < 220; i++ {
		rssi = append(rssi, float64(-49-i%9))
		rtt = append(rtt, 4+float64(i%7))
		rx = append(rx, float64(i%40)/10)
	}
	m.rssiHist, m.downHist = rssi, rx
	m.gwStats = probe.PingStats{Target: "10.43.16.1", Sent: 220, Received: 220, LastMs: 5, AvgMs: 6.2, P95Ms: 12.9, MaxMs: 17.6, JitterMs: 3.2, History: rtt}
	m.wanStats = probe.PingStats{Target: "1.1.1.1", Sent: 220, Received: 220, LastMs: 12, AvgMs: 11.4, P95Ms: 12.9, MaxMs: 13.7, JitterMs: 1, History: rtt}
	m.recompute()
	return m
}

// layoutStates are the model states that change what a view has to fit.
var layoutStates = []struct {
	name  string
	apply func(*Model)
}{
	{"live", func(m *Model) {}},
	{"zero", func(m *Model) {
		m.link = wifi.LinkInfo{}
		m.gwStats, m.wanStats = probe.PingStats{}, probe.PingStats{}
		m.rssiHist, m.downHist = nil, nil
		m.advice, m.playbook = nil, nil
	}},
	{"ethernet", func(m *Model) {
		m.link = wifi.LinkInfo{IsEthernet: true, DefaultIface: "en7", Interface: "en7"}
	}},
	{"privileged", func(m *Model) {
		m.link.Privileged, m.link.HasCCA, m.link.CCAPct = true, true, 72
		m.link.HasMCS, m.link.MCS, m.link.NSS = true, 11, 2
		m.link.SSID, m.link.BSSID = "a-very-long-network-name-32-chars", "aa:bb:cc:dd:ee:ff"
		m.roamCount = 12
	}},
	{"measured", func(m *Model) {
		m.speed = probe.SpeedResult{Source: "networkQuality", Timestamp: time.Unix(0, 0), DownloadMbps: 1234.5, UploadMbps: 456.7, LoadedRTTMs: 1127, RPM: 53, BloatDeltaMs: 1100}
	}},
	{"busy", func(m *Model) {
		m.speedRunning, m.continuousMode = true, true
		m.speedProgress = probe.SpeedProgress{Phase: "networkQuality", DownloadMbps: 92.1, UploadMbps: 98.9}
		m.toast = "Racing DNS resolvers (System, 1.1.1.1, 9.9.9.9, 8.8.8.8, Router) and waiting for the results..."
	}},
	{"pinning", func(m *Model) { m.pinning = true }},
	{"full", func(m *Model) {
		for i := 0; i < 30; i++ {
			m.spotsStore.Pin(fmt.Sprintf("Conférence room número %d 会議室", i), m.link, m.gwStats, m.wanStats, m.speed, m.score)
		}
		m.selectedIdx = 29
		for i := 0; i < 40; i++ {
			m.scan.Buckets = append(m.scan.Buckets, wifi.ChannelBucket{Band: wifi.Band5GHz, Channel: 36 + i*4, Count: i%7 + 1, MaxRSSI: -60 - i, MaxWidthMHz: 80, IsCurrent: i == 2, IsRecommended: i == 3})
		}
		m.scan.TotalAPs, m.scan.Best5GHzChannel, m.scan.Best5GHzNonDFS, m.scan.Best2GHzChannel = 120, 149, 149, 11
		for _, n := range []string{"System DNS", "Cloudflare", "Quad9", "Google", "Router"} {
			m.dns.Entries = append(m.dns.Entries, probe.DNSBenchmark{Name: n, Server: "2606:4700:4700::1111", CachedMs: 30.8, UncachedMs: 123.9, IsSystem: n == "System DNS"})
		}
		m.dns.System, m.dns.Fastest = m.dns.Entries[0], m.dns.Entries[1]
		m.dns.Fastest.UncachedMs = 20
		m.modem = advisor.ModemReport{Parsed: true, Health: "Watch", Uptime: "112 days 14:22:10", DownstreamCount: 32}
		for i := 0; i < 20; i++ {
			m.advice = append(m.advice, advisor.Advice{Severity: advisor.SeverityTip, Category: "Wi-Fi", Title: "A long finding title that does not fit on a narrow terminal", Detail: strings.Repeat("detail — ", 30), Action: strings.Repeat("action · ", 30)})
		}
	}},
}

// TestViewFitsEveryTerminalSize requires exactly height rows, none wider than the terminal.
// A taller frame makes Bubble Tea drop rows from the top, which hides the header.
func TestViewFitsEveryTerminalSize(t *testing.T) {
	widths := []int{1, 20, 43, 44, 60, 71, 72, 80, 104, 112, 150, 240}
	heights := []int{1, 3, 7, 11, 12, 20, 24, 30, 36, 40, 60}
	for _, st := range layoutStates {
		t.Run(st.name, func(t *testing.T) {
			m := layoutModel(t)
			st.apply(m)
			bad, total := 0, 0
			for _, tab := range []Tab{TabRadar, TabSpots, TabSpectrum, TabAdvisor} {
				m.tab = tab
				for _, w := range widths {
					for _, h := range heights {
						total++
						m.width, m.height = w, h
						lines := strings.Split(m.View(), "\n")
						widest := 0
						for _, l := range lines {
							widest = max(widest, lipgloss.Width(l))
						}
						if len(lines) != h || widest > w {
							if bad++; bad <= 5 {
								t.Errorf("tab %d at %dx%d: got %d rows, widest %d", tab+1, w, h, len(lines), widest)
							}
						}
					}
				}
			}
			if bad > 0 {
				t.Errorf("%d of %d sizes do not fit", bad, total)
			}
		})
	}
}
