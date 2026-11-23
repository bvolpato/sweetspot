//go:build linux

package wifi

import (
	"context"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// ReadLink reads Wi-Fi link metrics on Linux via iw, /proc/net/dev, and ip route.
// When run with root/CAP_NET_ADMIN, iw survey dump also populates CCA channel airtime %.
func ReadLink(ctx context.Context) (LinkInfo, error) {
	info := LinkInfo{
		Timestamp:  time.Now(),
		Band:       BandUnknown,
		Privileged: os.Geteuid() == 0,
	}

	iface, gw := readDefaultRouteLinux(ctx)
	info.DefaultIface = iface
	info.Gateway = gw
	info.Interface = iface

	if iface != "" {
		c, cancel := context.WithTimeout(ctx, 2*time.Second)
		out, err := exec.CommandContext(c, "iw", "dev", iface, "link").Output()
		cancel()
		if err == nil && !strings.Contains(string(out), "Not connected") {
			info.PowerOn = true
			info.Connected = true
			parseIwLink(string(out), &info)
			enrichIwStation(ctx, iface, &info)
			enrichIwSurvey(ctx, iface, &info)
		} else if strings.HasPrefix(iface, "en") || strings.HasPrefix(iface, "eth") {
			info.IsEthernet = true
		}
		info.InBytes, info.OutBytes, info.InErrors, info.OutErrors = readLinuxProcNetDev(iface)
	}
	info.DNSServers = readLinuxResolvConf()
	return info, nil
}

// ScanNeighbors scans nearby networks on Linux via iw scan (or nmcli fallback).
func ScanNeighbors(ctx context.Context, current LinkInfo) ScanResult {
	start := time.Now()
	if current.Interface == "" {
		return SummarizeScan(nil, current, time.Since(start))
	}
	c, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	out, err := exec.CommandContext(c, "iw", "dev", current.Interface, "scan").Output()
	if err != nil {
		if nm := fallbackNmcliScan(ctx); len(nm) > 0 {
			return SummarizeScan(nm, current, time.Since(start))
		}
		res := SummarizeScan(nil, current, time.Since(start))
		res.Err = "RF scan requires sudo (or NetworkManager nmcli) on Linux"
		return res
	}
	var neighbors []NeighborAP
	var cur *NeighborAP
	reFreq := regexp.MustCompile(`freq:\s*(\d+)`)
	reSig := regexp.MustCompile(`signal:\s*(-?\d+)`)
	for _, line := range strings.Split(string(out), "\n") {
		trim := strings.TrimSpace(line)
		if strings.HasPrefix(trim, "BSS ") {
			if cur != nil && cur.Channel > 0 {
				neighbors = append(neighbors, *cur)
			}
			fields := strings.Fields(trim)
			bssid := ""
			if len(fields) >= 2 {
				bssid = strings.Split(fields[1], "(")[0]
			}
			cur = &NeighborAP{BSSID: bssid, WidthMHz: 20}
			continue
		}
		if cur == nil {
			continue
		}
		if m := reFreq.FindStringSubmatch(trim); m != nil {
			freq, _ := strconv.Atoi(m[1])
			cur.Band, cur.Channel = freqToBandChannel(freq)
		} else if m := reSig.FindStringSubmatch(trim); m != nil {
			cur.RSSI, _ = strconv.Atoi(m[1])
		} else if strings.HasPrefix(trim, "SSID: ") {
			cur.SSID = strings.TrimPrefix(trim, "SSID: ")
		}
	}
	if cur != nil && cur.Channel > 0 {
		neighbors = append(neighbors, *cur)
	}
	return SummarizeScan(neighbors, current, time.Since(start))
}

func fallbackNmcliScan(ctx context.Context) []NeighborAP {
	c, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	out, err := exec.CommandContext(c, "nmcli", "-t", "-f", "BSSID,SSID,CHAN,FREQ,SIGNAL,SECURITY", "dev", "wifi", "list").Output()
	if err != nil {
		return nil
	}
	var outAPs []NeighborAP
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		// nmcli escapes ':' in BSSID as '\:'
		safe := strings.ReplaceAll(line, `\:`, "\x00")
		parts := strings.Split(safe, ":")
		if len(parts) < 5 {
			continue
		}
		bssid := strings.ReplaceAll(parts[0], "\x00", ":")
		ch, _ := strconv.Atoi(parts[2])
		freqStr := strings.Fields(parts[3])
		freq := 0
		if len(freqStr) > 0 {
			freq, _ = strconv.Atoi(freqStr[0])
		}
		sigPct, _ := strconv.Atoi(parts[4])
		band, _ := freqToBandChannel(freq)
		if ch > 0 {
			outAPs = append(outAPs, NeighborAP{
				BSSID:    bssid,
				SSID:     parts[1],
				Channel:  ch,
				Band:     band,
				WidthMHz: 20,
				RSSI:     sigPct/2 - 100,
			})
		}
	}
	return outAPs
}

func enrichIwStation(ctx context.Context, iface string, info *LinkInfo) {
	c, cancel := context.WithTimeout(ctx, 1500*time.Millisecond)
	defer cancel()
	out, err := exec.CommandContext(c, "iw", "dev", iface, "station", "dump").Output()
	if err != nil {
		return
	}
	var txPkts, txRetries uint64
	reMCS := regexp.MustCompile(`(?:HE-MCS|VHT-MCS|EHT-MCS|MCS)\s+(\d+)`)
	reNSS := regexp.MustCompile(`(?:HE-NSS|VHT-NSS|EHT-NSS)\s+(\d+)`)
	for _, line := range strings.Split(string(out), "\n") {
		trim := strings.TrimSpace(line)
		if strings.HasPrefix(trim, "tx packets:") {
			txPkts, _ = strconv.ParseUint(strings.TrimSpace(strings.TrimPrefix(trim, "tx packets:")), 10, 64)
		} else if strings.HasPrefix(trim, "tx retries:") {
			txRetries, _ = strconv.ParseUint(strings.TrimSpace(strings.TrimPrefix(trim, "tx retries:")), 10, 64)
		} else if strings.HasPrefix(trim, "tx bitrate:") {
			if m := reMCS.FindStringSubmatch(trim); m != nil {
				info.MCS, _ = strconv.Atoi(m[1])
				info.HasMCS = true
			}
			if m := reNSS.FindStringSubmatch(trim); m != nil {
				info.NSS, _ = strconv.Atoi(m[1])
			}
		}
	}
	if txPkts > 0 {
		info.TxRetriesPct = float64(txRetries) * 100.0 / float64(txPkts)
	}
}

func enrichIwSurvey(ctx context.Context, iface string, info *LinkInfo) {
	c, cancel := context.WithTimeout(ctx, 1500*time.Millisecond)
	defer cancel()
	out, err := exec.CommandContext(c, "iw", "dev", iface, "survey", "dump").Output()
	if err != nil {
		return
	}
	inUse := false
	var activeMs, busyMs float64
	for _, line := range strings.Split(string(out), "\n") {
		trim := strings.TrimSpace(line)
		if strings.HasPrefix(trim, "frequency:") {
			inUse = strings.Contains(trim, "[in use]")
			activeMs, busyMs = 0, 0
			continue
		}
		if !inUse {
			continue
		}
		if strings.HasPrefix(trim, "noise:") {
			f := strings.Fields(strings.TrimPrefix(trim, "noise:"))
			if len(f) > 0 {
				if n, err := strconv.Atoi(f[0]); err == nil && n < 0 {
					info.Noise = n
					if info.RSSI != 0 {
						info.SNR = info.RSSI - info.Noise
					}
				}
			}
		} else if strings.HasPrefix(trim, "channel active time:") {
			f := strings.Fields(strings.TrimPrefix(trim, "channel active time:"))
			if len(f) > 0 {
				activeMs, _ = strconv.ParseFloat(f[0], 64)
			}
		} else if strings.HasPrefix(trim, "channel busy time:") {
			f := strings.Fields(strings.TrimPrefix(trim, "channel busy time:"))
			if len(f) > 0 {
				busyMs, _ = strconv.ParseFloat(f[0], 64)
			}
		}
		if activeMs > 0 && busyMs >= 0 {
			pct := int(busyMs * 100.0 / activeMs)
			if pct > 100 {
				pct = 100
			}
			info.CCAPct = pct
			info.HasCCA = true
		}
	}
}

func readDefaultRouteLinux(ctx context.Context) (iface, gw string) {
	c, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	out, err := exec.CommandContext(c, "ip", "route", "show", "default").Output()
	if err != nil {
		return "", ""
	}
	fields := strings.Fields(string(out))
	for i := 0; i < len(fields)-1; i++ {
		if fields[i] == "via" && gw == "" {
			gw = fields[i+1]
		}
		if fields[i] == "dev" && iface == "" {
			iface = fields[i+1]
		}
	}
	return iface, gw
}

func parseIwLink(text string, info *LinkInfo) {
	reBSSID := regexp.MustCompile(`Connected to ([0-9a-fA-F:]{17})`)
	reSig := regexp.MustCompile(`signal:\s*(-?\d+)\s*dBm`)
	reFreq := regexp.MustCompile(`freq:\s*(\d+)`)
	reTx := regexp.MustCompile(`tx bitrate:\s*([0-9.]+)\s*MBit/s`)
	reRx := regexp.MustCompile(`rx bitrate:\s*([0-9.]+)\s*MBit/s`)
	reW := regexp.MustCompile(`(\d+)MHz`)
	for _, line := range strings.Split(text, "\n") {
		trim := strings.TrimSpace(line)
		if m := reBSSID.FindStringSubmatch(trim); m != nil {
			info.BSSID = m[1]
		}
		if strings.HasPrefix(trim, "SSID:") {
			info.SSID = strings.TrimSpace(strings.TrimPrefix(trim, "SSID:"))
		}
		if m := reSig.FindStringSubmatch(trim); m != nil {
			info.RSSI, _ = strconv.Atoi(m[1])
			info.Noise = -92
			info.SNR = info.RSSI - info.Noise
		}
		if m := reFreq.FindStringSubmatch(trim); m != nil {
			freq, _ := strconv.Atoi(m[1])
			info.Band, info.Channel = freqToBandChannel(freq)
		}
		if m := reRx.FindStringSubmatch(trim); m != nil {
			info.RxRateMbps, _ = strconv.ParseFloat(m[1], 64)
		}
		if m := reTx.FindStringSubmatch(trim); m != nil {
			info.TxRateMbps, _ = strconv.ParseFloat(m[1], 64)
			if mw := reW.FindStringSubmatch(trim); mw != nil {
				info.WidthMHz, _ = strconv.Atoi(mw[1])
			}
			switch {
			case strings.Contains(trim, "EHT"):
				info.PHYMode = "802.11be (Wi-Fi 7)"
			case strings.Contains(trim, "HE"):
				info.PHYMode = "802.11ax (Wi-Fi 6)"
			case strings.Contains(trim, "VHT"):
				info.PHYMode = "802.11ac (Wi-Fi 5)"
			default:
				info.PHYMode = "802.11n"
			}
		}
	}
}

func freqToBandChannel(freq int) (Band, int) {
	switch {
	case freq >= 2412 && freq <= 2484:
		if freq == 2484 {
			return Band2GHz, 14
		}
		return Band2GHz, (freq - 2407) / 5
	case freq >= 5180 && freq <= 5885:
		return Band5GHz, (freq - 5000) / 5
	case freq >= 5955 && freq <= 7115:
		return Band6GHz, (freq - 5950) / 5
	default:
		return BandUnknown, 0
	}
}

func readLinuxProcNetDev(iface string) (inB, outB, inE, outE uint64) {
	data, err := os.ReadFile("/proc/net/dev")
	if err != nil {
		return 0, 0, 0, 0
	}
	for _, line := range strings.Split(string(data), "\n") {
		trim := strings.TrimSpace(line)
		if !strings.HasPrefix(trim, iface+":") {
			continue
		}
		rest := strings.TrimSpace(strings.TrimPrefix(trim, iface+":"))
		f := strings.Fields(rest)
		if len(f) >= 11 {
			inB, _ = strconv.ParseUint(f[0], 10, 64)
			inE, _ = strconv.ParseUint(f[2], 10, 64)
			outB, _ = strconv.ParseUint(f[8], 10, 64)
			outE, _ = strconv.ParseUint(f[10], 10, 64)
			return
		}
	}
	return 0, 0, 0, 0
}

func readLinuxResolvConf() []string {
	data, err := os.ReadFile("/etc/resolv.conf")
	if err != nil {
		return nil
	}
	var out []string
	for _, line := range strings.Split(string(data), "\n") {
		f := strings.Fields(line)
		if len(f) >= 2 && f[0] == "nameserver" {
			out = append(out, f[1])
		}
	}
	return out
}
