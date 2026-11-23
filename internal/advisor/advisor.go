package advisor

import (
	"fmt"
	"sort"
	"strings"

	"github.com/bvolpato/sweetspot/internal/probe"
	"github.com/bvolpato/sweetspot/internal/wifi"
)

// Severity orders diagnostic recommendations from most urgent to informational.
type Severity int

const (
	SeverityCritical Severity = iota
	SeverityHigh
	SeverityTip
	SeverityGood
)

func (s Severity) String() string {
	switch s {
	case SeverityCritical:
		return "CRITICAL"
	case SeverityHigh:
		return "HIGH"
	case SeverityTip:
		return "TIP"
	default:
		return "GOOD"
	}
}

// Advice represents one actionable, evidence-backed recommendation.
type Advice struct {
	Severity Severity `json:"severity"`
	Category string   `json:"category"`
	Title    string   `json:"title"`
	Detail   string   `json:"detail"`
	Action   string   `json:"action"`
}

// Diagnose evaluates live Wi-Fi, latency, throughput, RF scan, DNS, and optional modem stats.
func Diagnose(
	link wifi.LinkInfo,
	gw probe.PingStats,
	wan probe.PingStats,
	speed probe.SpeedResult,
	scan wifi.ScanResult,
	dns probe.DNSRaceResult,
	modem ModemReport,
) []Advice {
	var out []Advice

	if !link.Connected && !link.IsEthernet {
		return []Advice{{
			Severity: SeverityCritical,
			Category: "Link",
			Title:    "No active Wi-Fi or Ethernet link",
			Detail:   "Enable Wi-Fi or connect an Ethernet cable to begin live measurements.",
			Action:   "Check System Settings -> Wi-Fi or network cable.",
		}}
	}

	isEnterprise := strings.Contains(strings.ToLower(link.Security), "enterprise")

	// 1. Signal strength & SNR (Spot Finder advice)
	if !link.IsEthernet {
		switch {
		case link.RSSI < -75 || (link.SNR > 0 && link.SNR < 15):
			out = append(out, Advice{
				Severity: SeverityCritical,
				Category: "Spot / RF",
				Title:    fmt.Sprintf("Weak Wi-Fi signal (%d dBm, %d dB SNR)", link.RSSI, link.SNR),
				Detail:   "Low signal forces lower MCS modulation, retries, and jitter spikes.",
				Action:   "Move closer to an access point or walk with sweetspot open to find a spot >= -60 dBm.",
			})
		case link.RSSI < -65 || (link.SNR > 0 && link.SNR < 24):
			out = append(out, Advice{
				Severity: SeverityHigh,
				Category: "Spot / RF",
				Title:    fmt.Sprintf("Moderate signal (%d dBm, %d dB SNR)", link.RSSI, link.SNR),
				Detail:   "Usable for browsing, but prone to rate drops when others share the channel.",
				Action:   "Try toggling Wi-Fi off/on to drop a sticky far AP, or pin a closer spot (>= -55 dBm).",
			})
		default:
			out = append(out, Advice{
				Severity: SeverityGood,
				Category: "Spot / RF",
				Title:    fmt.Sprintf("Strong RF link (%d dBm, %d dB SNR, %.0f Mbps PHY)", link.RSSI, link.SNR, link.TxRateMbps),
				Detail:   "Signal strength is not your bottleneck at this spot.",
				Action:   "Pin this location with [Space] as a baseline spot.",
			})
		}
	}

	// 2. Band & Channel Width Bottleneck (the classic 2.4 GHz or 40 MHz -> 80 MHz fix!)
	if !link.IsEthernet {
		if link.Band == wifi.Band2GHz {
			out = append(out, Advice{
				Severity: SeverityCritical,
				Category: "Router / Band",
				Title:    fmt.Sprintf("Connected on 2.4 GHz (Ch %d)", link.Channel),
				Detail:   "2.4 GHz has narrow channels and heavy apartment/office interference.",
				Action:   "Disable Smart Connect / band steering on your router and join a dedicated 5 GHz or 6 GHz SSID.",
			})
			if link.Channel != 1 && link.Channel != 6 && link.Channel != 11 && link.Channel > 0 {
				best2 := scan.Best2GHzChannel
				if best2 == 0 {
					best2 = 11
				}
				out = append(out, Advice{
					Severity: SeverityHigh,
					Category: "Router / RF",
					Title:    fmt.Sprintf("Overlapping 2.4 GHz channel (%d)", link.Channel),
					Detail:   "Only channels 1, 6, and 11 are non-overlapping on 2.4 GHz.",
					Action:   fmt.Sprintf("Move 2.4 GHz to Channel %d and keep 20/40 MHz coexistence enabled.", best2),
				})
			}
		} else if link.Band == wifi.Band5GHz && link.WidthMHz > 0 && link.WidthMHz <= 40 {
			if isEnterprise && link.WidthMHz == 20 {
				action := "Enterprise APs use 20 MHz by design for floor density; use desk Ethernet if available, or avoid concurrent bulk uploads during calls."
				if scan.Has6GHzNearby {
					action = "6 GHz APs are visible nearby (40+ MHz). Check if a WPA3 / 6 GHz corporate SSID exists, or use desk Ethernet."
				}
				out = append(out, Advice{
					Severity: SeverityTip,
					Category: "Office Wi-Fi",
					Title:    fmt.Sprintf("Enterprise 5 GHz is 20 MHz wide (%.0f Mbps PHY ceiling)", link.TxRateMbps),
					Detail:   "A 20 MHz 802.11ax channel caps real-world throughput around ~140-170 Mbps and queues quickly under multi-flow load.",
					Action:   action,
				})
			} else {
				targetCh := ""
				if scan.Best5GHzNonDFS > 0 {
					targetCh = fmt.Sprintf(" and move to Channel %d (or DFS Channel %d if supported)", scan.Best5GHzNonDFS, scan.Best5GHzChannel)
				}
				out = append(out, Advice{
					Severity: SeverityHigh,
					Category: "Router / Width",
					Title:    fmt.Sprintf("5 GHz capped at %d MHz (%.0f Mbps PHY)", link.WidthMHz, link.TxRateMbps),
					Detail:   "Running 40 MHz cuts your Wi-Fi throughput roughly in half compared to 80 MHz.",
					Action:   fmt.Sprintf("In your router's 5 GHz Wireless Settings, set Mode to 80 MHz (e.g., Up to 1801+ Mbps / HE80) with AX + OFDMA enabled%s.", targetCh),
				})
			}
		}
	}

	// 3. RF Channel Congestion from scan
	if scan.TotalAPs > 0 && !link.IsEthernet {
		if link.Band == wifi.Band5GHz && scan.CurrentOverlapAPs >= 6 && !isEnterprise {
			best := scan.Best5GHzChannel
			rec := fmt.Sprintf("Switch 5 GHz to DFS Channel %d", best)
			if scan.Best5GHzNonDFS != best {
				rec += fmt.Sprintf(" (or non-DFS Channel %d if your router hides DFS channels)", scan.Best5GHzNonDFS)
			}
			out = append(out, Advice{
				Severity: SeverityHigh,
				Category: "RF Congestion",
				Title:    fmt.Sprintf("Crowded 5 GHz block: %d nearby APs overlap Channel %d", scan.CurrentOverlapAPs, link.Channel),
				Detail:   fmt.Sprintf("Scan found %d total nearby networks; co-channel contention increases Wi-Fi jitter.", scan.TotalAPs),
				Action:   rec + ", and disable unused Guest SSIDs to cut beacon overhead.",
			})
		}
	}

	// 3.5. Channel Airtime Utilization (CCA %) & Spatial Streams (NSS) when available
	if !link.IsEthernet && link.HasCCA {
		if link.CCAPct >= 65 {
			out = append(out, Advice{
				Severity: SeverityCritical,
				Category: "Airtime / CCA",
				Title:    fmt.Sprintf("Channel airtime is %d%% busy (heavily saturated)", link.CCAPct),
				Detail:   "High CCA means other devices or neighboring APs are consuming most of the channel airtime even if your RSSI looks strong.",
				Action:   "Move to a cleaner channel or 6 GHz band, or switch to wired Ethernet if available.",
			})
		} else if link.CCAPct >= 45 {
			out = append(out, Advice{
				Severity: SeverityHigh,
				Category: "Airtime / CCA",
				Title:    fmt.Sprintf("Elevated channel airtime utilization (%d%% CCA)", link.CCAPct),
				Detail:   "Noticeable co-channel contention; expect jitter spikes when nearby devices transmit.",
				Action:   "Run [s] RF Scan to find a less crowded channel block.",
			})
		}
	}
	if !link.IsEthernet && link.NSS == 1 && link.Band != wifi.Band2GHz {
		out = append(out, Advice{
			Severity: SeverityTip,
			Category: "MIMO / Streams",
			Title:    "Connected on 1x1 spatial stream (half of 2x2 MIMO rate)",
			Detail:   "Interference, multipath reflections, or AP limits dropped this link from 2 spatial streams to 1.",
			Action:   "Shift position slightly or face the AP to see if NSS recovers to 2x2.",
		})
	}

	// 4. Packet loss & Wi-Fi hop jitter
	if gw.Received >= 5 {
		if gw.LossPct >= 1.0 {
			out = append(out, Advice{
				Severity: SeverityCritical,
				Category: "Wi-Fi Stability",
				Title:    fmt.Sprintf("%.1f%% packet loss to router (%s)", gw.LossPct, gw.Target),
				Detail:   "Packet loss on the first hop proves the issue is local Wi-Fi interference or weak signal, not your ISP.",
				Action:   "Change Wi-Fi channel, move closer to the AP, or test on Ethernet.",
			})
		} else if gw.JitterMs >= 8.0 || gw.SpikeCount >= 3 {
			action := "Check for neighboring channel overlap or router MU-MIMO quirks."
			if link.AirDropActive {
				action = "Set AirDrop to Receiving Off (stops macOS awdl0 off-channel jumps to Ch 149) and check channel contention."
			}
			out = append(out, Advice{
				Severity: SeverityHigh,
				Category: "Wi-Fi Jitter",
				Title:    fmt.Sprintf("Router hop jitter %.1f ms (max %.1f ms, %d spikes)", gw.JitterMs, gw.MaxMs, gw.SpikeCount),
				Detail:   "Clean 5 GHz Wi-Fi should hold 1-6 ms to the router with <2 ms jitter.",
				Action:   action,
			})
		}
	}

	// 5. Bufferbloat / Loaded responsiveness
	if speed.DownloadMbps > 0 && (speed.BloatDeltaMs >= 50 || (speed.RPM > 0 && speed.RPM < 600)) {
		sev := SeverityHigh
		if speed.BloatDeltaMs >= 150 || (speed.RPM > 0 && speed.RPM < 200) {
			sev = SeverityCritical
		}
		shapeUp := int(speed.UploadMbps * 0.9)
		if shapeUp < 10 {
			shapeUp = 10
		}
		detail := fmt.Sprintf("Loaded latency jumped to %.0f ms (%d RPM) vs %.0f ms idle during %.0f/%.0f Mbps load.",
			speed.LoadedRTTMs, speed.RPM, speed.IdleRTTMs, speed.DownloadMbps, speed.UploadMbps)
		action := fmt.Sprintf("Enable SQM (CAKE or fq_codel) on your router capped to ~90%% of uplink (~%d Mbps). If your combo modem/router lacks SQM, put it in Bridge Mode behind an SQM-capable router.", shapeUp)
		if isEnterprise {
			action = "Shared office AP/WAN queuing under concurrent flows. Avoid running large uploads/Docker pulls during video calls, or offload builds to a remote workspace."
		}
		out = append(out, Advice{
			Severity: sev,
			Category: "Bufferbloat",
			Title:    fmt.Sprintf("Latency bloats +%.0f ms under load (%d RPM)", speed.BloatDeltaMs, speed.RPM),
			Detail:   detail,
			Action:   action,
		})
	}

	// 6. DNS Speedup
	if len(dns.Entries) > 0 && !dns.Fastest.Failed && !dns.System.Failed {
		diff := dns.System.UncachedMs - dns.Fastest.UncachedMs
		if diff >= 10.0 {
			out = append(out, Advice{
				Severity: SeverityTip,
				Category: "DNS",
				Title:    fmt.Sprintf("%s (%s) is %.0f ms faster than System DNS", dns.Fastest.Name, dns.Fastest.Server, diff),
				Detail:   fmt.Sprintf("Measured uncached lookups: %s = %.1f ms vs System DNS = %.1f ms.", dns.Fastest.Server, dns.Fastest.UncachedMs, dns.System.UncachedMs),
				Action:   fmt.Sprintf("Set %s as primary DNS on your router/Wi-Fi (note: corporate DNS proxies like Scout still intercept system lookups).", dns.Fastest.Server),
			})
		}
	}

	// 7. AirDrop / AWDL tip when on non-149 5 GHz channel
	if !link.IsEthernet && link.AirDropActive && link.Band == wifi.Band5GHz && link.Channel != 149 {
		out = append(out, Advice{
			Severity: SeverityTip,
			Category: "macOS Wi-Fi",
			Title:    "AirDrop is enabled on a non-149 5 GHz channel",
			Detail:   "macOS AWDL periodically hops to Channel 149 for AirDrop discovery, which can add 100-200 ms micro-stutters during calls.",
			Action:   "Set AirDrop to Receiving Off (System Settings -> General -> AirDrop & Handoff) if you experience periodic call stutters.",
		})
	}

	// 8. Append cable modem DOCSIS findings if parsed
	if modem.Parsed {
		out = append(out, modem.Findings...)
	}

	sort.SliceStable(out, func(i, j int) bool {
		return out[i].Severity < out[j].Severity
	})
	return out
}
