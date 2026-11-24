package spots

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bvolpato/sweetspot/internal/advisor"
	"github.com/bvolpato/sweetspot/internal/probe"
	"github.com/bvolpato/sweetspot/internal/wifi"
)

func TestStoreRankingTieBreakAndExport(t *testing.T) {
	dir := t.TempDir()
	st := OpenStore(filepath.Join(dir, "spots.json"))

	st.Pin("", wifi.LinkInfo{Connected: true, Band: wifi.Band5GHz, Channel: 48, WidthMHz: 80, RSSI: -62, SNR: 30},
		probe.PingStats{AvgMs: 7}, probe.PingStats{AvgMs: 18}, probe.SpeedResult{}, advisor.ScoreBreakdown{Total: 80, Grade: "B"})
	st.Pin("Window", wifi.LinkInfo{Connected: true, Band: wifi.Band5GHz, Channel: 48, WidthMHz: 80, RSSI: -50, SNR: 42},
		probe.PingStats{AvgMs: 4}, probe.PingStats{AvgMs: 14}, probe.SpeedResult{DownloadMbps: 520, UploadMbps: 40, RPM: 540}, advisor.ScoreBreakdown{Total: 80, Grade: "B"})

	ranked := st.Ranked()
	if len(ranked) != 2 || ranked[0].Name != "Window" || ranked[1].Name != "Spot #1" {
		t.Fatalf("expected tie-break by SNR (Window first, auto-named Spot #1 second), got %+v", ranked)
	}

	mdPath := filepath.Join(dir, "report.md")
	if err := ExportMarkdown(mdPath, ranked, []advisor.Advice{{Severity: advisor.SeverityGood, Category: "RF", Title: "Strong"}}); err != nil {
		t.Fatalf("ExportMarkdown failed: %v", err)
	}
	raw, err := os.ReadFile(mdPath)
	if err != nil {
		t.Fatalf("read export failed: %v", err)
	}
	if !strings.Contains(string(raw), "| #1 | Window |") || !strings.Contains(string(raw), "[GOOD]") {
		t.Fatalf("unexpected exported markdown:\n%s", string(raw))
	}

	st.Clear()
	if st.Len() != 0 {
		t.Fatalf("expected 0 spots after Clear, got %d", st.Len())
	}
}
