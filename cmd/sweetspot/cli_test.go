package main

import (
	"encoding/json"
	"flag"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bvolpato/sweetspot/internal/advisor"
	"github.com/bvolpato/sweetspot/internal/probe"
	"github.com/bvolpato/sweetspot/internal/spots"
	"github.com/bvolpato/sweetspot/internal/wifi"
)

func TestSeverityJSONMarshalsAsString(t *testing.T) {
	a := advisor.Advice{Severity: advisor.SeverityCritical, Category: "Test", Title: "Critical issue"}
	raw, err := json.Marshal(a)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}
	if !strings.Contains(string(raw), `"severity":"CRITICAL"`) {
		t.Fatalf("expected string severity in JSON, got %s", string(raw))
	}
	var decoded advisor.Advice
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}
	if decoded.Severity != advisor.SeverityCritical {
		t.Fatalf("round-trip severity mismatch: %v", decoded.Severity)
	}
}

func TestComputeDiffMarkdown(t *testing.T) {
	before := DiagnosticReport{
		Score:       advisor.ScoreBreakdown{Total: 64, Grade: "D"},
		Link:        wifi.LinkInfo{Connected: true, Band: wifi.Band5GHz, Channel: 36, WidthMHz: 40, RSSI: -41, SNR: 50, TxRateMbps: 573},
		Speed:       probe.SpeedResult{DownloadMbps: 216.4, UploadMbps: 30.1, RPM: 394, LoadedRTTMs: 152},
		GatewayPing: probe.PingStats{Received: 20, AvgMs: 5.5, JitterMs: 1.2},
		WANPing:     probe.PingStats{Received: 20, AvgMs: 20.3, MaxMs: 67.5},
	}
	after := DiagnosticReport{
		Score:       advisor.ScoreBreakdown{Total: 91, Grade: "A"},
		Link:        wifi.LinkInfo{Connected: true, Band: wifi.Band5GHz, Channel: 48, WidthMHz: 80, RSSI: -34, SNR: 56, TxRateMbps: 1080},
		Speed:       probe.SpeedResult{DownloadMbps: 669.6, UploadMbps: 45.1, RPM: 542, LoadedRTTMs: 110},
		GatewayPing: probe.PingStats{Received: 20, AvgMs: 5.0, JitterMs: 0.9},
		WANPing:     probe.PingStats{Received: 20, AvgMs: 16.9, MaxMs: 24.1},
	}

	diff := ComputeDiff(before, after)
	if diff.ScoreDelta != 27 {
		t.Fatalf("expected score delta +27, got %d", diff.ScoreDelta)
	}
	md := diff.Markdown()
	for _, want := range []string{"Downlink", "669.6 Mbps", "3.09x", "Ch 48 · 80 MHz", "+27 pts"} {
		if !strings.Contains(md, want) {
			t.Errorf("expected %q in diff markdown:\n%s", want, md)
		}
	}
}

func TestPersistentSpotStoreRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "spots.json")
	s1 := spots.OpenStore(path)
	s1.Pin("Kitchen", wifi.LinkInfo{Connected: true, Band: wifi.Band5GHz, Channel: 48, WidthMHz: 80, RSSI: -68, SNR: 24},
		probe.PingStats{AvgMs: 8}, probe.PingStats{AvgMs: 22}, probe.SpeedResult{}, advisor.ScoreBreakdown{Total: 71, Grade: "C"})
	s1.Pin("Desk", wifi.LinkInfo{Connected: true, Band: wifi.Band5GHz, Channel: 48, WidthMHz: 80, RSSI: -40, SNR: 52},
		probe.PingStats{AvgMs: 4}, probe.PingStats{AvgMs: 15}, probe.SpeedResult{DownloadMbps: 650, UploadMbps: 44}, advisor.ScoreBreakdown{Total: 94, Grade: "A+"})

	s2 := spots.OpenStore(path)
	ranked := s2.Ranked()
	if len(ranked) != 2 {
		t.Fatalf("expected 2 persisted spots, got %d", len(ranked))
	}
	if ranked[0].Name != "Desk" || ranked[1].Name != "Kitchen" {
		t.Fatalf("unexpected ranking after reload: %+v", ranked)
	}
	if removed := s2.DeleteByName("kitchen"); removed != 1 {
		t.Fatalf("expected 1 removed spot, got %d", removed)
	}
	if s3 := spots.OpenStore(path); s3.Len() != 1 {
		t.Fatalf("expected 1 spot after delete persistence, got %d", s3.Len())
	}
}

func TestReorderFlagsAllowsTrailingFlagsAfterPositionals(t *testing.T) {
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	asJSON := fs.Bool("json", false, "")
	format := fs.String("format", "text", "")
	args := reorderFlags([]string{"before.json", "after.json", "--format", "markdown", "--json"}, fs)
	if err := fs.Parse(args); err != nil {
		t.Fatalf("parse failed: %v", err)
	}
	if !*asJSON || *format != "markdown" {
		t.Fatalf("expected json=true format=markdown, got json=%v format=%q", *asJSON, *format)
	}
	if fs.NArg() != 2 || fs.Arg(0) != "before.json" || fs.Arg(1) != "after.json" {
		t.Fatalf("unexpected positionals: %v", fs.Args())
	}
}
