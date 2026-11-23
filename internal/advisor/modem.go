package advisor

import (
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
)

// ModemReport summarizes parsed DOCSIS 3.0/3.1 cable modem diagnostics.
type ModemReport struct {
	Parsed               bool     `json:"parsed"`
	DownstreamCount      int      `json:"downstream_count"`
	UpstreamCount        int      `json:"upstream_count"`
	OFDMCount            int      `json:"ofdm_count"`
	DownPowerMin         float64  `json:"down_power_min_dbmv"`
	DownPowerMax         float64  `json:"down_power_max_dbmv"`
	DownSNRMin           float64  `json:"down_snr_min_db"`
	DownSNRMax           float64  `json:"down_snr_max_db"`
	UpPowerMin           float64  `json:"up_power_min_dbmv"`
	UpPowerMax           float64  `json:"up_power_max_dbmv"`
	TotalUncorrectables  int64    `json:"total_uncorrectables"`
	ChannelsWithUncorr   int      `json:"channels_with_uncorrectables"`
	UpstreamOFDMAActive  bool     `json:"upstream_ofdma_active"`
	Uptime               string   `json:"uptime,omitempty"`
	Health               string   `json:"health"` // "Healthy", "Watch", "Poor"
	Findings             []Advice `json:"findings"`
}

var (
	reDownQAM   = regexp.MustCompile(`(?i)Locked\s+(?:256|64|1024|4096)?\s*QAM[^\n]*?(-?[0-9]+(?:\.[0-9]+)?)\s*dBmV\s+([0-9]+(?:\.[0-9]+)?)\s*dB(?:\s+([0-9]+)\s+([0-9]+))?`)
	reDownOFDM  = regexp.MustCompile(`(?i)\b\d+\s+\d+\s+[0-9]+\s*Hz\s+(-?[0-9]+(?:\.[0-9]+)?)\s*dBmV\s+([0-9]+(?:\.[0-9]+)?)\s*dB\s+\d+\s*~\s*\d+\s+[0-9]+\s+([0-9]+)`)
	reUpChannel = regexp.MustCompile(`(?i)Locked\s+(?:ATDMA|SC-QAM|TDMA|OFDMA)[^\n]*?(-?[0-9]+(?:\.[0-9]+)?)\s*dBmV`)
	reUptime    = regexp.MustCompile(`(?i)System Up Time:\s*([^\n]+)`)
)

// ParseModemDiagnostics parses pasted cable modem diagnostic pages (NETGEAR CAX/CM, ARRIS, Motorola)
// and generates concrete DOCSIS health findings.
func ParseModemDiagnostics(raw string) ModemReport {
	rep := ModemReport{
		DownPowerMin: math.MaxFloat64,
		DownPowerMax: -math.MaxFloat64,
		DownSNRMin:   math.MaxFloat64,
		DownSNRMax:   -math.MaxFloat64,
		UpPowerMin:   math.MaxFloat64,
		UpPowerMax:   -math.MaxFloat64,
	}

	if m := reUptime.FindStringSubmatch(raw); m != nil {
		rep.Uptime = strings.TrimSpace(m[1])
	}

	for _, line := range strings.Split(raw, "\n") {
		if m := reDownQAM.FindStringSubmatch(line); m != nil {
			pwr, _ := strconv.ParseFloat(m[1], 64)
			snr, _ := strconv.ParseFloat(m[2], 64)
			rep.DownstreamCount++
			updateMinMax(&rep.DownPowerMin, &rep.DownPowerMax, pwr)
			updateMinMax(&rep.DownSNRMin, &rep.DownSNRMax, snr)
			if m[4] != "" {
				uncorr, _ := strconv.ParseInt(m[4], 10, 64)
				if uncorr > 0 {
					rep.TotalUncorrectables += uncorr
					rep.ChannelsWithUncorr++
				}
			}
			continue
		}
		if m := reDownOFDM.FindStringSubmatch(line); m != nil {
			pwr, _ := strconv.ParseFloat(m[1], 64)
			snr, _ := strconv.ParseFloat(m[2], 64)
			uncorr, _ := strconv.ParseInt(m[3], 10, 64)
			rep.OFDMCount++
			updateMinMax(&rep.DownPowerMin, &rep.DownPowerMax, pwr)
			updateMinMax(&rep.DownSNRMin, &rep.DownSNRMax, snr)
			if uncorr > 0 {
				rep.TotalUncorrectables += uncorr
				rep.ChannelsWithUncorr++
			}
			continue
		}
		if m := reUpChannel.FindStringSubmatch(line); m != nil {
			pwr, _ := strconv.ParseFloat(m[1], 64)
			rep.UpstreamCount++
			updateMinMax(&rep.UpPowerMin, &rep.UpPowerMax, pwr)
			if strings.Contains(strings.ToUpper(line), "OFDMA") {
				rep.UpstreamOFDMAActive = true
			}
		}
	}

	if rep.DownstreamCount == 0 && rep.OFDMCount == 0 && rep.UpstreamCount == 0 {
		return rep
	}

	rep.Parsed = true
	if rep.DownPowerMin == math.MaxFloat64 {
		rep.DownPowerMin, rep.DownPowerMax = 0, 0
	}
	if rep.DownSNRMin == math.MaxFloat64 {
		rep.DownSNRMin, rep.DownSNRMax = 0, 0
	}
	if rep.UpPowerMin == math.MaxFloat64 {
		rep.UpPowerMin, rep.UpPowerMax = 0, 0
	}

	rep.Health = "Healthy"

	// 1. Hot downstream signal (very common in apartments; opposite of loose splitter!)
	if rep.DownPowerMax > 8.5 {
		sev := SeverityTip
		if rep.DownPowerMax > 12.0 {
			sev = SeverityHigh
			rep.Health = "Watch"
		}
		padDB := 6
		if rep.DownPowerMax > 13 {
			padDB = 10
		}
		action := fmt.Sprintf("Do NOT remove splitters (that makes a hot signal worse). Add an inline %d dB coax attenuator at the modem input to center downstream around %.1f to %.1f dBmV",
			padDB, rep.DownPowerMin-float64(padDB), rep.DownPowerMax-float64(padDB))
		if rep.UpstreamCount > 0 {
			action += fmt.Sprintf(" and upstream around %.1f dBmV.", rep.UpPowerMax+float64(padDB))
		}
		rep.Findings = append(rep.Findings, Advice{
			Severity: sev,
			Category: "Modem Coax",
			Title:    fmt.Sprintf("Downstream coax signal is hot (+%.1f to +%.1f dBmV)", rep.DownPowerMin, rep.DownPowerMax),
			Detail:   "Ideal DOCSIS 3.1 downstream power is -7 to +7 dBmV. High power can trigger modem 'Poor' warnings or tuner clipping.",
			Action:   action,
		})
	} else if rep.DownPowerMin < -8.0 {
		rep.Health = "Poor"
		rep.Findings = append(rep.Findings, Advice{
			Severity: SeverityHigh,
			Category: "Modem Coax",
			Title:    fmt.Sprintf("Downstream coax signal is weak (%.1f dBmV)", rep.DownPowerMin),
			Detail:   "Downstream power below -8 dBmV reduces SNR headroom and causes drops.",
			Action:   "Tighten coax connectors, remove unused splitters, or ask your ISP to check the drop.",
		})
	}

	// 2. Downstream SNR
	if rep.DownSNRMin > 0 && rep.DownSNRMin < 33.0 {
		rep.Health = "Poor"
		rep.Findings = append(rep.Findings, Advice{
			Severity: SeverityCritical,
			Category: "Modem Coax",
			Title:    fmt.Sprintf("Low downstream SNR (%.1f dB)", rep.DownSNRMin),
			Detail:   "256-QAM / OFDM channels need >= 33 dB SNR to stay error-free.",
			Action:   "Inspect coax cable and splitters for ingress noise, or schedule an ISP line check.",
		})
	} else if rep.DownSNRMin >= 38.0 {
		rep.Findings = append(rep.Findings, Advice{
			Severity: SeverityGood,
			Category: "Modem Coax",
			Title:    fmt.Sprintf("Clean coax SNR (%.1f to %.1f dB)", rep.DownSNRMin, rep.DownSNRMax),
			Detail:   "Downstream signal-to-noise ratio is well above the 33 dB DOCSIS threshold.",
			Action:   "No coax noise remediation needed.",
		})
	}

	// 3. Upstream power
	if rep.UpstreamCount > 0 && rep.UpPowerMax > 50.5 {
		rep.Health = "Poor"
		rep.Findings = append(rep.Findings, Advice{
			Severity: SeverityCritical,
			Category: "Modem Coax",
			Title:    fmt.Sprintf("High upstream transmit power (%.1f dBmV)", rep.UpPowerMax),
			Detail:   "Upstream above 50 dBmV means the modem is shouting near its limit, causing T3 timeouts and call drops.",
			Action:   "Remove unnecessary coax splitters between the wall outlet and modem, or call your ISP.",
		})
	}

	// 4. Uncorrectables
	if rep.TotalUncorrectables > 50000 || rep.ChannelsWithUncorr >= 4 {
		rep.Health = "Poor"
		rep.Findings = append(rep.Findings, Advice{
			Severity: SeverityHigh,
			Category: "Modem Coax",
			Title:    fmt.Sprintf("Elevated uncorrectable codewords (%d across %d channels)", rep.TotalUncorrectables, rep.ChannelsWithUncorr),
			Detail:   "Sustained uncorrectables indicate coax ingress noise or marginal signal levels.",
			Action:   "Check coax connectors, replace aged splitters, and monitor whether counters climb over 10 minutes.",
		})
	} else if rep.TotalUncorrectables == 0 {
		rep.Findings = append(rep.Findings, Advice{
			Severity: SeverityGood,
			Category: "Modem Coax",
			Title:    "Zero uncorrectable codewords across all locked channels",
			Detail:   "Your coax link is delivering clean frames.",
			Action:   "No coax line repair needed.",
		})
	}

	// 5. Legacy low-split upstream note
	if rep.UpstreamCount > 0 && !rep.UpstreamOFDMAActive {
		rep.Findings = append(rep.Findings, Advice{
			Severity: SeverityTip,
			Category: "ISP Plant",
			Title:    fmt.Sprintf("Legacy DOCSIS upstream (%d ATDMA channels, no OFDMA)", rep.UpstreamCount),
			Detail:   "Your ISP plant has not enabled mid/high-split OFDMA upload here, which explains asymmetric ~30-45 Mbps upload caps.",
			Action:   "This is an ISP plant limit, not a Wi-Fi issue. Pair with router SQM if uploads affect video calls.",
		})
	}

	return rep
}

func updateMinMax(minV, maxV *float64, v float64) {
	if v < *minV {
		*minV = v
	}
	if v > *maxV {
		*maxV = v
	}
}
