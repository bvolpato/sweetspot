package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/bvolpato/sweetspot/internal/advisor"
	"github.com/bvolpato/sweetspot/internal/probe"
	"github.com/bvolpato/sweetspot/internal/spots"
	"github.com/bvolpato/sweetspot/internal/ui"
	"github.com/bvolpato/sweetspot/internal/wifi"
)

var version = "dev"

const schemaVersion = "1"

func main() {
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "tui":
			runTUI(os.Args[2:])
			return
		case "status":
			runStatus(os.Args[2:])
			return
		case "diagnose", "diag":
			runDiagnose(os.Args[2:])
			return
		case "scan":
			runScan(os.Args[2:])
			return
		case "ping":
			runPing(os.Args[2:])
			return
		case "speed":
			runSpeed(os.Args[2:])
			return
		case "dns":
			runDNS(os.Args[2:])
			return
		case "modem":
			runModem(os.Args[2:])
			return
		case "spot", "spots":
			runSpot(os.Args[2:])
			return
		case "diff":
			runDiff(os.Args[2:])
			return
		case "version":
			fmt.Printf("sweetspot %s\n", version)
			return
		case "help", "-h", "--help":
			printUsage()
			return
		}
	}

	// Root invocation: preserve backward-compatible flags (--once, --json, --speed, --scan, --modem, --continuous, --version).
	fs := flag.NewFlagSet("sweetspot", flag.ExitOnError)
	fs.Usage = printUsage
	showVersion := fs.Bool("version", false, "Print version and exit")
	wanTarget := fs.String("wan", "1.1.1.1", "WAN target IP/host for latency probing")
	continuous := fs.Bool("continuous", false, "Start TUI with continuous walk-around throughput probing")
	modemFile := fs.String("modem", "", "Path to a text file containing DOCSIS cable modem stats")
	once := fs.Bool("once", false, "Run a non-interactive diagnostic snapshot and exit")
	asJSON := fs.Bool("json", false, "Output JSON (implies --once)")
	withSpeed := fs.Bool("speed", false, "Include an active download/upload burst during --once")
	withScan := fs.Bool("scan", false, "Include a nearby RF channel scan during --once")
	withNQ := fs.Bool("network-quality", false, "Use macOS networkQuality for speed/RPM during --once")
	outPath := fs.String("out", "", "Save JSON snapshot to file (implies --once)")
	failBelow := fs.Int("fail-below", 0, "Exit with code 3 if SweetSpot score is below this threshold")
	interval := fs.Duration("interval", time.Second, "Wi-Fi polling interval for TUI")
	_ = fs.Parse(os.Args[1:])

	if *showVersion {
		fmt.Printf("sweetspot %s\n", version)
		return
	}

	modemText := readModemInput(*modemFile, false)

	// Automatically fall back to non-interactive diagnose when stdout is not a TTY (e.g., AI agents or pipes).
	if *once || *asJSON || *outPath != "" || !isTerminal(os.Stdout) {
		format := "text"
		if *asJSON {
			format = "json"
		}
		executeDiagnose(diagnoseOpts{
			wanTarget:      *wanTarget,
			modemText:      modemText,
			withSpeed:      *withSpeed,
			withScan:       *withScan,
			withDNS:        true,
			withNQ:         *withNQ,
			pingCount:      10,
			format:         format,
			outPath:        *outPath,
			failBelowScore: *failBelow,
		})
		return
	}

	startTUI(*wanTarget, *continuous, modemText, *interval)
}

func runTUI(args []string) {
	fs := flag.NewFlagSet("sweetspot tui", flag.ExitOnError)
	wanTarget := fs.String("wan", "1.1.1.1", "WAN target IP/host for continuous latency probing")
	continuous := fs.Bool("continuous", false, "Start with continuous walk-around throughput probing")
	modemFile := fs.String("modem", "", "Path to a text file containing DOCSIS cable modem stats")
	interval := fs.Duration("interval", time.Second, "Wi-Fi polling interval")
	_ = fs.Parse(args)
	startTUI(*wanTarget, *continuous, readModemInput(*modemFile, false), *interval)
}

func startTUI(wanTarget string, continuous bool, modemText string, interval time.Duration) {
	model := ui.NewModel(ui.Options{
		WANTarget:        wanTarget,
		ContinuousSpeed:  continuous,
		InitialModemText: modemText,
		PollInterval:     interval,
	})
	p := tea.NewProgram(model, tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "sweetspot: %v\n", err)
		os.Exit(1)
	}
}

// runStatus returns an instant (<100ms) zero-active-probe link snapshot -- ideal for agent polling.
func runStatus(args []string) {
	fs := flag.NewFlagSet("sweetspot status", flag.ExitOnError)
	asJSON := fs.Bool("json", false, "Output JSON")
	format := fs.String("format", "text", "Output format: text, json, markdown")
	failBelow := fs.Int("fail-below", 0, "Exit with code 3 if score is below this threshold")
	_ = fs.Parse(args)
	if *asJSON {
		*format = "json"
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	link, _ := wifi.ReadLink(ctx)
	score := advisor.ComputeScore(link, probe.PingStats{}, probe.PingStats{}, probe.SpeedResult{})
	advice := advisor.Diagnose(link, probe.PingStats{}, probe.PingStats{}, probe.SpeedResult{}, wifi.ScanResult{}, probe.DNSRaceResult{}, advisor.ModemReport{})

	switch *format {
	case "json":
		emitJSON(map[string]any{
			"schema_version": schemaVersion,
			"tool_version":   version,
			"timestamp":      time.Now(),
			"score":          score,
			"link":           link,
			"advice":         advice,
		})
	case "markdown":
		fmt.Printf("| Field | Value |\n|---|---|\n")
		fmt.Printf("| **Score** | %d/100 (%s) |\n", score.Total, score.Grade)
		fmt.Printf("| **Link** | %s |\n", link.ChannelLabel())
		fmt.Printf("| **Signal / SNR** | %d dBm / %d dB (noise %d dBm) |\n", link.RSSI, link.SNR, link.Noise)
		fmt.Printf("| **PHY / Mode** | %.0f Mbps · %s (%s) |\n", link.TxRateMbps, link.PHYMode, link.Security)
		if link.HasCCA {
			fmt.Printf("| **CCA Airtime** | %d%% |\n", link.CCAPct)
		}
		if link.Gateway != "" {
			fmt.Printf("| **Gateway** | %s |\n", link.Gateway)
		}
	default:
		fmt.Printf("◈ sweetspot status  ~%d/100 (%s)\n", score.Total, score.Grade)
		fmt.Printf("  Link:    %s · %d dBm / %d dB SNR · %.0f Mbps PHY · %s (%s)\n",
			link.ChannelLabel(), link.RSSI, link.SNR, link.TxRateMbps, link.PHYMode, link.Security)
		printLinkDetails(link)
		if link.Gateway != "" {
			fmt.Printf("  Gateway: %s · DNS: %s\n", link.Gateway, strings.Join(link.DNSServers, ", "))
		}
	}

	if *failBelow > 0 && score.Total < *failBelow {
		os.Exit(3)
	}
}

type diagnoseOpts struct {
	wanTarget      string
	modemText      string
	withSpeed      bool
	withScan       bool
	withDNS        bool
	withNQ         bool
	pingCount      int
	format         string
	outPath        string
	failBelowScore int
}

func runDiagnose(args []string) {
	fs := flag.NewFlagSet("sweetspot diagnose", flag.ExitOnError)
	wanTarget := fs.String("wan", "1.1.1.1", "WAN target IP/host")
	withSpeed := fs.Bool("speed", false, "Run active download + upload + bufferbloat burst")
	withNQ := fs.Bool("network-quality", false, "Run macOS networkQuality for throughput and RPM")
	withScan := fs.Bool("scan", false, "Run nearby RF channel congestion scan")
	noDNS := fs.Bool("no-dns", false, "Skip DNS resolver race")
	pingCount := fs.Int("ping-count", 15, "Number of idle ICMP pings per hop")
	modemFile := fs.String("modem", "", "Path to DOCSIS cable modem stats file (use '-' for stdin)")
	modemClip := fs.Bool("modem-clipboard", false, "Read DOCSIS cable modem stats from clipboard")
	asJSON := fs.Bool("json", false, "Output JSON")
	format := fs.String("format", "text", "Output format: text, json, markdown")
	outPath := fs.String("out", "", "Also save structured JSON report to this path (for sweetspot diff)")
	failBelow := fs.Int("fail-below", 0, "Exit with code 3 if score is below this threshold")
	_ = fs.Parse(args)
	if *asJSON {
		*format = "json"
	}

	executeDiagnose(diagnoseOpts{
		wanTarget:      *wanTarget,
		modemText:      readModemInput(*modemFile, *modemClip),
		withSpeed:      *withSpeed,
		withScan:       *withScan,
		withDNS:        !*noDNS,
		withNQ:         *withNQ,
		pingCount:      *pingCount,
		format:         *format,
		outPath:        *outPath,
		failBelowScore: *failBelow,
	})
}

func executeDiagnose(opts diagnoseOpts) {
	ctx, cancel := context.WithTimeout(context.Background(), 75*time.Second)
	defer cancel()

	// Sequential execution: never overlap RF scan or throughput with idle pings!
	link, _ := wifi.ReadLink(ctx)
	gw := probe.OneShotPing(ctx, link.Gateway, opts.pingCount, 40.0)
	wan := probe.OneShotPing(ctx, opts.wanTarget, opts.pingCount, 80.0)

	var dns probe.DNSRaceResult
	if opts.withDNS {
		dns = probe.RunDNSRace(ctx, link.Gateway)
	}

	var scan wifi.ScanResult
	if opts.withScan {
		scan = wifi.ScanNeighbors(ctx, link)
	}

	var speed probe.SpeedResult
	if opts.withNQ {
		speed = probe.RunMacOSNetworkQuality(ctx)
	} else if opts.withSpeed {
		wanRing := probe.NewPingRing(opts.wanTarget, 60, 80.0)
		pCtx, pCancel := context.WithCancel(ctx)
		go probe.StreamPing(pCtx, opts.wanTarget, 300*time.Millisecond, func(s probe.PingSample) {
			wanRing.Add(s)
		})
		speed = probe.RunSpeedBurst(ctx, 4*time.Second, 3*time.Second, 3, wan.AvgMs, func() float64 {
			return wanRing.Stats().LastMs
		}, nil)
		pCancel()
	}

	var modem advisor.ModemReport
	if opts.modemText != "" {
		modem = advisor.ParseModemDiagnostics(opts.modemText)
	}

	score := advisor.ComputeScore(link, gw, wan, speed)
	advice := advisor.Diagnose(link, gw, wan, speed, scan, dns, modem)
	playbook := advisor.BuildPlaybook(link, gw, speed, scan, dns, modem)

	report := DiagnosticReport{
		SchemaVersion: schemaVersion,
		ToolVersion:   version,
		Timestamp:     time.Now(),
		Score:         score,
		Link:          link,
		GatewayPing:   gw,
		WANPing:       wan,
		Speed:         speed,
		DNS:           dns,
		Scan:          scan,
		Modem:         modem,
		Advice:        advice,
		Playbook:      playbook,
	}

	if opts.outPath != "" {
		if err := spots.ExportJSON(opts.outPath, report); err != nil {
			fmt.Fprintf(os.Stderr, "sweetspot: failed to write %s: %v\n", opts.outPath, err)
			os.Exit(1)
		}
	}

	switch opts.format {
	case "json":
		emitJSON(report)
	case "markdown":
		printDiagnoseMarkdown(report)
	default:
		printDiagnoseText(report)
	}

	if opts.failBelowScore > 0 && score.Total < opts.failBelowScore {
		os.Exit(3)
	}
}

func runScan(args []string) {
	fs := flag.NewFlagSet("sweetspot scan", flag.ExitOnError)
	asJSON := fs.Bool("json", false, "Output JSON")
	format := fs.String("format", "text", "Output format: text, json, markdown")
	_ = fs.Parse(args)
	if *asJSON {
		*format = "json"
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	link, _ := wifi.ReadLink(ctx)
	res := wifi.ScanNeighbors(ctx, link)

	switch *format {
	case "json":
		emitJSON(res)
	case "markdown":
		fmt.Printf("| Band | Channel | Max Width | APs | Max RSSI | Status |\n|---|---:|---:|---:|---:|---|\n")
		for _, b := range res.Buckets {
			status := ""
			if b.IsCurrent {
				status = "Current"
			} else if b.IsRecommended {
				status = "Cleanest"
			}
			fmt.Printf("| %s | %d | %d MHz | %d | %d dBm | %s |\n", b.Band, b.Channel, b.MaxWidthMHz, b.Count, b.MaxRSSI, status)
		}
	default:
		fmt.Printf("RF Scan: %d nearby APs (%.1fs)\n", res.TotalAPs, res.Duration.Seconds())
		fmt.Printf("  Best 5 GHz: Ch %d (non-DFS: Ch %d) · Best 2.4 GHz: Ch %d · 6 GHz nearby: %v\n",
			res.Best5GHzChannel, res.Best5GHzNonDFS, res.Best2GHzChannel, res.Has6GHzNearby)
		for _, b := range res.Buckets {
			tag := ""
			if b.IsCurrent {
				tag = " [YOU]"
			} else if b.IsRecommended {
				tag = " [CLEAN]"
			}
			fmt.Printf("  %-7s Ch %-3d (%3d MHz)  %2d APs  max %4d dBm%s\n", b.Band, b.Channel, b.MaxWidthMHz, b.Count, b.MaxRSSI, tag)
		}
		if res.Err != "" {
			fmt.Fprintf(os.Stderr, "warning: %s\n", res.Err)
		}
	}
}

func runPing(args []string) {
	fs := flag.NewFlagSet("sweetspot ping", flag.ExitOnError)
	target := fs.String("target", "1.1.1.1", "WAN target IP/host")
	count := fs.Int("count", 20, "Number of ICMP packets per hop")
	asJSON := fs.Bool("json", false, "Output JSON")
	_ = fs.Parse(args)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	link, _ := wifi.ReadLink(ctx)
	gw := probe.OneShotPing(ctx, link.Gateway, *count, 40.0)
	wan := probe.OneShotPing(ctx, *target, *count, 80.0)

	if *asJSON {
		emitJSON(map[string]any{"gateway": gw, "wan": wan})
		return
	}
	fmt.Printf("Router (%s): min %.1f · avg %.1f · p95 %.1f · max %.1f · jitter %.1f ms · loss %.1f%% · spikes %d\n",
		gw.Target, gw.MinMs, gw.AvgMs, gw.P95Ms, gw.MaxMs, gw.JitterMs, gw.LossPct, gw.SpikeCount)
	fmt.Printf("WAN    (%s): min %.1f · avg %.1f · p95 %.1f · max %.1f · jitter %.1f ms · loss %.1f%% · spikes %d\n",
		wan.Target, wan.MinMs, wan.AvgMs, wan.P95Ms, wan.MaxMs, wan.JitterMs, wan.LossPct, wan.SpikeCount)
}

func runSpeed(args []string) {
	fs := flag.NewFlagSet("sweetspot speed", flag.ExitOnError)
	downDur := fs.Duration("down", 5*time.Second, "Download phase duration")
	upDur := fs.Duration("up", 3*time.Second, "Upload phase duration")
	streams := fs.Int("streams", 3, "Parallel HTTP streams")
	wanTarget := fs.String("wan", "1.1.1.1", "Target used to sample idle/loaded latency")
	useNQ := fs.Bool("network-quality", false, "Use Apple networkQuality on macOS")
	asJSON := fs.Bool("json", false, "Output JSON")
	_ = fs.Parse(args)

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	var res probe.SpeedResult
	if *useNQ {
		res = probe.RunMacOSNetworkQuality(ctx)
	} else {
		idle := probe.OneShotPing(ctx, *wanTarget, 8, 80.0)
		wanRing := probe.NewPingRing(*wanTarget, 60, 80.0)
		pCtx, pCancel := context.WithCancel(ctx)
		go probe.StreamPing(pCtx, *wanTarget, 300*time.Millisecond, func(s probe.PingSample) {
			wanRing.Add(s)
		})
		res = probe.RunSpeedBurst(ctx, *downDur, *upDur, *streams, idle.AvgMs, func() float64 {
			return wanRing.Stats().LastMs
		}, nil)
		pCancel()
	}

	if *asJSON {
		emitJSON(res)
		return
	}
	fmt.Printf("Source: %s\n", res.Source)
	fmt.Printf("  Downlink: %.1f Mbps\n", res.DownloadMbps)
	fmt.Printf("  Uplink:   %.1f Mbps\n", res.UploadMbps)
	if res.RPM > 0 {
		fmt.Printf("  Loaded:   %d RPM (%.0f ms loaded vs %.0f ms idle, +%.0f ms bloat)\n",
			res.RPM, res.LoadedRTTMs, res.IdleRTTMs, res.BloatDeltaMs)
	}
	if res.Err != "" {
		fmt.Fprintf(os.Stderr, "warning: %s\n", res.Err)
	}
}

func runDNS(args []string) {
	fs := flag.NewFlagSet("sweetspot dns", flag.ExitOnError)
	asJSON := fs.Bool("json", false, "Output JSON")
	_ = fs.Parse(args)

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	link, _ := wifi.ReadLink(ctx)
	res := probe.RunDNSRace(ctx, link.Gateway)
	if *asJSON {
		emitJSON(res)
		return
	}
	fmt.Printf("Configured DNS: %s\n", strings.Join(link.DNSServers, ", "))
	for _, e := range res.Entries {
		if e.Failed {
			fmt.Printf("  %-11s (%-11s): timeout\n", e.Name, e.Server)
			continue
		}
		tag := ""
		if !e.IsSystem && e.Server == res.Fastest.Server {
			tag = " [FASTEST]"
		}
		fmt.Printf("  %-11s (%-11s): uncached %5.1f ms · cached %5.1f ms%s\n", e.Name, e.Server, e.UncachedMs, e.CachedMs, tag)
	}
}

func runModem(args []string) {
	fs := flag.NewFlagSet("sweetspot modem", flag.ExitOnError)
	clip := fs.Bool("clipboard", false, "Read cable modem status text from clipboard")
	asJSON := fs.Bool("json", false, "Output JSON")
	_ = fs.Parse(reorderFlags(args, fs))

	path := ""
	if fs.NArg() > 0 {
		path = fs.Arg(0)
	} else if !*clip && !isTerminal(os.Stdin) {
		path = "-"
	}

	raw := readModemInput(path, *clip)
	if strings.TrimSpace(raw) == "" {
		fmt.Fprintln(os.Stderr, "Usage: sweetspot modem <file|-> [--clipboard] [--json]")
		os.Exit(2)
	}

	rep := advisor.ParseModemDiagnostics(raw)
	if *asJSON {
		emitJSON(rep)
		return
	}
	if !rep.Parsed {
		fmt.Fprintln(os.Stderr, "sweetspot modem: could not detect DOCSIS channel tables in input")
		os.Exit(1)
	}
	fmt.Printf("DOCSIS Modem Health: %s\n", rep.Health)
	fmt.Printf("  Downstream: %d QAM + %d OFDM · Power %.1f to %.1f dBmV · SNR %.1f to %.1f dB\n",
		rep.DownstreamCount, rep.OFDMCount, rep.DownPowerMin, rep.DownPowerMax, rep.DownSNRMin, rep.DownSNRMax)
	fmt.Printf("  Upstream:   %d channels (OFDMA=%v) · Power %.1f to %.1f dBmV\n",
		rep.UpstreamCount, rep.UpstreamOFDMAActive, rep.UpPowerMin, rep.UpPowerMax)
	fmt.Printf("  Errors:     %d uncorrectable codewords across %d channel(s)\n", rep.TotalUncorrectables, rep.ChannelsWithUncorr)
	for _, f := range rep.Findings {
		fmt.Printf("  [%s] %s\n       -> %s\n", f.Severity, f.Title, f.Action)
	}
}

func runSpot(args []string) {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "Usage: sweetspot spot <pin|list|best|rm|clear> [flags]")
		os.Exit(2)
	}
	store := spots.OpenStore(spots.DefaultPath())
	sub := args[0]
	rest := args[1:]

	switch sub {
	case "pin":
		fs := flag.NewFlagSet("sweetspot spot pin", flag.ExitOnError)
		withSpeed := fs.Bool("speed", false, "Run an active speed burst before pinning")
		wanTarget := fs.String("wan", "1.1.1.1", "WAN ping target")
		asJSON := fs.Bool("json", false, "Output JSON")
		_ = fs.Parse(reorderFlags(rest, fs))
		name := strings.Join(fs.Args(), " ")

		ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
		defer cancel()
		link, _ := wifi.ReadLink(ctx)
		gw := probe.OneShotPing(ctx, link.Gateway, 12, 40.0)
		wan := probe.OneShotPing(ctx, *wanTarget, 12, 80.0)
		var spd probe.SpeedResult
		if *withSpeed {
			spd = probe.RunSpeedBurst(ctx, 4*time.Second, 3*time.Second, 3, wan.AvgMs, nil, nil)
		}
		score := advisor.ComputeScore(link, gw, wan, spd)
		sp := store.Pin(name, link, gw, wan, spd, score)
		if *asJSON {
			emitJSON(sp)
			return
		}
		fmt.Printf("Pinned %q -> %d/100 (%s) · %s · %d dBm / %d dB SNR (saved to %s)\n",
			sp.Name, sp.Score.Total, sp.Score.Grade, sp.ChannelLabel, sp.RSSI, sp.SNR, store.Path())

	case "list", "compare":
		fs := flag.NewFlagSet("sweetspot spot list", flag.ExitOnError)
		asJSON := fs.Bool("json", false, "Output JSON")
		format := fs.String("format", "text", "Output format: text, json, markdown")
		_ = fs.Parse(rest)
		if *asJSON {
			*format = "json"
		}
		ranked := store.Ranked()
		switch *format {
		case "json":
			emitJSON(map[string]any{"path": store.Path(), "count": len(ranked), "spots": ranked})
		case "markdown":
			fmt.Print(spots.FormatMarkdownTable(ranked))
		default:
			if len(ranked) == 0 {
				fmt.Println("No spots pinned yet. Run `sweetspot spot pin \"Desk\" --speed` or press [Space] in the TUI.")
				return
			}
			for i, sp := range ranked {
				fmt.Printf("#%d  %-18s  %3d (%-2s)  %-22s  %3d dBm / %2d dB  gw %.1fms  wan %.1fms  down %.1f / up %.1f Mbps\n",
					i+1, sp.Name, sp.Score.Total, sp.Score.Grade, sp.ChannelLabel,
					sp.RSSI, sp.SNR, sp.GatewayAvgMs, sp.WANAvgMs, sp.DownloadMbps, sp.UploadMbps)
			}
		}

	case "best":
		fs := flag.NewFlagSet("sweetspot spot best", flag.ExitOnError)
		asJSON := fs.Bool("json", false, "Output JSON")
		_ = fs.Parse(rest)
		ranked := store.Ranked()
		if len(ranked) == 0 {
			fmt.Fprintln(os.Stderr, "No spots pinned yet.")
			os.Exit(1)
		}
		best := ranked[0]
		if *asJSON {
			emitJSON(best)
			return
		}
		fmt.Printf("★ Best spot: %s -- %d/100 (%s) · %s · %d dBm / %d dB SNR\n",
			best.Name, best.Score.Total, best.Score.Grade, best.ChannelLabel, best.RSSI, best.SNR)

	case "rm", "delete":
		if len(rest) == 0 {
			fmt.Fprintln(os.Stderr, "Usage: sweetspot spot rm <name>")
			os.Exit(2)
		}
		n := store.DeleteByName(strings.Join(rest, " "))
		fmt.Printf("Removed %d spot(s).\n", n)

	case "clear":
		store.Clear()
		fmt.Println("Cleared all pinned spots.")

	default:
		fmt.Fprintf(os.Stderr, "Unknown spot action %q (expected pin, list, best, rm, clear)\n", sub)
		os.Exit(2)
	}
}

func runDiff(args []string) {
	fs := flag.NewFlagSet("sweetspot diff", flag.ExitOnError)
	asJSON := fs.Bool("json", false, "Output JSON")
	format := fs.String("format", "markdown", "Output format: markdown, json, text")
	_ = fs.Parse(reorderFlags(args, fs))
	if *asJSON {
		*format = "json"
	}
	if fs.NArg() != 2 {
		fmt.Fprintln(os.Stderr, "Usage: sweetspot diff [--format markdown|json|text] <before.json> <after.json>")
		os.Exit(2)
	}

	before := loadReport(fs.Arg(0))
	after := loadReport(fs.Arg(1))
	diff := ComputeDiff(before, after)

	switch *format {
	case "json":
		emitJSON(diff)
	case "text":
		for _, m := range diff.Metrics {
			fmt.Printf("%-26s  %24s  ->  %-24s  (%s)\n", m.Metric, m.Before, m.After, m.Delta)
		}
	default:
		fmt.Print(diff.Markdown())
	}
}

func loadReport(path string) DiagnosticReport {
	data, err := os.ReadFile(path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "sweetspot diff: %v\n", err)
		os.Exit(1)
	}
	var rep DiagnosticReport
	if err := json.Unmarshal(data, &rep); err != nil {
		fmt.Fprintf(os.Stderr, "sweetspot diff: invalid JSON in %s: %v\n", path, err)
		os.Exit(1)
	}
	return rep
}

func printDiagnoseText(rep DiagnosticReport) {
	fmt.Printf("◈ sweetspot  %d/100 (%s) -- %s\n", rep.Score.Total, rep.Score.Grade, rep.Score.Summary)
	fmt.Printf("  Link:    %s · %d dBm / %d dB SNR · %.0f Mbps PHY · %s (%s)\n",
		rep.Link.ChannelLabel(), rep.Link.RSSI, rep.Link.SNR, rep.Link.TxRateMbps, rep.Link.PHYMode, rep.Link.Security)
	printLinkDetails(rep.Link)
	fmt.Printf("  Router:  avg %.1f ms · max %.1f ms · jitter %.1f ms · loss %.1f%% (%s)\n",
		rep.GatewayPing.AvgMs, rep.GatewayPing.MaxMs, rep.GatewayPing.JitterMs, rep.GatewayPing.LossPct, rep.GatewayPing.Target)
	fmt.Printf("  WAN:     avg %.1f ms · max %.1f ms · jitter %.1f ms · loss %.1f%% (%s)\n",
		rep.WANPing.AvgMs, rep.WANPing.MaxMs, rep.WANPing.JitterMs, rep.WANPing.LossPct, rep.WANPing.Target)
	if rep.Speed.DownloadMbps > 0 || rep.Speed.UploadMbps > 0 {
		rpm := ""
		if rep.Speed.RPM > 0 {
			rpm = fmt.Sprintf(" · %d RPM (+%.0f ms bloat)", rep.Speed.RPM, rep.Speed.BloatDeltaMs)
		}
		fmt.Printf("  Speed:   ↓ %.1f Mbps · ↑ %.1f Mbps (%s)%s\n", rep.Speed.DownloadMbps, rep.Speed.UploadMbps, rep.Speed.Source, rpm)
	}
	if !rep.DNS.Fastest.Failed && len(rep.DNS.Entries) > 0 {
		fmt.Printf("  DNS:     fastest %s (%s) %.1f ms uncached vs system %.1f ms\n",
			rep.DNS.Fastest.Name, rep.DNS.Fastest.Server, rep.DNS.Fastest.UncachedMs, rep.DNS.System.UncachedMs)
	}
	if rep.Scan.TotalAPs > 0 {
		fmt.Printf("  RF Scan: %d APs · best 5 GHz Ch %d (non-DFS Ch %d) · best 2.4 GHz Ch %d\n",
			rep.Scan.TotalAPs, rep.Scan.Best5GHzChannel, rep.Scan.Best5GHzNonDFS, rep.Scan.Best2GHzChannel)
	}
	if len(rep.Advice) > 0 {
		fmt.Println("\nRecommendations:")
		for _, a := range rep.Advice {
			fmt.Printf("  [%s] (%s) %s\n       -> %s\n", a.Severity, a.Category, a.Title, a.Action)
		}
	}
}

func printDiagnoseMarkdown(rep DiagnosticReport) {
	fmt.Printf("## SweetSpot Diagnostic (%d/100 · %s)\n\n", rep.Score.Total, rep.Score.Grade)
	fmt.Printf("| Metric | Value |\n|---|---|\n")
	fmt.Printf("| **Wi-Fi Link** | %s · %.0f Mbps PHY (%s) |\n", rep.Link.ChannelLabel(), rep.Link.TxRateMbps, rep.Link.PHYMode)
	fmt.Printf("| **Signal / SNR** | %d dBm / %d dB (noise %d dBm) |\n", rep.Link.RSSI, rep.Link.SNR, rep.Link.Noise)
	if rep.Link.HasCCA {
		fmt.Printf("| **Channel Airtime (CCA)** | %d%% |\n", rep.Link.CCAPct)
	}
	fmt.Printf("| **Router Ping** | avg %.1f ms · max %.1f ms · jitter %.1f ms · loss %.1f%% |\n",
		rep.GatewayPing.AvgMs, rep.GatewayPing.MaxMs, rep.GatewayPing.JitterMs, rep.GatewayPing.LossPct)
	fmt.Printf("| **WAN Ping (%s)** | avg %.1f ms · max %.1f ms · jitter %.1f ms · loss %.1f%% |\n",
		rep.WANPing.Target, rep.WANPing.AvgMs, rep.WANPing.MaxMs, rep.WANPing.JitterMs, rep.WANPing.LossPct)
	if rep.Speed.DownloadMbps > 0 {
		fmt.Printf("| **Throughput** | ↓ %.1f Mbps · ↑ %.1f Mbps (%d RPM, +%.0f ms bloat) |\n",
			rep.Speed.DownloadMbps, rep.Speed.UploadMbps, rep.Speed.RPM, rep.Speed.BloatDeltaMs)
	}
	if len(rep.Advice) > 0 {
		fmt.Printf("\n### Recommendations\n\n")
		for _, a := range rep.Advice {
			fmt.Printf("- **[%s] %s** (%s): %s\n", a.Severity, a.Title, a.Category, a.Action)
		}
	}
}

func printLinkDetails(link wifi.LinkInfo) {
	var details []string
	if link.LinkQuality > 0 {
		details = append(details, fmt.Sprintf("LQM %d", link.LinkQuality))
	}
	if link.EffDownMbps > 0 {
		details = append(details, fmt.Sprintf("Eff %.0f/%.0f Mbps", link.EffDownMbps, link.EffUpMbps))
	}
	if link.Scheduler != "" {
		s := link.Scheduler
		if link.QoSMode != "" {
			s += "/" + link.QoSMode
		}
		details = append(details, s)
	}
	if link.HasCCA {
		details = append(details, fmt.Sprintf("CCA %d%%", link.CCAPct))
	}
	if link.HasMCS {
		m := fmt.Sprintf("MCS %d", link.MCS)
		if link.NSS > 0 {
			m += fmt.Sprintf(" %dx%d", link.NSS, link.NSS)
		}
		details = append(details, m)
	}
	if link.BSSID != "" {
		details = append(details, "BSSID "+link.BSSID)
	}
	if len(details) > 0 {
		fmt.Printf("  Details: %s\n", strings.Join(details, " · "))
	}
}

func readModemInput(path string, fromClipboard bool) string {
	if fromClipboard {
		var out []byte
		var err error
		switch runtime.GOOS {
		case "darwin":
			out, err = exec.Command("/usr/bin/pbpaste").Output()
		case "windows":
			out, err = exec.Command("powershell", "-NoProfile", "-Command", "Get-Clipboard").Output()
		default:
			out, err = exec.Command("wl-paste", "-n").Output()
			if err != nil {
				out, err = exec.Command("xclip", "-o", "-selection", "clipboard").Output()
			}
		}
		if err == nil {
			return string(out)
		}
		return ""
	}
	if path == "" {
		return ""
	}
	if path == "-" {
		b, err := io.ReadAll(os.Stdin)
		if err != nil {
			fmt.Fprintf(os.Stderr, "sweetspot: failed reading stdin: %v\n", err)
			os.Exit(1)
		}
		return string(b)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "sweetspot: failed reading %s: %v\n", path, err)
		os.Exit(1)
	}
	return string(b)
}

func emitJSON(v any) {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	_ = enc.Encode(v)
}

func isTerminal(f *os.File) bool {
	st, err := f.Stat()
	if err != nil {
		return false
	}
	return (st.Mode() & os.ModeCharDevice) != 0
}

// reorderFlags moves flags ahead of positional arguments so Go's flag.FlagSet accepts
// ergonomic invocations like `sweetspot spot pin "Desk" --speed --json` or `sweetspot diff a.json b.json --json`.
func reorderFlags(args []string, fs *flag.FlagSet) []string {
	type boolFlag interface {
		IsBoolFlag() bool
	}
	var flagsPart, posPart []string
	for i := 0; i < len(args); i++ {
		a := args[i]
		if a == "--" {
			posPart = append(posPart, args[i+1:]...)
			break
		}
		if len(a) > 1 && strings.HasPrefix(a, "-") {
			flagsPart = append(flagsPart, a)
			if !strings.Contains(a, "=") {
				name := strings.TrimLeft(a, "-")
				if f := fs.Lookup(name); f != nil {
					if bf, ok := f.Value.(boolFlag); !ok || !bf.IsBoolFlag() {
						if i+1 < len(args) {
							i++
							flagsPart = append(flagsPart, args[i])
						}
					}
				}
			}
			continue
		}
		posPart = append(posPart, a)
	}
	return append(flagsPart, posPart...)
}

func printUsage() {
	fmt.Fprintf(os.Stdout, `sweetspot %s -- interactive Wi-Fi spot finder and network/modem advisor

Usage:
  sweetspot [flags]                           Launch interactive TUI (auto-runs diagnose if non-TTY)
  sweetspot tui [--continuous]                Launch interactive TUI explicitly
  sweetspot status [--json|--format markdown] Instant (<100ms) RF & link snapshot (no active probes)
  sweetspot diagnose [flags]                  Full sequential diagnostic + Advisor + Playbook
  sweetspot scan [--json|--format markdown]   Scan nearby 2.4 / 5 / 6 GHz channels for congestion
  sweetspot ping [--count 20] [--json]        Dual-hop idle ICMP ping (router gateway + WAN)
  sweetspot speed [--network-quality] [--json] Download, upload, and loaded bufferbloat/RPM test
  sweetspot dns [--json]                      Race System DNS vs 1.1.1.1, 9.9.9.9, 8.8.8.8, Router
  sweetspot modem [<file>| - | --clipboard]   Parse DOCSIS 3.0/3.1 cable modem status & coax health
  sweetspot spot <pin|list|best|rm|clear>     Manage persistent spots (shared with TUI)
  sweetspot diff <before.json> <after.json>   Render Before vs. After comparison table

Agent / script examples:
  sweetspot status --json
  sweetspot diagnose --speed --scan --out before.json --json
  sweetspot spot pin "Desk Window" --speed --json
  sweetspot spot best --json
  pbpaste | sweetspot modem - --json
  sweetspot diff before.json after.json --format markdown

Exit codes:
  0  Success
  1  Runtime error
  2  Invalid arguments
  3  SweetSpot score fell below --fail-below threshold
`, version)
}
