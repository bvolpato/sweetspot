//go:build darwin

package wifi

/*
#cgo CFLAGS: -x objective-c -fobjc-arc
#cgo LDFLAGS: -framework CoreWLAN -framework Foundation
#include "wifi_darwin.h"
*/
import "C"

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// ReadLink reads live Wi-Fi telemetry via CoreWLAN (zero sudo, <1ms, no scan stutter),
// enriches it with zero-sudo ifconfig -v link quality/effective rate/QoS fields,
// and automatically reads wdutil info when running as root (sudo) for BSSID, CCA %, MCS, and NSS.
func ReadLink(ctx context.Context) (LinkInfo, error) {
	info := LinkInfo{
		Timestamp:  time.Now(),
		Interface:  "en0",
		Band:       BandUnknown,
		Privileged: os.Geteuid() == 0,
	}

	var cinfo C.ss_wifi_info_t
	if C.ss_read_wifi_info(&cinfo) == 0 {
		if s := C.GoString(&cinfo.iface[0]); s != "" {
			info.Interface = s
		}
		info.SSID = C.GoString(&cinfo.ssid[0])
		info.BSSID = C.GoString(&cinfo.bssid[0])
		info.CountryCode = C.GoString(&cinfo.country[0])
		info.PowerOn = cinfo.power_on != 0
		info.Connected = cinfo.connected != 0
		info.RSSI = int(cinfo.rssi)
		info.Noise = int(cinfo.noise)
		if info.RSSI != 0 && info.Noise != 0 {
			info.SNR = info.RSSI - info.Noise
		}
		info.TxRateMbps = float64(cinfo.tx_rate)
		info.Channel = int(cinfo.channel)
		info.Band = mapBand(int(cinfo.band))
		info.WidthMHz = mapWidth(int(cinfo.width))
		info.PHYMode = mapPHY(int(cinfo.phy_mode))
		info.Security = mapSecurity(int(cinfo.security))
	}

	iface, gw := readDefaultRoute(ctx)
	info.DefaultIface = iface
	info.Gateway = gw
	if iface != "" && iface != info.Interface && strings.HasPrefix(iface, "en") {
		info.IsEthernet = true
	}

	targetIface := info.DefaultIface
	if targetIface == "" {
		targetIface = info.Interface
	}
	inB, outB, inE, outE := readIfaceCounters(ctx, targetIface)
	info.InBytes = inB
	info.OutBytes = outB
	info.InErrors = inE
	info.OutErrors = outE

	enrichIfconfigVerbose(ctx, targetIface, &info)

	// When started with sudo, enrich with unredacted BSSID/SSID, CCA %, MCS, NSS, and GI.
	if info.Privileged && !info.IsEthernet {
		enrichWdutil(ctx, &info)
	}

	info.DNSServers = readDNSServers(ctx)
	info.AirDropActive = checkAirDropActive(ctx)

	return info, nil
}

var (
	reLinkQuality = regexp.MustCompile(`link quality:\s*(\d+)`)
	reScheduler   = regexp.MustCompile(`scheduler:\s*([A-Za-z0-9_-]+)`)
	reDownEff     = regexp.MustCompile(`downlink rate:\s*([0-9.]+)\s*Mbps`)
	reUpEff       = regexp.MustCompile(`uplink rate:\s*([0-9.]+)\s*Mbps`)
	reQoSMode     = regexp.MustCompile(`qosmarking enabled:\s*yes\s+mode:\s*(\S+)`)
)

func enrichIfconfigVerbose(ctx context.Context, iface string, info *LinkInfo) {
	if iface == "" {
		return
	}
	c, cancel := context.WithTimeout(ctx, 1500*time.Millisecond)
	defer cancel()
	out, err := exec.CommandContext(c, "/sbin/ifconfig", "-v", iface).Output()
	if err != nil {
		return
	}
	text := string(out)
	if m := reLinkQuality.FindStringSubmatch(text); m != nil {
		info.LinkQuality, _ = strconv.Atoi(m[1])
	}
	if m := reScheduler.FindStringSubmatch(text); m != nil {
		info.Scheduler = m[1]
	}
	if m := reDownEff.FindStringSubmatch(text); m != nil {
		info.EffDownMbps, _ = strconv.ParseFloat(m[1], 64)
	}
	if m := reUpEff.FindStringSubmatch(text); m != nil {
		info.EffUpMbps, _ = strconv.ParseFloat(m[1], 64)
	}
	if m := reQoSMode.FindStringSubmatch(text); m != nil {
		info.QoSMode = m[1]
	}
}

func enrichWdutil(ctx context.Context, info *LinkInfo) {
	c, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	out, err := exec.CommandContext(c, "/usr/bin/wdutil", "info").Output()
	if err != nil {
		return
	}
	inWiFi := false
	for _, raw := range strings.Split(string(out), "\n") {
		line := strings.TrimSpace(raw)
		if strings.HasPrefix(line, "WIFI") {
			inWiFi = true
			continue
		}
		if inWiFi && (strings.HasPrefix(line, "BLUETOOTH") || strings.HasPrefix(line, "AWDL") || strings.HasPrefix(line, "NETWORK")) {
			break
		}
		if !inWiFi {
			continue
		}
		parts := strings.SplitN(line, ":", 2)
		if len(parts) != 2 {
			continue
		}
		key := strings.TrimSpace(parts[0])
		val := strings.TrimSpace(parts[1])
		if val == "" || val == "None" || strings.Contains(val, "<redacted>") {
			continue
		}
		switch key {
		case "SSID":
			info.SSID = val
		case "BSSID":
			info.BSSID = val
		case "MCS Index":
			if v, err := strconv.Atoi(val); err == nil {
				info.MCS = v
				info.HasMCS = true
			}
		case "NSS":
			if v, err := strconv.Atoi(val); err == nil && v > 0 {
				info.NSS = v
			}
		case "Guard Interval":
			f := strings.Fields(val)
			if len(f) > 0 {
				if v, err := strconv.Atoi(f[0]); err == nil {
					info.GuardInterval = v
				}
			}
		case "CCA":
			num := strings.TrimSpace(strings.TrimSuffix(val, "%"))
			if v, err := strconv.Atoi(num); err == nil {
				info.CCAPct = v
				info.HasCCA = true
			}
		}
	}
}

// ScanNeighbors triggers an explicit on-demand RF neighborhood scan.
func ScanNeighbors(ctx context.Context, current LinkInfo) ScanResult {
	start := time.Now()
	const maxAPs = 192
	var buf [maxAPs]C.ss_neighbor_t

	n := int(C.ss_scan_neighbors(&buf[0], C.int(maxAPs)))
	neighbors := make([]NeighborAP, 0, max(n, 0))
	if n > 0 {
		for i := 0; i < n; i++ {
			row := buf[i]
			ch := int(row.channel)
			if ch <= 0 {
				continue
			}
			neighbors = append(neighbors, NeighborAP{
				SSID:     C.GoString(&row.ssid[0]),
				BSSID:    C.GoString(&row.bssid[0]),
				Channel:  ch,
				Band:     mapBand(int(row.band)),
				WidthMHz: mapWidth(int(row.width)),
				RSSI:     int(row.rssi),
				Noise:    int(row.noise),
				Security: mapSecurity(int(row.security)),
			})
		}
	}

	if len(neighbors) == 0 {
		neighbors = fallbackSystemProfilerScan(ctx)
	}

	res := SummarizeScan(neighbors, current, time.Since(start))
	if len(neighbors) == 0 && n < 0 {
		res.Err = "RF scan returned no neighbors (check Wi-Fi power or Location Services)"
	}
	return res
}

func fallbackSystemProfilerScan(ctx context.Context) []NeighborAP {
	scanCtx, cancel := context.WithTimeout(ctx, 12*time.Second)
	defer cancel()
	out, err := exec.CommandContext(scanCtx, "/usr/sbin/system_profiler", "SPAirPortDataType").Output()
	if err != nil {
		return nil
	}
	parts := bytes.SplitN(out, []byte("Other Local Wi-Fi Networks:"), 2)
	if len(parts) < 2 {
		return nil
	}
	reCh := regexp.MustCompile(`Channel:\s*(\d+)\s*\((2|5|6)GHz,\s*(\d+)MHz\)`)
	reSig := regexp.MustCompile(`Signal / Noise:\s*(-?\d+)\s*dBm\s*/\s*(-?\d+)\s*dBm`)

	var neighbors []NeighborAP
	var cur *NeighborAP
	for _, line := range strings.Split(string(parts[1]), "\n") {
		trim := strings.TrimSpace(line)
		if m := reCh.FindStringSubmatch(trim); m != nil {
			if cur != nil {
				neighbors = append(neighbors, *cur)
			}
			ch, _ := strconv.Atoi(m[1])
			w, _ := strconv.Atoi(m[3])
			band := Band5GHz
			switch m[2] {
			case "2":
				band = Band2GHz
			case "6":
				band = Band6GHz
			}
			cur = &NeighborAP{Channel: ch, Band: band, WidthMHz: w}
			continue
		}
		if cur != nil {
			if m := reSig.FindStringSubmatch(trim); m != nil {
				cur.RSSI, _ = strconv.Atoi(m[1])
				cur.Noise, _ = strconv.Atoi(m[2])
			}
		}
	}
	if cur != nil {
		neighbors = append(neighbors, *cur)
	}
	return neighbors
}

func readDefaultRoute(ctx context.Context) (iface, gw string) {
	c, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	out, err := exec.CommandContext(c, "/sbin/route", "-n", "get", "default").Output()
	if err != nil {
		return "", ""
	}
	for _, line := range strings.Split(string(out), "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 2 {
			switch fields[0] {
			case "gateway:":
				gw = fields[1]
			case "interface:":
				iface = fields[1]
			}
		}
	}
	return iface, gw
}

func readIfaceCounters(ctx context.Context, iface string) (inB, outB, inE, outE uint64) {
	if iface == "" {
		return 0, 0, 0, 0
	}
	c, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	out, err := exec.CommandContext(c, "/usr/sbin/netstat", "-ibn", "-I", iface).Output()
	if err != nil {
		return 0, 0, 0, 0
	}
	for _, line := range strings.Split(string(out), "\n") {
		if !strings.Contains(line, "<Link#") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) >= 10 {
			inE, _ = strconv.ParseUint(fields[5], 10, 64)
			inB, _ = strconv.ParseUint(fields[6], 10, 64)
			outE, _ = strconv.ParseUint(fields[8], 10, 64)
			outB, _ = strconv.ParseUint(fields[9], 10, 64)
			return inB, outB, inE, outE
		}
	}
	return 0, 0, 0, 0
}

func readDNSServers(ctx context.Context) []string {
	c, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	out, err := exec.CommandContext(c, "/usr/sbin/scutil", "--dns").Output()
	if err != nil {
		return nil
	}
	var servers []string
	seen := map[string]bool{}
	inFirst := false
	for _, line := range strings.Split(string(out), "\n") {
		trim := strings.TrimSpace(line)
		if strings.HasPrefix(trim, "resolver #1") {
			inFirst = true
			continue
		}
		if inFirst && strings.HasPrefix(trim, "resolver #") {
			break
		}
		if inFirst && strings.HasPrefix(trim, "nameserver[") {
			parts := strings.SplitN(trim, ":", 2)
			if len(parts) == 2 {
				ip := strings.TrimSpace(parts[1])
				if ip != "" && !seen[ip] {
					seen[ip] = true
					servers = append(servers, ip)
				}
			}
		}
	}
	return servers
}

func checkAirDropActive(ctx context.Context) bool {
	c, cancel := context.WithTimeout(ctx, 1*time.Second)
	defer cancel()
	out, err := exec.CommandContext(c, "/usr/bin/defaults", "read", "com.apple.sharingd", "DiscoverableMode").Output()
	if err != nil {
		return true
	}
	mode := strings.ToLower(strings.TrimSpace(string(out)))
	return mode != "off"
}

func mapBand(v int) Band {
	switch v {
	case 1:
		return Band2GHz
	case 2:
		return Band5GHz
	case 3:
		return Band6GHz
	default:
		return BandUnknown
	}
}

func mapWidth(v int) int {
	switch v {
	case 1:
		return 20
	case 2:
		return 40
	case 3:
		return 80
	case 4:
		return 160
	default:
		return 0
	}
}

func mapPHY(v int) string {
	switch v {
	case 1:
		return "802.11a"
	case 2:
		return "802.11b"
	case 3:
		return "802.11g"
	case 4:
		return "802.11n (Wi-Fi 4)"
	case 5:
		return "802.11ac (Wi-Fi 5)"
	case 6:
		return "802.11ax (Wi-Fi 6)"
	case 7:
		return "802.11be (Wi-Fi 7)"
	default:
		return "Unknown"
	}
}

func mapSecurity(v int) string {
	switch v {
	case 0:
		return "Open"
	case 1:
		return "WEP"
	case 2, 3:
		return "WPA Personal"
	case 4, 5:
		return "WPA2 Personal"
	case 7, 8:
		return "WPA Enterprise"
	case 9, 10:
		return "WPA2 Enterprise"
	case 11:
		return "WPA3 Personal"
	case 12:
		return "WPA3 Enterprise"
	case 13:
		return "WPA2/WPA3 Personal"
	case 14:
		return "OWE"
	default:
		return fmt.Sprintf("Mode %d", v)
	}
}
