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

// ChecklistItem represents one vetted optimization check in the playbook sidebar.
type ChecklistItem struct {
	Status string // "ok", "warn", "check"
	Title  string
	Note   string
}

// Diagnose evaluates live Wi-Fi, latency, throughput, RF scan, DNS, and optional modem stats.
// Every finding is grounded in live measurements with zero placebo tweaks and no duplicate tips.
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
			Action:   "Check System Settings -> Wi-Fi or verify your Ethernet cable.",
		}}
	}

	isEnterprise := strings.Contains(strings.ToLower(link.Security), "enterprise")
	mentionedAirDrop := false

	// 1. Hardware / interface packet errors
	if link.InErrors > 0 || link.OutErrors > 0 {
		out = append(out, Advice{
			Severity: SeverityHigh,
			Category: "Interface",
			Title:    fmt.Sprintf("Interface errors on %s (%d rx / %d tx)", link.Interface, link.InErrors, link.OutErrors),
			Detail:   "Non-zero interface errors indicate driver drops, adapter faults, or a bad Ethernet cable.",
			Action:   "Test with another cable/port (if wired) or toggle the adapter off and on.",
		})
	}

	// 2. Channel Airtime Utilization (CCA %) -- strongest signal for floor/apartment congestion
	if !link.IsEthernet && link.HasCCA {
		switch {
		case link.CCAPct >= 65:
			action := "Switch your router to a cleaner 5 GHz DFS channel (100-128) or 6 GHz, and turn off unused Guest SSIDs."
			if isEnterprise {
				action = "This AP channel is heavily crowded by nearby laptops. Use wired desk Ethernet if available, walk to another zone/AP, or check if a 6 GHz corporate SSID exists."
			}
			out = append(out, Advice{
				Severity: SeverityCritical,
				Category: "Airtime / CCA",
				Title:    fmt.Sprintf("Channel airtime is %d%% busy (heavily saturated)", link.CCAPct),
				Detail:   "High CCA proves other devices or neighboring APs are consuming the channel even when your RSSI looks good.",
				Action:   action,
			})
		case link.CCAPct >= 45:
			action := "Run [s] RF Scan and move your router to a less crowded 5 GHz channel block."
			if isEnterprise {
				action = "Moderate floor contention on this AP. Avoid large concurrent uploads during calls, or check if a nearby spot has lower CCA."
			}
			out = append(out, Advice{
				Severity: SeverityHigh,
				Category: "Airtime / CCA",
				Title:    fmt.Sprintf("Elevated channel airtime utilization (%d%% CCA)", link.CCAPct),
				Detail:   "Co-channel airtime above 45% causes bursty latency whenever neighboring devices transmit.",
				Action:   action,
			})
		case link.CCAPct < 30:
			out = append(out, Advice{
				Severity: SeverityGood,
				Category: "Airtime / CCA",
				Title:    fmt.Sprintf("Clean channel airtime (%d%% CCA busy)", link.CCAPct),
				Detail:   "Plenty of free airtime on this channel.",
				Action:   "Airtime contention is not limiting you here.",
			})
		}
	}

	// 3. Signal strength, SNR, spatial streams (NSS), and L2 retries
	if !link.IsEthernet {
		switch {
		case link.RSSI < -75 || (link.SNR > 0 && link.SNR < 15):
			out = append(out, Advice{
				Severity: SeverityCritical,
				Category: "Spot / RF",
				Title:    fmt.Sprintf("Weak Wi-Fi signal (%d dBm, %d dB SNR)", link.RSSI, link.SNR),
				Detail:   "Weak signal forces low MCS rates, retries, and drops. Walls, metal, and appliances attenuate 5/6 GHz sharply.",
				Action:   "Walk with sweetspot to find a spot >= -60 dBm, elevate and centralize your router, or add an AP with wired Ethernet/MoCA backhaul.",
			})
		case link.RSSI < -65 || (link.SNR > 0 && link.SNR < 24):
			out = append(out, Advice{
				Severity: SeverityHigh,
				Category: "Spot / RF",
				Title:    fmt.Sprintf("Moderate signal (%d dBm, %d dB SNR)", link.RSSI, link.SNR),
				Detail:   "Usable for browsing, but rate-shifting under load increases jitter.",
				Action:   "Toggle Wi-Fi off/on to release a sticky far AP, or pin a closer spot (>= -55 dBm).",
			})
		default:
			out = append(out, Advice{
				Severity: SeverityGood,
				Category: "Spot / RF",
				Title:    fmt.Sprintf("Strong RF link (%d dBm, %d dB SNR, %.0f Mbps PHY)", link.RSSI, link.SNR, link.TxRateMbps),
				Detail:   "Signal strength is healthy at this spot.",
				Action:   "Pin this location with [Space] as your baseline spot.",
			})
		}

		if link.NSS == 1 && link.Band != wifi.Band2GHz {
			out = append(out, Advice{
				Severity: SeverityTip,
				Category: "MIMO / Streams",
				Title:    "Connected on 1x1 spatial stream (half of 2x2 MIMO rate)",
				Detail:   "Multipath interference or AP limits dropped this link from 2 spatial streams to 1.",
				Action:   "Shift laptop position slightly or face the AP to see if NSS recovers to 2x2.",
			})
		}

		if link.TxRetriesPct >= 15.0 {
			out = append(out, Advice{
				Severity: SeverityHigh,
				Category: "Wi-Fi Retries",
				Title:    fmt.Sprintf("High L2 frame retry rate (%.1f%%)", link.TxRetriesPct),
				Detail:   "Elevated Wi-Fi retries indicate hidden-node interference or marginal SNR.",
				Action:   "Switch to a cleaner channel or move closer to the AP.",
			})
		}
	}

	// 4. Band, Channel Width, Legacy PHY, and Security Bottlenecks
	if !link.IsEthernet {
		if link.Band == wifi.Band2GHz {
			out = append(out, Advice{
				Severity: SeverityCritical,
				Category: "Router / Band",
				Title:    fmt.Sprintf("Connected on 2.4 GHz (Ch %d)", link.Channel),
				Detail:   "2.4 GHz has narrow channels and heavy neighbor/Bluetooth/microwave interference.",
				Action:   "Disable Smart Connect (band steering) on your router and connect to a dedicated 5 GHz or 6 GHz SSID.",
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
					Detail:   "Only channels 1, 6, and 11 are non-overlapping on 2.4 GHz; intermediate channels take interference from both sides.",
					Action:   fmt.Sprintf("Set 2.4 GHz to Channel %d (20 MHz, with 20/40 MHz coexistence enabled).", best2),
				})
			}
		} else if link.Band == wifi.Band5GHz && link.WidthMHz > 0 && link.WidthMHz <= 40 {
			if isEnterprise && link.WidthMHz == 20 {
				action := "Enterprise APs use 20 MHz by design for high-density floors; use wired desk Ethernet if available, or avoid concurrent bulk uploads during calls."
				if scan.Has6GHzNearby {
					action = "6 GHz (40+ MHz) APs are visible nearby. Ask IT if a WPA3 / 6 GHz corporate SSID is available, or use desk Ethernet."
				}
				out = append(out, Advice{
					Severity: SeverityTip,
					Category: "Office Wi-Fi",
					Title:    fmt.Sprintf("Enterprise 5 GHz is 20 MHz wide (%.0f Mbps PHY ceiling)", link.TxRateMbps),
					Detail:   "A 20 MHz Wi-Fi 6 channel tops out around ~140-170 Mbps real-world throughput and queues quickly under multi-flow load.",
					Action:   action,
				})
			} else {
				targetCh := ""
				if scan.Best5GHzNonDFS > 0 {
					targetCh = fmt.Sprintf(" and pick Channel %d (or DFS Channel %d if your router lists DFS)", scan.Best5GHzNonDFS, scan.Best5GHzChannel)
				}
				out = append(out, Advice{
					Severity: SeverityHigh,
					Category: "Router / Width",
					Title:    fmt.Sprintf("5 GHz capped at %d MHz (%.0f Mbps PHY)", link.WidthMHz, link.TxRateMbps),
					Detail:   "Running 40 MHz cuts max 5 GHz throughput roughly in half compared to 80 MHz.",
					Action:   fmt.Sprintf("In router 5 GHz Wireless Settings, set Mode to 80 MHz (e.g., Up to 1801+ Mbps / HE80) with AX, OFDMA, and WMM enabled%s.", targetCh),
				})
			}
		}

		// Legacy PHY or TKIP/WEP/Open security
		lowPHY := strings.ToLower(link.PHYMode)
		if strings.Contains(lowPHY, "802.11a") && !strings.Contains(lowPHY, "ac") && !strings.Contains(lowPHY, "ax") ||
			strings.Contains(lowPHY, "802.11b") || strings.Contains(lowPHY, "802.11g") {
			out = append(out, Advice{
				Severity: SeverityHigh,
				Category: "Router / PHY",
				Title:    fmt.Sprintf("Legacy PHY mode active (%s)", link.PHYMode),
				Detail:   "Legacy 802.11a/b/g modes cap links to 54 Mbps -- often caused by WMM being disabled or WPA-TKIP encryption.",
				Action:   "Enable WMM (Wi-Fi Multimedia), switch security to WPA2-AES or WPA3, and enable 802.11ac/ax on your router.",
			})
		}
		if link.Security == "WEP" || link.Security == "WPA Personal" {
			out = append(out, Advice{
				Severity: SeverityHigh,
				Category: "Security / Speed",
				Title:    fmt.Sprintf("Legacy encryption (%s) limits speed and security", link.Security),
				Detail:   "WEP and WPA-TKIP force clients out of modern 802.11n/ac/ax high-throughput modes.",
				Action:   "Switch router Wi-Fi security to WPA2-Personal [AES] or WPA2/WPA3-Personal.",
			})
		}
	}

	// 5. RF Channel Congestion from explicit scan
	if scan.TotalAPs > 0 && !link.IsEthernet {
		if link.Band == wifi.Band5GHz && scan.CurrentOverlapAPs >= 6 && !isEnterprise {
			best := scan.Best5GHzChannel
			rec := fmt.Sprintf("Move 5 GHz to DFS Channel %d", best)
			if scan.Best5GHzNonDFS != best {
				rec += fmt.Sprintf(" (or non-DFS Channel %d if DFS is hidden)", scan.Best5GHzNonDFS)
			}
			out = append(out, Advice{
				Severity: SeverityHigh,
				Category: "RF Congestion",
				Title:    fmt.Sprintf("Crowded 5 GHz block: %d nearby APs overlap Ch %d", scan.CurrentOverlapAPs, link.Channel),
				Detail:   fmt.Sprintf("Scan found %d total nearby networks; overlapping APs compete for airtime and add jitter.", scan.TotalAPs),
				Action:   rec + ", and disable unused Guest SSIDs to cut beacon overhead.",
			})
		}
		if scan.Has6GHzNearby && link.Band != wifi.Band6GHz && !isEnterprise {
			out = append(out, Advice{
				Severity: SeverityTip,
				Category: "6 GHz / Wi-Fi 6E",
				Title:    "6 GHz band is active nearby with wide, clean channels",
				Detail:   "6 GHz avoids legacy 2.4/5 GHz congestion and offers clean 80/160 MHz channels.",
				Action:   "If your router supports Wi-Fi 6E/7, enable 6 GHz with WPA3 and set macOS Wi-Fi 6E Mode to Automatic.",
			})
		}
	}

	// 6. Dual-Hop Latency, Jitter, and Packet Loss (isolates Wi-Fi vs ISP!)
	if gw.Received >= 5 {
		if gw.LossPct >= 1.0 {
			out = append(out, Advice{
				Severity: SeverityCritical,
				Category: "Wi-Fi Loss",
				Title:    fmt.Sprintf("%.1f%% packet loss to router (%s)", gw.LossPct, gw.Target),
				Detail:   "Loss on the first hop proves the drop is on your local Wi-Fi link, not your ISP.",
				Action:   "Move to a stronger spot, change Wi-Fi channel, or test on Ethernet to confirm.",
			})
		} else if gw.JitterMs >= 8.0 || gw.SpikeCount >= 3 {
			action := "Check for neighboring channel overlap, high CCA airtime, or try toggling router MU-MIMO off (common fix on Apple Silicon Macs)."
			if link.AirDropActive && !link.IsEthernet && link.Band == wifi.Band5GHz && link.Channel != 149 {
				action = "Set AirDrop to Receiving Off (stops macOS awdl0 off-channel hops to Ch 149), check CCA airtime, and test router MU-MIMO off."
				mentionedAirDrop = true
			}
			out = append(out, Advice{
				Severity: SeverityHigh,
				Category: "Wi-Fi Jitter",
				Title:    fmt.Sprintf("Router hop jitter %.1f ms (max %.1f ms, %d spikes)", gw.JitterMs, gw.MaxMs, gw.SpikeCount),
				Detail:   "Clean 5/6 GHz Wi-Fi should hold 1-6 ms to the router with <2 ms jitter.",
				Action:   action,
			})
		}
	}

	if wan.Received >= 5 && wan.LossPct >= 1.0 && gw.LossPct < 0.5 {
		out = append(out, Advice{
			Severity: SeverityCritical,
			Category: "ISP / WAN Loss",
			Title:    fmt.Sprintf("%.1f%% packet loss to %s while router hop is clean", wan.LossPct, wan.Target),
			Detail:   "Zero loss to your router with loss beyond it points to your modem, coax/fiber drop, or ISP congestion.",
			Action:   "Paste your cable modem status page with [m] to check coax power, SNR, and uncorrectable errors.",
		})
	}

	// 7. Bufferbloat & Throughput vs. PHY Gap
	if speed.DownloadMbps > 0 {
		if speed.BloatDeltaMs >= 50 || (speed.RPM > 0 && speed.RPM < 600) {
			sev := SeverityHigh
			if speed.BloatDeltaMs >= 150 || (speed.RPM > 0 && speed.RPM < 200) {
				sev = SeverityCritical
			}
			shapeUp := int(speed.UploadMbps * 0.9)
			if shapeUp < 10 {
				shapeUp = 10
			}
			action := fmt.Sprintf("Enable SQM (CAKE or fq_codel) on your router capped to ~90%% of uplink (~%d Mbps). If your combo modem/router lacks SQM, put it in Bridge Mode behind an SQM router.", shapeUp)
			if isEnterprise {
				action = "Shared office AP/WAN queuing under concurrent flows. Avoid large uploads/Docker pulls during video calls, or offload builds to a remote workspace."
			}
			out = append(out, Advice{
				Severity: sev,
				Category: "Bufferbloat",
				Title:    fmt.Sprintf("Latency bloats +%.0f ms under load (%d RPM)", speed.BloatDeltaMs, speed.RPM),
				Detail:   fmt.Sprintf("Loaded latency reached %.0f ms vs %.0f ms idle during %.0f/%.0f Mbps test.", speed.LoadedRTTMs, speed.IdleRTTMs, speed.DownloadMbps, speed.UploadMbps),
				Action:   action,
			})
		} else if speed.RPM >= 900 && speed.BloatDeltaMs < 30 {
			out = append(out, Advice{
				Severity: SeverityGood,
				Category: "Responsiveness",
				Title:    fmt.Sprintf("Low bufferbloat under load (%d RPM, +%.0f ms)", speed.RPM, speed.BloatDeltaMs),
				Detail:   "Latency stays tight even while transferring data -- great for video calls and SSH.",
				Action:   "No queue-management changes needed.",
			})
		}

		if !link.IsEthernet && link.TxRateMbps >= 700 && speed.DownloadMbps < 140 {
			out = append(out, Advice{
				Severity: SeverityTip,
				Category: "Backhaul / ISP",
				Title:    fmt.Sprintf("Download (%.0f Mbps) is far below Wi-Fi PHY (%.0f Mbps)", speed.DownloadMbps, link.TxRateMbps),
				Detail:   "Your local Wi-Fi hop is fast, so the cap is upstream (ISP speed tier, VPN tunnel, or wireless mesh backhaul).",
				Action:   "Test once on Ethernet at the main router to confirm whether your ISP tier or a wireless mesh node is capping throughput.",
			})
		}
	}

	// 8. DNS Speedup
	if len(dns.Entries) > 0 && !dns.Fastest.Failed && !dns.System.Failed {
		diff := dns.System.UncachedMs - dns.Fastest.UncachedMs
		if diff >= 10.0 {
			out = append(out, Advice{
				Severity: SeverityTip,
				Category: "DNS",
				Title:    fmt.Sprintf("%s (%s) is %.0f ms faster than System DNS", dns.Fastest.Name, dns.Fastest.Server, diff),
				Detail:   fmt.Sprintf("Measured uncached lookups: %s = %.1f ms vs System DNS = %.1f ms.", dns.Fastest.Server, dns.Fastest.UncachedMs, dns.System.UncachedMs),
				Action:   fmt.Sprintf("Set %s primary (with 1.1.1.1 / 9.9.9.9 fallback) on your router or Wi-Fi settings. Corporate DNS proxies (e.g., Scout) still intercept managed laptops.", dns.Fastest.Server),
			})
		}
	}

	// 9. macOS AirDrop / AWDL tip (skip if already mentioned in Wi-Fi Jitter to avoid duplication)
	if !mentionedAirDrop && !link.IsEthernet && link.AirDropActive && link.Band == wifi.Band5GHz && link.Channel != 149 {
		out = append(out, Advice{
			Severity: SeverityTip,
			Category: "macOS Wi-Fi",
			Title:    "AirDrop is enabled on a non-149 5 GHz channel",
			Detail:   "macOS AWDL hops off-channel to Channel 149 for AirDrop discovery, which can add 100-200 ms micro-stutters on calls.",
			Action:   "Set AirDrop to Receiving Off (System Settings -> General -> AirDrop & Handoff) if you see periodic call stutters.",
		})
	}

	// 10. Cable Modem DOCSIS findings
	if modem.Parsed {
		out = append(out, modem.Findings...)
	}

	sort.SliceStable(out, func(i, j int) bool {
		return out[i].Severity < out[j].Severity
	})
	return out
}

// BuildPlaybook returns a vetted, context-aware Wi-Fi optimization checklist tailored to
// whether the user is on Home Wi-Fi, Enterprise/Office Wi-Fi, or Wired Ethernet.
func BuildPlaybook(
	link wifi.LinkInfo,
	gw probe.PingStats,
	speed probe.SpeedResult,
	scan wifi.ScanResult,
	dns probe.DNSRaceResult,
	modem ModemReport,
) []ChecklistItem {
	isEnterprise := strings.Contains(strings.ToLower(link.Security), "enterprise")
	var items []ChecklistItem

	// 1. Signal & placement
	if link.IsEthernet {
		items = append(items, ChecklistItem{Status: "ok", Title: "Wired Ethernet", Note: "Bypassing all wireless contention"})
	} else if link.RSSI >= -60 && link.SNR >= 28 {
		items = append(items, ChecklistItem{Status: "ok", Title: "Signal & SNR", Note: fmt.Sprintf("%d dBm / %d dB SNR (target >= -60 dBm)", link.RSSI, link.SNR)})
	} else {
		items = append(items, ChecklistItem{Status: "warn", Title: "Signal & Placement", Note: "Aim for >= -60 dBm; elevate AP, avoid walls/metal"})
	}

	// 2. Band & Channel Width
	if !link.IsEthernet {
		if link.Band == wifi.Band6GHz || (link.Band == wifi.Band5GHz && link.WidthMHz >= 80) {
			items = append(items, ChecklistItem{Status: "ok", Title: "Band & Width", Note: link.ChannelLabel()})
		} else if isEnterprise {
			items = append(items, ChecklistItem{Status: "check", Title: "Enterprise 20 MHz Cap", Note: "Ask IT about WPA3/6 GHz SSID or use desk Ethernet"})
		} else {
			items = append(items, ChecklistItem{Status: "warn", Title: "Unlock 80 MHz + Split SSIDs", Note: "Disable Smart Connect; set 5 GHz to 80 MHz + AX/OFDMA"})
		}
	}

	// 3. Channel / Airtime (CCA)
	if !link.IsEthernet {
		if link.HasCCA && link.CCAPct >= 45 {
			items = append(items, ChecklistItem{Status: "warn", Title: fmt.Sprintf("Channel Airtime (%d%% CCA)", link.CCAPct), Note: "Saturated channel; change channel or move to less dense AP"})
		} else if scan.TotalAPs > 0 {
			items = append(items, ChecklistItem{Status: "ok", Title: "RF Channel Scan", Note: fmt.Sprintf("Best 5 GHz: Ch %d (non-DFS %d) · 2.4: Ch %d", scan.Best5GHzChannel, scan.Best5GHzNonDFS, scan.Best2GHzChannel)})
		} else {
			items = append(items, ChecklistItem{Status: "check", Title: "Scan Nearby Channels [s]", Note: "Find cleanest 5 GHz DFS/non-DFS & 2.4 GHz (1/6/11)"})
		}
	}

	// 4. Bufferbloat / SQM
	if speed.DownloadMbps == 0 {
		items = append(items, ChecklistItem{Status: "check", Title: "Measure Speed & Bloat [b]", Note: "Check download/upload & loaded RPM responsiveness"})
	} else if speed.BloatDeltaMs >= 50 || (speed.RPM > 0 && speed.RPM < 600) {
		note := "Enable SQM (CAKE/fq_codel) at ~90% uplink (or Bridge Mode)"
		if isEnterprise {
			note = "Avoid concurrent uploads + calls; offload builds to remote host"
		}
		items = append(items, ChecklistItem{Status: "warn", Title: fmt.Sprintf("Bufferbloat (%d RPM)", speed.RPM), Note: note})
	} else {
		items = append(items, ChecklistItem{Status: "ok", Title: "Low Bufferbloat", Note: fmt.Sprintf("%d RPM (+%.0f ms under load)", speed.RPM, speed.BloatDeltaMs)})
	}

	// 5. DNS
	if !dns.Fastest.Failed && !dns.System.Failed && (dns.System.UncachedMs-dns.Fastest.UncachedMs) >= 10 {
		items = append(items, ChecklistItem{Status: "warn", Title: "Faster DNS Available", Note: fmt.Sprintf("Try %s (%s: %.0fms vs %.0fms)", dns.Fastest.Name, dns.Fastest.Server, dns.Fastest.UncachedMs, dns.System.UncachedMs)})
	} else if len(dns.Entries) > 0 {
		items = append(items, ChecklistItem{Status: "ok", Title: "DNS Latency", Note: fmt.Sprintf("System %.0fms · Fastest %.0fms (%s)", dns.System.UncachedMs, dns.Fastest.UncachedMs, dns.Fastest.Server)})
	}

	// 6. Client Wi-Fi hygiene (macOS AirDrop / Private MAC)
	if !link.IsEthernet && link.AirDropActive && link.Band == wifi.Band5GHz && link.Channel != 149 {
		items = append(items, ChecklistItem{Status: "check", Title: "macOS AirDrop / Private MAC", Note: "AirDrop Receiving Off + Fixed MAC on trusted SSIDs"})
	} else if !link.IsEthernet {
		items = append(items, ChecklistItem{Status: "ok", Title: "Client Wi-Fi Hygiene", Note: "Keep Private Wi-Fi Address = Fixed & Low Data Mode Off"})
	}

	// 7. Home-only router & modem checklist
	if !isEnterprise {
		if modem.Parsed {
			st := "ok"
			if modem.Health != "Healthy" {
				st = "warn"
			}
			items = append(items, ChecklistItem{Status: st, Title: "DOCSIS Modem (" + modem.Health + ")", Note: fmt.Sprintf("Down %.1f..%.1f dBmV · SNR %.1f dB · Up %.1f dBmV", modem.DownPowerMin, modem.DownPowerMax, modem.DownSNRMin, modem.UpPowerMax)})
		} else {
			items = append(items, ChecklistItem{Status: "check", Title: "Cable Modem Coax Check [m]", Note: "Copy modem Cable Connection page & press [m]"})
		}
		items = append(items, ChecklistItem{Status: "check", Title: "Mesh / Router Best Practices", Note: "Disable unused Guest SSIDs; wire mesh via Ethernet/MoCA"})
	} else {
		items = append(items, ChecklistItem{Status: "check", Title: "Office Wi-Fi Best Practices", Note: "Toggle Wi-Fi if sticky; report high CCA/low RPM to IT"})
	}

	return items
}
