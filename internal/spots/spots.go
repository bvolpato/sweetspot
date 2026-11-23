package spots

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
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
	BSSID        string                 `json:"bssid,omitempty"`
	RSSI         int                    `json:"rssi_dbm"`
	Noise        int                    `json:"noise_dbm"`
	SNR          int                    `json:"snr_db"`
	TxRateMbps   float64                `json:"tx_rate_mbps"`
	EffDownMbps  float64                `json:"eff_down_mbps,omitempty"`
	LinkQuality  int                    `json:"link_quality,omitempty"`
	CCAPct       int                    `json:"cca_pct,omitempty"`
	HasCCA       bool                   `json:"has_cca,omitempty"`
	GatewayAvgMs float64                `json:"gateway_avg_ms"`
	GatewayJitMs float64                `json:"gateway_jitter_ms"`
	WANAvgMs     float64                `json:"wan_avg_ms"`
	LossPct      float64                `json:"loss_pct"`
	DownloadMbps float64                `json:"download_mbps"`
	UploadMbps   float64                `json:"upload_mbps"`
	LoadedRPM    int                    `json:"loaded_rpm"`
	BloatDeltaMs float64                `json:"bloat_delta_ms"`
}

// Store keeps pinned spots in memory (with optional JSON file persistence) and ranks them.
type Store struct {
	path   string
	nextID int
	items  []Spot
}

type persistedFile struct {
	NextID int    `json:"next_id"`
	Spots  []Spot `json:"spots"`
}

// DefaultPath returns the persistent spot store path (~/.local/share/sweetspot/spots.json).
func DefaultPath() string {
	if dir := strings.TrimSpace(os.Getenv("SWEETSPOT_STATE_DIR")); dir != "" {
		return filepath.Join(dir, "spots.json")
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return ""
	}
	return filepath.Join(home, ".local", "share", "sweetspot", "spots.json")
}

// NewStore creates an in-memory spot store.
func NewStore() *Store {
	return &Store{nextID: 1}
}

// OpenStore loads an existing JSON spot store at path (or returns an empty store wired to path).
func OpenStore(path string) *Store {
	s := &Store{path: path, nextID: 1}
	if path == "" {
		return s
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return s
	}
	var pf persistedFile
	if err := json.Unmarshal(data, &pf); err != nil {
		return s
	}
	s.items = pf.Spots
	s.nextID = pf.NextID
	for _, sp := range s.items {
		if sp.ID >= s.nextID {
			s.nextID = sp.ID + 1
		}
	}
	if s.nextID < 1 {
		s.nextID = 1
	}
	return s
}

func (s *Store) save() {
	if s.path == "" {
		return
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return
	}
	data, err := json.MarshalIndent(persistedFile{
		NextID: s.nextID,
		Spots:  s.items,
	}, "", "  ")
	if err != nil {
		return
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return
	}
	_ = os.Rename(tmp, s.path)
}

// Path returns the backing persistence file path, if configured.
func (s *Store) Path() string { return s.path }

// Pin records a new spot snapshot, persists it if backed by a file, and returns it.
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
		BSSID:        link.BSSID,
		RSSI:         link.RSSI,
		Noise:        link.Noise,
		SNR:          link.SNR,
		TxRateMbps:   link.TxRateMbps,
		EffDownMbps:  link.EffDownMbps,
		LinkQuality:  link.LinkQuality,
		CCAPct:       link.CCAPct,
		HasCCA:       link.HasCCA,
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
	s.save()
	return spot
}

// Delete removes the spot with the given ID.
func (s *Store) Delete(id int) bool {
	for i := range s.items {
		if s.items[i].ID == id {
			s.items = append(s.items[:i], s.items[i+1:]...)
			s.save()
			return true
		}
	}
	return false
}

// DeleteByName removes all spots matching name (case-insensitive).
func (s *Store) DeleteByName(name string) int {
	removed := 0
	kept := s.items[:0]
	for _, sp := range s.items {
		if strings.EqualFold(sp.Name, strings.TrimSpace(name)) {
			removed++
			continue
		}
		kept = append(kept, sp)
	}
	if removed > 0 {
		s.items = kept
		s.save()
	}
	return removed
}

// Clear deletes all saved spots.
func (s *Store) Clear() {
	s.items = nil
	s.nextID = 1
	s.save()
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

// FormatMarkdownTable renders ranked spots as a GitHub-flavored Markdown table.
func FormatMarkdownTable(ranked []Spot) string {
	var b strings.Builder
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
	return b.String()
}

// ExportMarkdown writes a shareable Markdown report with ranked spots and recommendations.
func ExportMarkdown(path string, ranked []Spot, advice []advisor.Advice) error {
	var b strings.Builder
	b.WriteString("# SweetSpot Wi-Fi Report\n\n")
	b.WriteString(fmt.Sprintf("Generated: %s\n\n", time.Now().Format(time.RFC3339)))

	if len(ranked) > 0 {
		b.WriteString("## Spot Leaderboard\n\n")
		b.WriteString(FormatMarkdownTable(ranked))
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
