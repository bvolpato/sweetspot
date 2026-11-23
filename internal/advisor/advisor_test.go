package advisor

import (
	"strings"
	"testing"

	"github.com/bvolpato/sweetspot/internal/probe"
	"github.com/bvolpato/sweetspot/internal/wifi"
)

func TestParseModemDiagnostics_HotDownstreamAndCleanSNR(t *testing.T) {
	sample := `
Startup Procedure
System Up Time: 38Days, 06:39:01

Downstream Bonded Channels
1	Locked	256 QAM	24	603000000 Hz	10.3 dBmV	43.4 dB	34	0
2	Locked	256 QAM	1	465000000 Hz	9.6 dBmV	43.2 dB	652	1111
3	Locked	256 QAM	2	471000000 Hz	9.4 dBmV	43.2 dB	20	0

Upstream Bonded Channels
1	Locked	ATDMA	1	5120 Ksym/sec	18200000 Hz	38.3 dBmV
2	Locked	ATDMA	2	5120 Ksym/sec	24600000 Hz	38.3 dBmV

Downstream OFDM Channels
1	193	663000000 Hz	11.7 dBmV	43.8 dB	148 ~ 3947	12135207456	0
`
	rep := ParseModemDiagnostics(sample)
	if !rep.Parsed {
		t.Fatal("expected modem report to parse")
	}
	if rep.DownstreamCount != 3 || rep.OFDMCount != 1 || rep.UpstreamCount != 2 {
		t.Fatalf("unexpected channel counts: %+v", rep)
	}
	if rep.DownPowerMax != 11.7 || rep.UpPowerMax != 38.3 {
		t.Fatalf("unexpected power bounds: downMax=%.1f upMax=%.1f", rep.DownPowerMax, rep.UpPowerMax)
	}
	if rep.TotalUncorrectables != 1111 {
		t.Fatalf("expected 1111 uncorrectables, got %d", rep.TotalUncorrectables)
	}
	foundHot := false
	for _, f := range rep.Findings {
		if strings.Contains(f.Title, "hot") && strings.Contains(f.Action, "attenuator") {
			foundHot = true
		}
	}
	if !foundHot {
		t.Fatalf("expected hot downstream attenuator advice, got: %+v", rep.Findings)
	}
}

func TestDiagnose_Detects40MHzCapAndBufferbloat(t *testing.T) {
	link := wifi.LinkInfo{
		Connected:  true,
		Band:       wifi.Band5GHz,
		Channel:    36,
		WidthMHz:   40,
		RSSI:       -41,
		Noise:      -91,
		SNR:        50,
		TxRateMbps: 573,
		Security:   "WPA2 Personal",
	}
	gw := probe.PingStats{Target: "192.168.1.1", Sent: 20, Received: 20, AvgMs: 5.5, JitterMs: 1.2}
	wan := probe.PingStats{Target: "1.1.1.1", Sent: 20, Received: 20, AvgMs: 20.0, JitterMs: 4.0}
	speed := probe.SpeedResult{DownloadMbps: 216, UploadMbps: 30, IdleRTTMs: 21, LoadedRTTMs: 152, BloatDeltaMs: 131, RPM: 394}
	scan := wifi.ScanResult{TotalAPs: 52, CurrentOverlapAPs: 13, Best5GHzChannel: 100, Best5GHzNonDFS: 48}
	dns := probe.DNSRaceResult{
		Entries: []probe.DNSBenchmark{{Name: "Cloudflare", Server: "1.1.1.1", UncachedMs: 20}, {Name: "System DNS", Server: "system", UncachedMs: 40, IsSystem: true}},
		Fastest: probe.DNSBenchmark{Name: "Cloudflare", Server: "1.1.1.1", UncachedMs: 20},
		System:  probe.DNSBenchmark{Name: "System DNS", Server: "system", UncachedMs: 40, IsSystem: true},
	}

	advice := Diagnose(link, gw, wan, speed, scan, dns, ModemReport{})
	score := ComputeScore(link, gw, wan, speed)
	if score.Total < 60 || score.Total > 90 {
		t.Fatalf("unexpected score for 40MHz bloated link: %+v", score)
	}

	wantCategories := map[string]bool{
		"Router / Width": false,
		"RF Congestion":  false,
		"Bufferbloat":    false,
		"DNS":            false,
	}
	for _, a := range advice {
		if _, ok := wantCategories[a.Category]; ok {
			wantCategories[a.Category] = true
		}
	}
	for cat, hit := range wantCategories {
		if !hit {
			t.Errorf("expected advice category %q in %+v", cat, advice)
		}
	}
}

func TestDiagnose_CCASaturationAndNoDuplicateAirDrop(t *testing.T) {
	link := wifi.LinkInfo{
		Connected:     true,
		Band:          wifi.Band5GHz,
		Channel:       144,
		WidthMHz:      20,
		RSSI:          -60,
		Noise:         -96,
		SNR:           36,
		TxRateMbps:    286,
		Security:      "WPA3 Enterprise",
		HasCCA:        true,
		CCAPct:        72,
		AirDropActive: true,
	}
	gw := probe.PingStats{Target: "10.43.16.1", Sent: 30, Received: 30, AvgMs: 18, MaxMs: 250, JitterMs: 35, SpikeCount: 4}
	wan := probe.PingStats{Target: "1.1.1.1", Sent: 30, Received: 30, AvgMs: 15, MaxMs: 25, JitterMs: 3}

	advice := Diagnose(link, gw, wan, probe.SpeedResult{}, wifi.ScanResult{}, probe.DNSRaceResult{}, ModemReport{})
	airdropMentions := 0
	foundCCA := false
	for _, a := range advice {
		if a.Category == "Airtime / CCA" && a.Severity == SeverityCritical {
			foundCCA = true
		}
		if strings.Contains(a.Action, "AirDrop") || strings.Contains(a.Title, "AirDrop") {
			airdropMentions++
		}
	}
	if !foundCCA {
		t.Fatalf("expected CRITICAL Airtime / CCA finding for 72%% CCA, got: %+v", advice)
	}
	if airdropMentions != 1 {
		t.Fatalf("expected AirDrop advice once (no duplicate), got %d mentions in %+v", airdropMentions, advice)
	}

	playbook := BuildPlaybook(link, gw, probe.SpeedResult{}, wifi.ScanResult{}, probe.DNSRaceResult{}, ModemReport{})
	if len(playbook) < 5 {
		t.Fatalf("expected comprehensive playbook items, got %d", len(playbook))
	}
}
