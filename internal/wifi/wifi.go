package wifi

import (
	"fmt"
	"sort"
	"time"
)

// Band represents a Wi-Fi frequency band.
type Band string

const (
	BandUnknown Band = "Unknown"
	Band2GHz    Band = "2.4 GHz"
	Band5GHz    Band = "5 GHz"
	Band6GHz    Band = "6 GHz"
)

// LinkInfo holds a single zero-stutter snapshot of the active Wi-Fi and route state.
type LinkInfo struct {
	Timestamp     time.Time `json:"timestamp"`
	Interface     string    `json:"interface"`
	DefaultIface  string    `json:"default_interface"`
	IsEthernet    bool      `json:"is_ethernet"`
	Privileged    bool      `json:"privileged"`
	Gateway       string    `json:"gateway"`
	PowerOn       bool      `json:"power_on"`
	Connected     bool      `json:"connected"`
	SSID          string    `json:"ssid,omitempty"`
	BSSID         string    `json:"bssid,omitempty"`
	RSSI          int       `json:"rssi_dbm"`
	Noise         int       `json:"noise_dbm"`
	SNR           int       `json:"snr_db"`
	TxRateMbps    float64   `json:"tx_rate_mbps"`
	RxRateMbps    float64   `json:"rx_rate_mbps,omitempty"`
	EffDownMbps   float64   `json:"eff_down_mbps,omitempty"`
	EffUpMbps     float64   `json:"eff_up_mbps,omitempty"`
	LinkQuality   int       `json:"link_quality,omitempty"`
	Scheduler     string    `json:"scheduler,omitempty"`
	QoSMode       string    `json:"qos_mode,omitempty"`
	Channel       int       `json:"channel"`
	Band          Band      `json:"band"`
	WidthMHz      int       `json:"width_mhz"`
	PHYMode       string    `json:"phy_mode"`
	Security      string    `json:"security"`
	CountryCode   string    `json:"country_code,omitempty"`
	MCS           int       `json:"mcs,omitempty"`
	HasMCS        bool      `json:"has_mcs,omitempty"`
	NSS           int       `json:"nss,omitempty"`
	GuardInterval int       `json:"guard_interval_ns,omitempty"`
	CCAPct        int       `json:"cca_pct,omitempty"`
	HasCCA        bool      `json:"has_cca,omitempty"`
	TxRetriesPct  float64   `json:"tx_retries_pct,omitempty"`
	DNSServers    []string  `json:"dns_servers,omitempty"`
	InBytes       uint64    `json:"in_bytes"`
	OutBytes      uint64    `json:"out_bytes"`
	InErrors      uint64    `json:"in_errors"`
	OutErrors     uint64    `json:"out_errors"`
	LiveRxMbps    float64   `json:"live_rx_mbps"`
	LiveTxMbps    float64   `json:"live_tx_mbps"`
	AirDropActive bool      `json:"airdrop_active"`
}

// NeighborAP describes a nearby Wi-Fi access point from an RF scan.
type NeighborAP struct {
	SSID     string `json:"ssid,omitempty"`
	BSSID    string `json:"bssid,omitempty"`
	Channel  int    `json:"channel"`
	Band     Band   `json:"band"`
	WidthMHz int    `json:"width_mhz"`
	RSSI     int    `json:"rssi_dbm"`
	Noise    int    `json:"noise_dbm"`
	Security string `json:"security,omitempty"`
}

// ChannelBucket aggregates nearby AP contention on a single channel and band.
type ChannelBucket struct {
	Band          Band `json:"band"`
	Channel       int  `json:"channel"`
	Count         int  `json:"count"`
	MaxRSSI       int  `json:"max_rssi_dbm"`
	MaxWidthMHz   int  `json:"max_width_mhz"`
	IsCurrent     bool `json:"is_current"`
	IsRecommended bool `json:"is_recommended"`
}

// ScanResult holds the parsed neighborhood RF scan.
type ScanResult struct {
	Timestamp         time.Time       `json:"timestamp"`
	Duration          time.Duration   `json:"duration"`
	TotalAPs          int             `json:"total_aps"`
	Neighbors         []NeighborAP    `json:"neighbors"`
	Buckets           []ChannelBucket `json:"buckets"`
	Has6GHzNearby     bool            `json:"has_6ghz_nearby"`
	Best5GHzChannel   int             `json:"best_5ghz_channel"`
	Best5GHzNonDFS    int             `json:"best_5ghz_non_dfs_channel"`
	Best2GHzChannel   int             `json:"best_2ghz_channel"`
	CurrentOverlapAPs int             `json:"current_overlap_aps"`
	Err               string          `json:"error,omitempty"`
}

// ChannelLabel returns a compact label such as "5 GHz · Ch 48 · 80 MHz".
func (l LinkInfo) ChannelLabel() string {
	if l.IsEthernet {
		return fmt.Sprintf("Ethernet (%s)", l.DefaultIface)
	}
	if !l.Connected {
		return "Disconnected"
	}
	if l.WidthMHz > 0 {
		return fmt.Sprintf("%s · Ch %d · %d MHz", l.Band, l.Channel, l.WidthMHz)
	}
	return fmt.Sprintf("%s · Ch %d", l.Band, l.Channel)
}

// QualityLabel returns a human-readable RF signal quality tier.
func (l LinkInfo) QualityLabel() string {
	if l.IsEthernet {
		return "Wired"
	}
	if !l.Connected {
		return "Offline"
	}
	switch {
	case l.RSSI >= -50 && l.SNR >= 35:
		return "Excellent"
	case l.RSSI >= -60 && l.SNR >= 28:
		return "Very Good"
	case l.RSSI >= -67 && l.SNR >= 22:
		return "Good"
	case l.RSSI >= -75 && l.SNR >= 15:
		return "Fair"
	default:
		return "Weak"
	}
}

// SummarizeScan builds channel buckets and picks the cleanest 2.4 GHz and 5 GHz channels.
func SummarizeScan(neighbors []NeighborAP, current LinkInfo, duration time.Duration) ScanResult {
	res := ScanResult{
		Timestamp: time.Now(),
		Duration:  duration,
		TotalAPs:  len(neighbors),
		Neighbors: neighbors,
	}

	type key struct {
		band Band
		ch   int
	}
	buckets := map[key]*ChannelBucket{}
	for _, ap := range neighbors {
		if ap.Band == Band6GHz {
			res.Has6GHzNearby = true
		}
		k := key{band: ap.Band, ch: ap.Channel}
		b, ok := buckets[k]
		if !ok {
			b = &ChannelBucket{
				Band:    ap.Band,
				Channel: ap.Channel,
				MaxRSSI: ap.RSSI,
			}
			buckets[k] = b
		}
		b.Count++
		if ap.RSSI != 0 && (b.MaxRSSI == 0 || ap.RSSI > b.MaxRSSI) {
			b.MaxRSSI = ap.RSSI
		}
		if ap.WidthMHz > b.MaxWidthMHz {
			b.MaxWidthMHz = ap.WidthMHz
		}
	}

	if current.Connected && current.Channel > 0 {
		ck := key{band: current.Band, ch: current.Channel}
		if b, ok := buckets[ck]; ok {
			b.IsCurrent = true
		} else {
			buckets[ck] = &ChannelBucket{
				Band:        current.Band,
				Channel:     current.Channel,
				Count:       0,
				MaxRSSI:     current.RSSI,
				MaxWidthMHz: current.WidthMHz,
				IsCurrent:   true,
			}
		}
		res.CurrentOverlapAPs = countOverlappingAPs(neighbors, current.Band, current.Channel)
	}

	best2 := 1
	best2Score := 1 << 30
	for _, ch := range []int{1, 6, 11} {
		score := countOverlappingAPs(neighbors, Band2GHz, ch)
		if score < best2Score {
			best2Score = score
			best2 = ch
		}
	}
	res.Best2GHzChannel = best2

	nonDFS5 := []int{36, 40, 44, 48, 149, 153, 157, 161}
	all5 := []int{36, 40, 44, 48, 52, 56, 60, 64, 100, 104, 108, 112, 116, 120, 124, 128, 132, 136, 140, 144, 149, 153, 157, 161}

	pickBest5 := func(candidates []int) int {
		best := candidates[0]
		bestScore := 1 << 30
		for _, ch := range candidates {
			primary := 0
			for _, ap := range neighbors {
				if ap.Band == Band5GHz && ap.Channel == ch {
					primary++
				}
			}
			overlap := countOverlappingAPs(neighbors, Band5GHz, ch)
			score := overlap*10 + primary*4
			if score < bestScore {
				bestScore = score
				best = ch
			}
		}
		return best
	}
	res.Best5GHzNonDFS = pickBest5(nonDFS5)
	res.Best5GHzChannel = pickBest5(all5)

	out := make([]ChannelBucket, 0, len(buckets))
	for _, b := range buckets {
		if b.Band == Band5GHz && (b.Channel == res.Best5GHzChannel || b.Channel == res.Best5GHzNonDFS) {
			b.IsRecommended = true
		}
		if b.Band == Band2GHz && b.Channel == res.Best2GHzChannel {
			b.IsRecommended = true
		}
		out = append(out, *b)
	}

	bandOrder := map[Band]int{Band6GHz: 0, Band5GHz: 1, Band2GHz: 2, BandUnknown: 3}
	sort.Slice(out, func(i, j int) bool {
		if bandOrder[out[i].Band] != bandOrder[out[j].Band] {
			return bandOrder[out[i].Band] < bandOrder[out[j].Band]
		}
		return out[i].Channel < out[j].Channel
	})
	res.Buckets = out
	return res
}

func countOverlappingAPs(neighbors []NeighborAP, band Band, ch int) int {
	total := 0
	for _, ap := range neighbors {
		if ap.Band != band {
			continue
		}
		switch band {
		case Band2GHz:
			diff := ap.Channel - ch
			if diff < 0 {
				diff = -diff
			}
			if diff <= 3 {
				total++
			}
		case Band5GHz:
			if same80MHzBlock5GHz(ap.Channel, ch) {
				total++
			}
		case Band6GHz:
			diff := ap.Channel - ch
			if diff < 0 {
				diff = -diff
			}
			if diff <= 12 {
				total++
			}
		}
	}
	return total
}

func same80MHzBlock5GHz(a, b int) bool {
	block := func(ch int) int {
		switch {
		case ch >= 36 && ch <= 48:
			return 1
		case ch >= 52 && ch <= 64:
			return 2
		case ch >= 100 && ch <= 112:
			return 3
		case ch >= 116 && ch <= 128:
			return 4
		case ch >= 132 && ch <= 144:
			return 5
		case ch >= 149 && ch <= 161:
			return 6
		default:
			return ch
		}
	}
	return block(a) == block(b)
}
