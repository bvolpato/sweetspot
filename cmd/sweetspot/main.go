package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/bvolpato/sweetspot/internal/advisor"
	"github.com/bvolpato/sweetspot/internal/probe"
	"github.com/bvolpato/sweetspot/internal/ui"
	"github.com/bvolpato/sweetspot/internal/wifi"
)

func main() {
	wanTarget := flag.String("wan", "1.1.1.1", "WAN target IP/host for continuous latency probing")
	continuous := flag.Bool("continuous", false, "Start with continuous walk-around throughput probing enabled")
	modemFile := flag.String("modem", "", "Path to a text file containing cable modem DOCSIS connection stats")
	once := flag.Bool("once", false, "Run a non-interactive diagnostic snapshot and exit")
	asJSON := flag.Bool("json", false, "Output JSON (implies --once)")
	withSpeed := flag.Bool("speed", false, "Include an active download/upload burst during --once")
	withScan := flag.Bool("scan", false, "Include a nearby RF channel scan during --once")
	interval := flag.Duration("interval", time.Second, "Wi-Fi polling interval")
	flag.Parse()

	var modemText string
	if *modemFile != "" {
		raw, err := os.ReadFile(*modemFile)
		if err != nil {
			fmt.Fprintf(os.Stderr, "sweetspot: failed to read --modem file: %v\n", err)
			os.Exit(1)
		}
		modemText = string(raw)
	}

	if *once || *asJSON {
		runOnce(*wanTarget, modemText, *withSpeed, *withScan, *asJSON)
		return
	}

	model := ui.NewModel(ui.Options{
		WANTarget:        *wanTarget,
		ContinuousSpeed:  *continuous,
		InitialModemText: modemText,
		PollInterval:     *interval,
	})
	p := tea.NewProgram(model, tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "sweetspot: %v\n", err)
		os.Exit(1)
	}
}

func runOnce(wanTarget, modemText string, withSpeed, withScan, asJSON bool) {
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()

	link, _ := wifi.ReadLink(ctx)
	gw := probe.OneShotPing(ctx, link.Gateway, 10, 40.0)
	wan := probe.OneShotPing(ctx, wanTarget, 10, 80.0)
	dns := probe.RunDNSRace(ctx, link.Gateway)

	var scan wifi.ScanResult
	if withScan {
		scan = wifi.ScanNeighbors(ctx, link)
	}

	var speed probe.SpeedResult
	if withSpeed {
		speed = probe.RunSpeedBurst(ctx, 4*time.Second, 3*time.Second, 4, wan.AvgMs, nil, nil)
	}

	var modem advisor.ModemReport
	if modemText != "" {
		modem = advisor.ParseModemDiagnostics(modemText)
	}

	score := advisor.ComputeScore(link, gw, wan, speed)
	advice := advisor.Diagnose(link, gw, wan, speed, scan, dns, modem)

	if asJSON {
		payload := map[string]any{
			"score":  score,
			"link":   link,
			"gw":     gw,
			"wan":    wan,
			"speed":  speed,
			"dns":    dns,
			"scan":   scan,
			"modem":  modem,
			"advice": advice,
		}
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		_ = enc.Encode(payload)
		return
	}

	fmt.Printf("◈ sweetspot  %d/100 (%s) -- %s\n", score.Total, score.Grade, score.Summary)
	fmt.Printf("  Link:    %s · %d dBm / %d dB SNR · %.0f Mbps PHY · %s (%s)\n",
		link.ChannelLabel(), link.RSSI, link.SNR, link.TxRateMbps, link.PHYMode, link.Security)
	fmt.Printf("  Router:  avg %.1f ms · max %.1f ms · jitter %.1f ms · loss %.1f%% (%s)\n",
		gw.AvgMs, gw.MaxMs, gw.JitterMs, gw.LossPct, gw.Target)
	fmt.Printf("  WAN:     avg %.1f ms · max %.1f ms · jitter %.1f ms · loss %.1f%% (%s)\n",
		wan.AvgMs, wan.MaxMs, wan.JitterMs, wan.LossPct, wan.Target)
	if speed.DownloadMbps > 0 {
		fmt.Printf("  Speed:   ↓ %.1f Mbps · ↑ %.1f Mbps\n", speed.DownloadMbps, speed.UploadMbps)
	}
	if !dns.Fastest.Failed {
		fmt.Printf("  DNS:     fastest %s (%s) %.1f ms uncached vs system %.1f ms\n",
			dns.Fastest.Name, dns.Fastest.Server, dns.Fastest.UncachedMs, dns.System.UncachedMs)
	}
	if len(advice) > 0 {
		fmt.Println("\nRecommendations:")
		for _, a := range advice {
			fmt.Printf("  [%s] (%s) %s\n       -> %s\n", a.Severity, a.Category, a.Title, a.Action)
		}
	}
}
