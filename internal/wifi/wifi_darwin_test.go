//go:build darwin

package wifi

import "testing"

func TestParseIfconfigVerboseAndWdutilInfo(t *testing.T) {
	ifconfigOut := `
en0: flags=8863<UP,BROADCAST,SMART,RUNNING,SIMPLEX,MULTICAST> mtu 1500 index 15
	link quality: 100 (good)
	scheduler: FQ_CODEL (driver managed)
	uplink rate: 168.22 Mbps [eff] / 304.20 Mbps
	downlink rate: 168.22 Mbps [eff] / 304.20 Mbps [max]
	qosmarking enabled: yes mode: fastlane
`
	var info LinkInfo
	parseIfconfigVerbose(ifconfigOut, &info)
	if info.LinkQuality != 100 || info.Scheduler != "FQ_CODEL" || info.QoSMode != "fastlane" || info.EffDownMbps != 168.22 {
		t.Fatalf("unexpected ifconfig -v parse: %+v", info)
	}

	wdutilOut := `
WIFI
    MAC Address          : aa:bb:cc:dd:ee:ff
    SSID                 : <redacted>
    BSSID                : <redacted>
    MCS Index            : 11
    NSS                  : 2
    Guard Interval       : 800 ns
    CCA                  : 66%
BLUETOOTH
    Power                : On
`
	parseWdutilInfo(wdutilOut, &info)
	if info.SSID != "" || info.BSSID != "" {
		t.Fatalf("expected <redacted> SSID/BSSID to be skipped, got ssid=%q bssid=%q", info.SSID, info.BSSID)
	}
	if !info.HasCCA || info.CCAPct != 66 || !info.HasMCS || info.MCS != 11 || info.NSS != 2 || info.GuardInterval != 800 {
		t.Fatalf("unexpected wdutil parse: %+v", info)
	}
}
