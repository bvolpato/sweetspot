//go:build windows

package wifi

import (
	"context"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// ReadLink reads live Wi-Fi telemetry on Windows via netsh wlan show interfaces,
// plus gateway, DNS servers, and interface byte counters.
func ReadLink(ctx context.Context) (LinkInfo, error) {
	info := LinkInfo{
		Timestamp: time.Now(),
		Interface: "Wi-Fi",
		Band:      BandUnknown,
	}

	c, cancel := context.WithTimeout(ctx, 2*time.Second)
	out, err := exec.CommandContext(c, "netsh", "wlan", "show", "interfaces").Output()
	cancel()
	if err == nil {
		parseNetshInterfaces(string(out), &info)
	}

	gw := readWindowsDefaultGateway(ctx)
	info.Gateway = gw
	if !info.Connected && gw != "" {
		info.IsEthernet = true
		info.DefaultIface = "Ethernet"
	} else {
		info.DefaultIface = info.Interface
	}

	info.InBytes, info.OutBytes, info.InErrors, info.OutErrors = readWindowsNetstat(ctx)
	info.DNSServers = readWindowsDNS(ctx)
	return info, nil
}

// ScanNeighbors scans nearby networks on Windows using netsh wlan show networks mode=bssid.
func ScanNeighbors(ctx context.Context, current LinkInfo) ScanResult {
	start := time.Now()
	c, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	out, err := exec.CommandContext(c, "netsh", "wlan", "show", "networks", "mode=bssid").Output()
	if err != nil {
		res := SummarizeScan(nil, current, time.Since(start))
		res.Err = "netsh wlan scan failed (check Wi-Fi adapter and Location permissions)"
		return res
	}

	var neighbors []NeighborAP
	var curSSID, curAuth string
	var cur *NeighborAP

	for _, raw := range strings.Split(string(out), "\n") {
		line := strings.TrimSpace(raw)
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, ":", 2)
		if len(parts) != 2 {
			continue
		}
		k := strings.TrimSpace(parts[0])
		v := strings.TrimSpace(parts[1])

		switch {
		case strings.HasPrefix(k, "SSID "):
			curSSID = v
		case k == "Authentication":
			curAuth = v
		case strings.HasPrefix(k, "BSSID "):
			if cur != nil && cur.Channel > 0 {
				neighbors = append(neighbors, *cur)
			}
			cur = &NeighborAP{
				SSID:     curSSID,
				BSSID:    v,
				Security: curAuth,
				WidthMHz: 20,
			}
		case cur != nil && k == "Signal":
			pct, _ := strconv.Atoi(strings.TrimSuffix(v, "%"))
			cur.RSSI = pct/2 - 100
		case cur != nil && k == "Band":
			cur.Band = parseWindowsBand(v)
		case cur != nil && k == "Channel":
			cur.Channel, _ = strconv.Atoi(v)
			if cur.Band == BandUnknown {
				cur.Band = inferBandFromChannel(cur.Channel)
			}
		}
	}
	if cur != nil && cur.Channel > 0 {
		neighbors = append(neighbors, *cur)
	}
	return SummarizeScan(neighbors, current, time.Since(start))
}

func parseNetshInterfaces(text string, info *LinkInfo) {
	for _, raw := range strings.Split(text, "\n") {
		line := strings.TrimSpace(raw)
		parts := strings.SplitN(line, ":", 2)
		if len(parts) != 2 {
			continue
		}
		k := strings.TrimSpace(parts[0])
		v := strings.TrimSpace(parts[1])
		switch k {
		case "Name":
			if v != "" {
				info.Interface = v
			}
		case "State":
			if strings.EqualFold(v, "connected") {
				info.Connected = true
				info.PowerOn = true
			}
		case "SSID":
			info.SSID = v
		case "AP BSSID", "BSSID":
			info.BSSID = v
		case "Radio type":
			info.PHYMode = mapWindowsPHY(v)
		case "Authentication":
			info.Security = v
		case "Band":
			info.Band = parseWindowsBand(v)
		case "Channel":
			info.Channel, _ = strconv.Atoi(v)
			if info.Band == BandUnknown {
				info.Band = inferBandFromChannel(info.Channel)
			}
		case "Receive rate (Mbps)":
			info.RxRateMbps, _ = strconv.ParseFloat(v, 64)
		case "Transmit rate (Mbps)":
			info.TxRateMbps, _ = strconv.ParseFloat(v, 64)
		case "Signal":
			pct, _ := strconv.Atoi(strings.TrimSuffix(v, "%"))
			info.LinkQuality = pct
			info.RSSI = pct/2 - 100
			info.Noise = -92
			info.SNR = info.RSSI - info.Noise
		}
	}
}

func parseWindowsBand(v string) Band {
	low := strings.ToLower(v)
	switch {
	case strings.Contains(low, "6"):
		return Band6GHz
	case strings.Contains(low, "5"):
		return Band5GHz
	case strings.Contains(low, "2.4"):
		return Band2GHz
	default:
		return BandUnknown
	}
}

func inferBandFromChannel(ch int) Band {
	switch {
	case ch >= 1 && ch <= 14:
		return Band2GHz
	case ch >= 32:
		return Band5GHz
	default:
		return BandUnknown
	}
}

func mapWindowsPHY(v string) string {
	switch {
	case strings.Contains(v, "802.11be"):
		return "802.11be (Wi-Fi 7)"
	case strings.Contains(v, "802.11ax"):
		return "802.11ax (Wi-Fi 6)"
	case strings.Contains(v, "802.11ac"):
		return "802.11ac (Wi-Fi 5)"
	case strings.Contains(v, "802.11n"):
		return "802.11n (Wi-Fi 4)"
	default:
		return v
	}
}

func readWindowsDefaultGateway(ctx context.Context) string {
	c, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	out, err := exec.CommandContext(c, "route", "print", "0.0.0.0").Output()
	if err != nil {
		return ""
	}
	re := regexp.MustCompile(`0\.0\.0\.0\s+0\.0\.0\.0\s+(\d+\.\d+\.\d+\.\d+)`)
	if m := re.FindStringSubmatch(string(out)); m != nil {
		return m[1]
	}
	return ""
}

func readWindowsNetstat(ctx context.Context) (inB, outB, inE, outE uint64) {
	c, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	out, err := exec.CommandContext(c, "netstat", "-e").Output()
	if err != nil {
		return 0, 0, 0, 0
	}
	for _, line := range strings.Split(string(out), "\n") {
		f := strings.Fields(line)
		if len(f) >= 3 && f[0] == "Bytes" {
			inB, _ = strconv.ParseUint(f[1], 10, 64)
			outB, _ = strconv.ParseUint(f[2], 10, 64)
		} else if len(f) >= 3 && f[0] == "Errors" {
			inE, _ = strconv.ParseUint(f[1], 10, 64)
			outE, _ = strconv.ParseUint(f[2], 10, 64)
		}
	}
	return inB, outB, inE, outE
}

func readWindowsDNS(ctx context.Context) []string {
	c, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	out, err := exec.CommandContext(c, "ipconfig", "/all").Output()
	if err != nil {
		return nil
	}
	var servers []string
	seen := map[string]bool{}
	reIP := regexp.MustCompile(`\b(\d+\.\d+\.\d+\.\d+)\b`)
	inDNS := false
	for _, raw := range strings.Split(string(out), "\n") {
		if strings.Contains(raw, "DNS Servers") {
			inDNS = true
			if m := reIP.FindStringSubmatch(raw); m != nil && !seen[m[1]] {
				seen[m[1]] = true
				servers = append(servers, m[1])
			}
			continue
		}
		if inDNS {
			if strings.Contains(raw, ":") {
				inDNS = false
				continue
			}
			if m := reIP.FindStringSubmatch(raw); m != nil && !seen[m[1]] {
				seen[m[1]] = true
				servers = append(servers, m[1])
			}
		}
	}
	return servers
}
