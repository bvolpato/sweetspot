package spots

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/bvolpato/sweetspot/internal/advisor"
	"github.com/bvolpato/sweetspot/internal/probe"
	"github.com/bvolpato/sweetspot/internal/wifi"
)

// Spot stores a point-in-time benchmark snapshot for a physical location.
type Spot struct {
	ID           int                    `json:"id"`
	Name         string                 `json:"name"`
	Timestamp    time.Time              `json:"timestamp"`
	Score        advisor.ScoreBreakdown `json:"score"`
	ChannelLabel string                 `json:"channel_label"`
	RSSI         int                    `json:"rssi_dbm"`
	Noise        int                    `json:"noise_dbm"`
	SNR          int                    `json:"snr_db"`
	TxRateMbps   float64                `json:"tx_rate_mbps"`
	GatewayAvgMs float64                `json:"gateway_avg_ms"`
	GatewayJitMs float64                `json:"gateway_jitter_ms"`
	WANAvgMs     float64                `json:"wan_avg_ms"`
	LossPct      float64                `json:"loss_pct"`
	DownloadMbps float64                `json:"download_mbps"`
	UploadMbps   float64                `json:"upload_mbps"`
	LoadedRPM    int                    `json:"loaded_rpm"`
	BloatDeltaMs float64                `json:"bloat_delta_ms"`
}

// Store keeps pinned spots in memory and ranks them by SweetSpot score.
type Store struct {
	nextID int
	items  []Spot
}

// NewStore creates an empty spot store.
func NewStore() *Store {
	return &Store{nextID: 1}
}

// Pin records a new spot snapshot and returns it.
func (s *Store) Pin(
	name string,
	link wifi.LinkInfo,
	gw probe.PingStats,
	wan probe.PingStats,
	speed probe.SpeedResult,
	score advisor.ScoreBreakdown,
) Spot {
	name = strings.TrimSpace(name)
	if name == "" {
		name = fmt.Sprintf("Spot #%d", s.nextID)
	}
	loss := gw.LossPct
	if wan.LossPct > loss {
		loss = wan.LossPct
	}
	spot := Spot{
		ID:           s.nextID,
		Name:         name,
		Timestamp:    time.Now(),
		Score:        score,
		ChannelLabel: link.ChannelLabel(),
		RSSI:         link.RSSI,
		Noise:        link.Noise,
		SNR:          link.SNR,
		TxRateMbps:   link.TxRateMbps,
		GatewayAvgMs: gw.AvgMs,
		GatewayJitMs: gw.JitterMs,
		WANAvgMs:     wan.AvgMs,
		LossPct:      loss,
		DownloadMbps: speed.DownloadMbps,
		UploadMbps:   speed.UploadMbps,
		LoadedRPM:    speed.RPM,
		BloatDeltaMs: speed.BloatDeltaMs,
	}
	s.nextID++
	s.items = append(s.items, spot)
	return spot
}

// Delete removes the spot at ranked index idx.
func (s *Store) Delete(id int) {
	for i := range s.items {
		if s.items[i].ID == id {
			s.items = append(s.items[:i], s.items[i+1:]...)
			return
		}
	}
}

// Ranked returns all spots sorted from highest SweetSpot score to lowest.
func (s *Store) Ranked() []Spot {
	out := make([]Spot, len(s.items))
	copy(out, s.items)
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Score.Total != out[j].Score.Total {
			return out[i].Score.Total > out[j].Score.Total
		}
		return out[i].SNR > out[j].SNR
	})
	return out
}

// Len returns the number of pinned spots.
func (s *Store) Len() int { return len(s.items) }

// ExportMarkdown writes a shareable Markdown report with ranked spots and recommendations.
func ExportMarkdown(path string, ranked []Spot, advice []advisor.Advice) error {
	var b strings.Builder
	b.WriteString("# SweetSpot Wi-Fi Report\n\n")
	b.WriteString(fmt.Sprintf("Generated: %s\n\n", time.Now().Format(time.RFC3339)))

	if len(ranked) > 0 {
		b.WriteString("## Spot Leaderboard\n\n")
		b.WriteString("| Rank | Spot | Score | Channel | RSSI / SNR | PHY | Router ms | WAN ms | Down / Up Mbps | RPM |\n")
		b.WriteString("|---:|---|---:|---|---:|---:|---:|---:|---:|---:|\n")
		for i, sp := range ranked {
			spd := "-"
			if sp.DownloadMbps > 0 {
				spd = fmt.Sprintf("%.1f / %.1f", sp.DownloadMbps, sp.UploadMbps)
			}
			rpm := "-"
			if sp.LoadedRPM > 0 {
				rpm = fmt.Sprintf("%d", sp.LoadedRPM)
			}
			b.WriteString(fmt.Sprintf("| #%d | %s | **%d (%s)** | %s | %d dBm / %d dB | %.0f Mbps | %.1f ms | %.1f ms | %s | %s |\n",
				i+1, sp.Name, sp.Score.Total, sp.Score.Grade, sp.ChannelLabel, sp.RSSI, sp.SNR, sp.TxRateMbps,
				sp.GatewayAvgMs, sp.WANAvgMs, spd, rpm))
		}
		b.WriteString("\n")
	}

	if len(advice) > 0 {
		b.WriteString("## Recommendations\n\n")
		for _, a := range advice {
			b.WriteString(fmt.Sprintf("- **[%s] %s** (%s): %s %s\n", a.Severity, a.Title, a.Category, a.Detail, a.Action))
		}
	}
	return os.WriteFile(path, []byte(b.String()), 0o644)
}

// ExportJSON writes the full structured snapshot to disk.
func ExportJSON(path string, payload any) error {
	data, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}
