package ui

import (
	"fmt"
	"math"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/bvolpato/sweetspot/internal/advisor"
	"github.com/bvolpato/sweetspot/internal/probe"
	"github.com/bvolpato/sweetspot/internal/wifi"
)

func (m *Model) View() string {
	w := m.width
	if w < 76 {
		w = 76
	}
	h := m.height
	if h < 26 {
		h = 26
	}

	var sections []string
	sections = append(sections, m.renderHeader(w))

	if m.pinning {
		sections = append(sections, m.renderPinModal(w))
	} else {
		switch m.tab {
		case TabRadar:
			sections = append(sections, m.renderRadarView(w, h))
		case TabSpots:
			sections = append(sections, m.renderSpotsView(w))
		case TabSpectrum:
			sections = append(sections, m.renderSpectrumView(w))
		case TabAdvisor:
			sections = append(sections, m.renderAdvisorView(w))
		}
	}

	sections = append(sections, m.renderFooter(w))
	return lipgloss.JoinVertical(lipgloss.Left, sections...)
}

func (m *Model) renderHeader(w int) string {
	logo := lipgloss.NewStyle().Bold(true).Foreground(ColCyan).Render("◈ sweetspot")
	chanBadge := lipgloss.NewStyle().Foreground(ColText).Background(ColTrack).Padding(0, 1).Render(m.link.ChannelLabel())

	var statusPill string
	switch {
	case m.scanning:
		statusPill = StylePillBusy.Render(m.spinner.View() + " RF SCAN")
	case m.speedRunning:
		statusPill = StylePillBusy.Render(m.spinner.View() + " " + strings.ToUpper(m.speedProgress.Phase))
	case m.continuousMode:
		statusPill = StylePillWarn.Render("⚡ CONT SPEED")
	case m.link.Connected || m.link.IsEthernet:
		statusPill = StylePillLive.Render("● LIVE")
	default:
		statusPill = Badge("OFFLINE", ColCoral)
	}

	scoreCol := ColorForScore(m.score.Total)
	scoreLabel := fmt.Sprintf("%d/100 · %s", m.score.Total, m.score.Grade)
	if m.speed.DownloadMbps == 0 && !m.speedRunning {
		scoreLabel = fmt.Sprintf("~%d/100 · %s", m.score.Total, m.score.Grade)
	}
	scorePill := Badge(scoreLabel, scoreCol)

	tabs := []struct {
		id    Tab
		label string
	}{
		{TabRadar, "1 Radar"},
		{TabSpots, fmt.Sprintf("2 Spots (%d)", m.spotsStore.Len())},
		{TabSpectrum, "3 RF & DNS"},
		{TabAdvisor, fmt.Sprintf("4 Advisor (%d)", len(m.advice))},
	}
	var tabParts []string
	for _, t := range tabs {
		if m.tab == t.id {
			tabParts = append(tabParts, StyleTabActive.Render(t.label))
		} else {
			tabParts = append(tabParts, StyleTabInactive.Render(t.label))
		}
	}

	topLeft := logo + "  " + chanBadge
	if m.link.SSID != "" {
		topLeft += " " + StyleMuted.Render(m.link.SSID)
	}
	sudoBadge := ""
	if m.link.Privileged {
		sudoBadge = Badge("SUDO+", ColViolet) + " "
	}
	topRight := sudoBadge + statusPill + " " + scorePill
	gap := w - lipgloss.Width(topLeft) - lipgloss.Width(topRight) - 2
	if gap < 1 {
		gap = 1
	}
	row1 := " " + topLeft + strings.Repeat(" ", gap) + topRight
	row2 := " " + strings.Join(tabParts, " ")
	divider := lipgloss.NewStyle().Foreground(ColBorder).Render(strings.Repeat("─", max(w-2, 40)))
	return lipgloss.JoinVertical(lipgloss.Left, row1, row2, " "+divider)
}

func (m *Model) renderRadarView(w, h int) string {
	twoCol := w >= 104
	cardW := w - 2
	leftW := cardW
	rightW := cardW
	if twoCol {
		leftW = (cardW - 1) / 2
		rightW = cardW - 1 - leftW
	}

	// Dynamically size Braille charts so the dashboard fills the terminal cleanly.
	// Fixed chrome: header (3) + footer (2) + advisor card (5) + borders/headers inside 2 stacked rows (12) = ~22 rows.
	availCharts := h - 24
	if !twoCol {
		availCharts = 6
	}
	topChartH := 4
	latChartH := 4
	if availCharts > 8 {
		topChartH = min(4+(availCharts-8)/2, 8)
		latChartH = min(4+(availCharts-8)-(topChartH-4), 8)
	}

	topCardH := 8 + topChartH
	latCardH := 4 + latChartH

	signalCard := m.renderSignalCard(leftW, topCardH, topChartH)
	speedCard := m.renderSpeedCard(rightW, topCardH, topChartH)
	gwCard := m.renderLatencyCard("Router Hop", m.gwStats, 10, 35, leftW, latCardH, latChartH)
	wanCard := m.renderLatencyCard("Internet ("+m.opts.WANTarget+")", m.wanStats, 25, 70, rightW, latCardH, latChartH)
	advisorStrip := m.renderQuickAdvisorCard(cardW)

	if twoCol {
		row1 := lipgloss.JoinHorizontal(lipgloss.Top, signalCard, " ", speedCard)
		row2 := lipgloss.JoinHorizontal(lipgloss.Top, gwCard, " ", wanCard)
		return lipgloss.JoinVertical(lipgloss.Left, row1, row2, advisorStrip)
	}
	return lipgloss.JoinVertical(lipgloss.Left, signalCard, speedCard, gwCard, wanCard, advisorStrip)
}

func (m *Model) renderSignalCard(w, cardH, chartH int) string {
	inner := w - 4
	header := CardHeader("◉", "SIGNAL RADAR & SWEETSPOT", m.link.QualityLabel(), inner)

	tower := RenderSignalTower(m.link.RSSI, m.link.Connected, m.link.IsEthernet)

	rssiCol := ColorForRSSI(m.link.RSSI)
	var bigRSSI string
	if m.link.IsEthernet {
		bigRSSI = RenderBigNumber("100", ColEmerald)
	} else if m.link.Connected {
		bigRSSI = RenderBigNumber(fmt.Sprintf("%d", m.link.RSSI), rssiCol)
	} else {
		bigRSSI = RenderBigNumber("0", ColCoral)
	}

	unitCol := lipgloss.JoinVertical(lipgloss.Left,
		lipgloss.NewStyle().Bold(true).Foreground(rssiCol).Render("dBm"),
		StyleSubtitle.Render(fmt.Sprintf("SNR %d dB", m.link.SNR)),
		StyleMuted.Render(fmt.Sprintf("Noise %d", m.link.Noise)),
	)
	if m.link.IsEthernet {
		unitCol = lipgloss.JoinVertical(lipgloss.Left,
			lipgloss.NewStyle().Bold(true).Foreground(ColEmerald).Render("ETH"),
			StyleSubtitle.Render("Wired Link"),
			StyleMuted.Render("No RF loss"),
		)
	}

	scoreCol := ColorForScore(m.score.Total)
	bigScore := RenderBigNumber(fmt.Sprintf("%d", m.score.Total), scoreCol)
	scoreMeta := lipgloss.JoinVertical(lipgloss.Left,
		Badge(m.score.Grade, scoreCol),
		StyleSubtitle.Render(fmt.Sprintf("RF %d · Lat %d", m.score.SignalScore, m.score.LatScore)),
		StyleMuted.Render(fmt.Sprintf("Spd %d/30", m.score.SpeedScore)),
	)

	sep := lipgloss.NewStyle().Foreground(ColBorder).Render("  │  \n  │  \n  │  ")
	heroLeft := lipgloss.JoinHorizontal(lipgloss.Bottom, tower, "  ", bigRSSI, " ", unitCol)
	heroRight := lipgloss.JoinHorizontal(lipgloss.Bottom, bigScore, " ", scoreMeta)
	hero := lipgloss.JoinHorizontal(lipgloss.Bottom, heroLeft, sep, heroRight)

	barFrac := (float64(m.link.RSSI) + 90.0) / 55.0
	if m.link.IsEthernet {
		barFrac = 1.0
	}
	meter := RenderGradientBar(barFrac, max(inner-2, 16))

	extra := []string{
		fmt.Sprintf("PHY %s", lipgloss.NewStyle().Bold(true).Foreground(ColCyan).Render(fmt.Sprintf("%.0f Mbps", m.link.TxRateMbps))),
		StyleValue.Render(m.link.PHYMode),
	}
	if m.link.EffDownMbps > 0 {
		extra = append(extra, StyleSubtitle.Render(fmt.Sprintf("Eff %.0f Mbps", m.link.EffDownMbps)))
	}
	if m.link.LinkQuality > 0 {
		extra = append(extra, StyleSubtitle.Render(fmt.Sprintf("LQM %d", m.link.LinkQuality)))
	}
	if m.link.HasCCA {
		ccaCol := ColEmerald
		if m.link.CCAPct >= 65 {
			ccaCol = ColCoral
		} else if m.link.CCAPct >= 45 {
			ccaCol = ColAmber
		}
		extra = append(extra, lipgloss.NewStyle().Bold(true).Foreground(ccaCol).Render(fmt.Sprintf("CCA %d%%", m.link.CCAPct)))
	}
	if m.link.HasMCS {
		mcsStr := fmt.Sprintf("MCS %d", m.link.MCS)
		if m.link.NSS > 0 {
			mcsStr += fmt.Sprintf(" %dx%d", m.link.NSS, m.link.NSS)
		}
		extra = append(extra, StyleSubtitle.Render(mcsStr))
	}
	if m.link.BSSID != "" {
		bssidStr := m.link.BSSID
		if m.roamCount > 0 {
			bssidStr += fmt.Sprintf(" (%d roams)", m.roamCount)
		}
		extra = append(extra, StyleMuted.Render(bssidStr))
	} else if m.link.Scheduler != "" {
		q := m.link.Scheduler
		if m.link.QoSMode != "" {
			q += "/" + m.link.QoSMode
		}
		extra = append(extra, StyleMuted.Render(q))
	}
	radio := strings.Join(extra, " · ")

	rssiChart := BrailleChart(m.rssiHist, inner, chartH, -85, -35, true, "dBm", func(v float64) lipgloss.Color {
		return ColorForRSSI(int(v))
	})

	body := lipgloss.JoinVertical(lipgloss.Left,
		header,
		hero,
		meter,
		radio,
		rssiChart,
	)
	return StyleCardHi.Width(w - 2).Height(cardH).Render(body)
}

func (m *Model) renderSpeedCard(w, cardH, chartH int) string {
	inner := w - 4
	right := "[b] burst · [c] cont · [n] netQuality"
	if m.speedRunning {
		right = m.spinner.View() + " running " + m.speedProgress.Phase
	} else if m.speed.Source != "" {
		right = m.speed.Source + " · " + m.speed.Timestamp.Format("15:04:05")
	}
	header := CardHeader("⚡", "THROUGHPUT & BUFFERBLOAT", right, inner)

	dl := m.speed.DownloadMbps
	ul := m.speed.UploadMbps
	if m.speedRunning {
		if m.speedProgress.DownloadMbps > 0 {
			dl = m.speedProgress.DownloadMbps
		}
		if m.speedProgress.UploadMbps > 0 {
			ul = m.speedProgress.UploadMbps
		}
	}

	barW := max(inner-24, 10)
	dlLabel := lipgloss.NewStyle().Foreground(ColCyan).Bold(true).Width(8).Render("↓ DOWN")
	ulLabel := lipgloss.NewStyle().Foreground(ColViolet).Bold(true).Width(8).Render("↑ UP")
	var dlRow, ulRow, bloatBlock string

	if dl == 0 && ul == 0 && !m.speedRunning {
		ctaDown := lipgloss.NewStyle().Bold(true).Foreground(ColCyan).Render("press [b] to measure") +
			StyleMuted.Render("  (or [c] continuous)")
		ctaUp := lipgloss.NewStyle().Bold(true).Foreground(ColViolet).Render("press [b] to measure") +
			StyleMuted.Render("  (or [n] netQuality)")
		dlRow = dlLabel + " " + ctaDown
		ulRow = ulLabel + " " + ctaUp
		bloatBlock = lipgloss.JoinVertical(lipgloss.Left,
			fmt.Sprintf("%s  %s",
				Badge("NOT TESTED", ColTrackHi),
				StyleSubtitle.Render("Live "+m.link.Interface+" traffic charted below (zero extra bandwidth)"),
			),
			fmt.Sprintf("Live %s: %s  ·  %s",
				StyleMuted.Render(m.link.Interface),
				lipgloss.NewStyle().Bold(true).Foreground(ColCyan).Render("↓ "+FormatMbps(m.link.LiveRxMbps)),
				lipgloss.NewStyle().Bold(true).Foreground(ColViolet).Render("↑ "+FormatMbps(m.link.LiveTxMbps)),
			),
		)
	} else {
		dlRow = fmt.Sprintf("%s %s %s",
			dlLabel,
			RenderBar(math.Min(dl/800.0, 1.0), barW, ColCyan),
			StyleValue.Render(fmt.Sprintf("%10s", FormatMbps(dl))),
		)
		ulRow = fmt.Sprintf("%s %s %s",
			ulLabel,
			RenderBar(math.Min(ul/150.0, 1.0), barW, ColViolet),
			StyleValue.Render(fmt.Sprintf("%10s", FormatMbps(ul))),
		)

		bloatLine := StyleMuted.Render("Bufferbloat: sampling loaded latency...")
		if m.speed.LoadedRTTMs > 0 || m.speed.RPM > 0 {
			rpmCol := ColEmerald
			grade := "LOW BLOAT"
			if m.speed.RPM < 400 {
				rpmCol = ColCoral
				grade = "HIGH BLOAT"
			} else if m.speed.RPM < 800 {
				rpmCol = ColAmber
				grade = "MED BLOAT"
			}
			bloatLine = fmt.Sprintf("%s  Loaded %s  (%s · %s bloat)",
				Badge(grade, rpmCol),
				lipgloss.NewStyle().Bold(true).Foreground(rpmCol).Render(fmt.Sprintf("%d RPM", m.speed.RPM)),
				StyleValue.Render(fmt.Sprintf("%.0f ms", m.speed.LoadedRTTMs)),
				lipgloss.NewStyle().Foreground(rpmCol).Render(fmt.Sprintf("+%.0f ms", m.speed.BloatDeltaMs)),
			)
		}
		liveLine := fmt.Sprintf("Live %s: %s  ·  %s",
			StyleMuted.Render(m.link.Interface),
			lipgloss.NewStyle().Foreground(ColCyan).Render("↓ "+FormatMbps(m.link.LiveRxMbps)),
			lipgloss.NewStyle().Foreground(ColViolet).Render("↑ "+FormatMbps(m.link.LiveTxMbps)),
		)
		bloatBlock = lipgloss.JoinVertical(lipgloss.Left, bloatLine, liveLine)
	}

	chartTitle := StyleMuted.Render("Live RX Throughput (Mbps)")
	if m.speedRunning || m.speed.DownloadMbps > 0 {
		chartTitle = StyleMuted.Render("Throughput History (Mbps)")
	}
	speedChart := BrailleChart(m.downHist, inner, chartH, 0, 0, false, "M", func(v float64) lipgloss.Color {
		if v >= 250 {
			return ColEmerald
		}
		if v >= 60 {
			return ColCyan
		}
		return ColViolet
	})

	body := lipgloss.JoinVertical(lipgloss.Left,
		header,
		"",
		dlRow,
		ulRow,
		bloatBlock,
		chartTitle,
		speedChart,
	)
	return StyleCard.Width(w - 2).Height(cardH).Render(body)
}

func (m *Model) renderLatencyCard(title string, st probe.PingStats, goodMax, warnMax float64, w, cardH, chartH int) string {
	inner := w - 4
	right := st.Target
	if right == "" {
		right = "detecting..."
	}
	header := CardHeader("∿", strings.ToUpper(title), right, inner)

	chart := BrailleChart(st.History, inner, chartH, 0, 0, false, "ms", func(v float64) lipgloss.Color {
		return ColorForLatency(v, goodMax, warnMax)
	})

	lastCol := ColorForLatency(st.LastMs, goodMax, warnMax)
	lossCol := ColEmerald
	if st.LossPct > 0 {
		lossCol = ColCoral
	}

	stats1 := fmt.Sprintf("Now %s  ·  Avg %s  ·  P95 %s  ·  Max %s",
		lipgloss.NewStyle().Bold(true).Foreground(lastCol).Render(fmt.Sprintf("%.1fms", st.LastMs)),
		StyleValue.Render(fmt.Sprintf("%.1fms", st.AvgMs)),
		StyleValue.Render(fmt.Sprintf("%.1fms", st.P95Ms)),
		lipgloss.NewStyle().Foreground(ColorForLatency(st.MaxMs, goodMax, warnMax)).Render(fmt.Sprintf("%.1fms", st.MaxMs)),
	)
	stats2 := fmt.Sprintf("Jitter %s  ·  Loss %s  ·  Spikes %s  ·  Samples %d",
		StyleValue.Render(fmt.Sprintf("%.1fms", st.JitterMs)),
		lipgloss.NewStyle().Bold(true).Foreground(lossCol).Render(fmt.Sprintf("%.1f%%", st.LossPct)),
		StyleValue.Render(fmt.Sprintf("%d", st.SpikeCount)),
		st.Sent,
	)

	body := lipgloss.JoinVertical(lipgloss.Left, header, chart, stats1, StyleMuted.Render(stats2))
	return StyleCard.Width(w - 2).Height(cardH).Render(body)
}

func (m *Model) renderQuickAdvisorCard(w int) string {
	inner := w - 4
	header := CardHeader("✦", "LIVE ADVISOR", "Press [4] for full breakdown · [Space] to pin this spot", inner)
	var lines []string
	lines = append(lines, header)
	shown := 0
	for _, a := range m.advice {
		if shown >= 2 {
			break
		}
		badge := severityBadge(a.Severity)
		lines = append(lines, fmt.Sprintf("%s %s -- %s",
			badge,
			StyleValue.Render(a.Title),
			StyleSubtitle.Render(truncate(a.Action, max(inner-len(a.Title)-16, 24))),
		))
		shown++
	}
	if shown == 0 {
		lines = append(lines, StyleMuted.Render("Collecting baseline telemetry..."))
	}
	return StyleCard.Width(w - 2).Render(lipgloss.JoinVertical(lipgloss.Left, lines...))
}

func (m *Model) renderSpotsView(w int) string {
	inner := w - 4
	ranked := m.spotsStore.Ranked()
	header := CardHeader("★", "SPOT FINDER LEADERBOARD", "Walk around · press [Space] to pin · [d] delete · [e] export", inner)

	if len(ranked) == 0 {
		empty := lipgloss.JoinVertical(lipgloss.Left,
			header,
			"",
			StyleValue.Render("No spots pinned yet."),
			StyleSubtitle.Render("Walk to a desk or room, optionally run [b] for a quick speed burst, then press [Space] to pin it."),
			StyleMuted.Render("SweetSpot ranks every spot side by side by RF signal, jitter, speed, and bufferbloat."),
		)
		return StyleCardHi.Width(w - 2).Render(empty)
	}

	best := ranked[0]
	var rows []string
	rows = append(rows, header, "")
	rows = append(rows, StyleMuted.Render(fmt.Sprintf("%-4s %-18s %-11s %-22s %-14s %-10s %-14s %-7s",
		"RANK", "SPOT", "SCORE", "CHANNEL", "RSSI / SNR", "GW / WAN", "DOWN / UP", "RPM")))

	for i, sp := range ranked {
		rankLabel := fmt.Sprintf("#%d", i+1)
		if i == 0 {
			rankLabel = "★#1"
		}
		spd := "   -   "
		if sp.DownloadMbps > 0 {
			spd = fmt.Sprintf("%4.0f / %3.0f", sp.DownloadMbps, sp.UploadMbps)
		}
		rpm := "-"
		if sp.LoadedRPM > 0 {
			rpm = fmt.Sprintf("%d", sp.LoadedRPM)
		}
		line := fmt.Sprintf("%-4s %-18s %3d (%-2s)    %-22s %3d / %2d dB    %4.1f/%4.1f  %-14s %-7s",
			rankLabel,
			truncate(sp.Name, 18),
			sp.Score.Total, sp.Score.Grade,
			truncate(sp.ChannelLabel, 22),
			sp.RSSI, sp.SNR,
			sp.GatewayAvgMs, sp.WANAvgMs,
			spd, rpm,
		)
		style := lipgloss.NewStyle().Foreground(ColText)
		if i == 0 {
			style = style.Foreground(ColEmerald).Bold(true)
		}
		if i == m.selectedIdx {
			style = style.Background(ColTrack)
		}
		rows = append(rows, style.Render(line))
	}

	if len(ranked) >= 2 {
		worst := ranked[len(ranked)-1]
		rows = append(rows, "", StyleSubtitle.Render(fmt.Sprintf(
			"Winner: %s (+%d score, %+d dB SNR vs %s)",
			best.Name, best.Score.Total-worst.Score.Total, best.SNR-worst.SNR, worst.Name,
		)))
	}

	return StyleCardHi.Width(w - 2).Render(lipgloss.JoinVertical(lipgloss.Left, rows...))
}

func (m *Model) renderSpectrumView(w int) string {
	inner := w - 4

	rfRight := "Press [s] to scan nearby channels"
	if m.scanning {
		rfRight = m.spinner.View() + " scanning..."
	} else if m.scan.TotalAPs > 0 {
		rfRight = fmt.Sprintf("%d APs · best 5 GHz: Ch %d (non-DFS Ch %d) · best 2.4 GHz: Ch %d",
			m.scan.TotalAPs, m.scan.Best5GHzChannel, m.scan.Best5GHzNonDFS, m.scan.Best2GHzChannel)
	}
	rfHeader := CardHeader("▤", "RF CHANNEL CONGESTION", rfRight, inner)

	var rfLines []string
	rfLines = append(rfLines, rfHeader)
	if len(m.scan.Buckets) == 0 {
		rfLines = append(rfLines, StyleSubtitle.Render("No RF scan yet. Press [s] to scan neighboring APs (takes ~2-4s, skipped during idle monitoring to avoid scan jitter)."))
	} else {
		maxCount := 1
		for _, b := range m.scan.Buckets {
			if b.Count > maxCount {
				maxCount = b.Count
			}
		}
		barW := max(inner-46, 12)
		shown := 0
		for _, b := range m.scan.Buckets {
			if shown >= 14 {
				break
			}
			col := ColCyan
			if b.Count >= 5 {
				col = ColCoral
			} else if b.Count >= 3 {
				col = ColAmber
			} else if b.Band == wifi.Band6GHz {
				col = ColEmerald
			}
			tag := ""
			if b.IsCurrent {
				tag = " " + Badge("YOU", ColCyan)
			} else if b.IsRecommended {
				tag = " " + Badge("CLEAN", ColEmerald)
			}
			sigStr := ""
			if b.MaxRSSI != 0 {
				sigStr = fmt.Sprintf("max %d dBm", b.MaxRSSI)
			}
			rfLines = append(rfLines, fmt.Sprintf("%-7s Ch %-3d (%3dMHz) %s %2d APs %s%s",
				b.Band, b.Channel, b.MaxWidthMHz,
				RenderBar(float64(b.Count)/float64(maxCount), barW, col),
				b.Count, StyleMuted.Render(sigStr), tag,
			))
			shown++
		}
	}
	rfCard := StyleCard.Width(w - 2).Render(lipgloss.JoinVertical(lipgloss.Left, rfLines...))

	dnsRight := "Press [r] to re-race"
	if m.dnsRunning {
		dnsRight = m.spinner.View() + " racing..."
	} else if len(m.link.DNSServers) > 0 {
		dnsRight = "Active: " + strings.Join(m.link.DNSServers, ", ")
	}
	dnsHeader := CardHeader("⌁", "DNS RESOLVER RACE", dnsRight, inner)

	var dnsLines []string
	dnsLines = append(dnsLines, dnsHeader)
	if len(m.dns.Entries) == 0 {
		dnsLines = append(dnsLines, StyleSubtitle.Render("Racing DNS resolvers..."))
	} else {
		maxMs := 60.0
		for _, e := range m.dns.Entries {
			if e.UncachedMs > maxMs {
				maxMs = e.UncachedMs
			}
		}
		barW := max(inner-52, 12)
		for _, e := range m.dns.Entries {
			if e.Failed {
				dnsLines = append(dnsLines, fmt.Sprintf("%-11s (%-11s) %s", e.Name, e.Server, StyleMuted.Render("timeout")))
				continue
			}
			col := ColorForLatency(e.UncachedMs, 25, 45)
			tag := ""
			if !e.IsSystem && e.Server == m.dns.Fastest.Server {
				tag = " " + Badge("FASTEST", ColEmerald)
			}
			dnsLines = append(dnsLines, fmt.Sprintf("%-11s (%-11s) %s uncached %5.1fms · cached %5.1fms%s",
				e.Name, e.Server,
				RenderBar(e.UncachedMs/maxMs, barW, col),
				e.UncachedMs, e.CachedMs, tag,
			))
		}
	}
	dnsCard := StyleCard.Width(w - 2).Render(lipgloss.JoinVertical(lipgloss.Left, dnsLines...))

	return lipgloss.JoinVertical(lipgloss.Left, rfCard, dnsCard)
}

func (m *Model) renderAdvisorView(w int) string {
	inner := w - 4
	modemRight := "Press [m] to analyze cable modem stats from clipboard"
	if m.modem.Parsed {
		modemRight = fmt.Sprintf("Modem: %s · Down %.1f..%.1f dBmV · SNR %.1f dB · Up %.1f dBmV",
			m.modem.Health, m.modem.DownPowerMin, m.modem.DownPowerMax, m.modem.DownSNRMin, m.modem.UpPowerMax)
	}
	header := CardHeader("✦", "NETWORK & MODEM ADVISOR", modemRight, inner)

	var lines []string
	lines = append(lines, header, "")
	for _, a := range m.advice {
		lines = append(lines, fmt.Sprintf("%s %s %s",
			severityBadge(a.Severity),
			StyleMuted.Render("["+a.Category+"]"),
			StyleValue.Render(a.Title),
		))
		lines = append(lines, "   "+StyleSubtitle.Render(a.Detail))
		lines = append(lines, "   "+lipgloss.NewStyle().Foreground(ColTeal).Render("→ "+a.Action))
		lines = append(lines, "")
	}
	if len(m.advice) == 0 {
		lines = append(lines, StyleSubtitle.Render("Collecting measurements..."))
	}
	return StyleCardHi.Width(w - 2).Render(lipgloss.JoinVertical(lipgloss.Left, lines...))
}

func (m *Model) renderPinModal(w int) string {
	boxW := min(w-8, 60)
	content := lipgloss.JoinVertical(lipgloss.Left,
		lipgloss.NewStyle().Bold(true).Foreground(ColCyan).Render("★ Pin Current Spot"),
		"",
		StyleSubtitle.Render(fmt.Sprintf("Score %d (%s) · %s · %d dBm / %d dB SNR",
			m.score.Total, m.score.Grade, m.link.ChannelLabel(), m.link.RSSI, m.link.SNR)),
		"",
		m.pinInput.View(),
		"",
		StyleMuted.Render("Press [Enter] to save (blank = auto name) · [Esc] to cancel"),
	)
	card := StyleCardHi.Width(boxW).Padding(1, 2).Render(content)
	return lipgloss.PlaceHorizontal(w, lipgloss.Center, "\n"+card+"\n")
}

func (m *Model) renderFooter(w int) string {
	key := func(k, label string) string {
		return lipgloss.NewStyle().Bold(true).Foreground(ColCyan).Background(ColTrack).Render(" "+k+" ") +
			StyleMuted.Render(" "+label)
	}
	bar := strings.Join([]string{
		key("1-4", "Views"),
		key("Space", "Pin"),
		key("b", "Speed Burst"),
		key("c", "Continuous"),
		key("n", "netQuality"),
		key("s", "RF Scan"),
		key("r", "DNS"),
		key("m", "Modem"),
		key("e", "Export"),
		key("q", "Quit"),
	}, "  ")
	if m.toast != "" {
		return StyleFooter.Width(w).Render(StyleToast.Render("● "+m.toast) + "\n" + bar)
	}
	return StyleFooter.Width(w).Render(bar)
}

func severityBadge(s advisor.Severity) string {
	switch s {
	case advisor.SeverityCritical:
		return Badge("CRIT", ColCoral)
	case advisor.SeverityHigh:
		return Badge("HIGH", ColAmber)
	case advisor.SeverityTip:
		return Badge("TIP", ColCyan)
	default:
		return Badge("GOOD", ColEmerald)
	}
}

func truncate(s string, maxLen int) string {
	if maxLen <= 3 || len(s) <= maxLen {
		return s
	}
	return s[:maxLen-1] + "…"
}
