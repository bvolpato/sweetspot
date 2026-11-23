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
	if w < 72 {
		w = 72
	}

	var sections []string
	sections = append(sections, m.renderHeader(w))

	if m.pinning {
		sections = append(sections, m.renderPinModal(w))
	} else {
		switch m.tab {
		case TabRadar:
			sections = append(sections, m.renderRadarView(w))
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
	scorePill := Badge(fmt.Sprintf("%d/100 · %s", m.score.Total, m.score.Grade), scoreCol)

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
	topRight := statusPill + " " + scorePill
	gap := w - lipgloss.Width(topLeft) - lipgloss.Width(topRight) - 2
	if gap < 1 {
		gap = 1
	}
	row1 := " " + topLeft + strings.Repeat(" ", gap) + topRight
	row2 := " " + strings.Join(tabParts, " ")
	divider := lipgloss.NewStyle().Foreground(ColBorder).Render(strings.Repeat("─", max(w-2, 40)))
	return lipgloss.JoinVertical(lipgloss.Left, row1, row2, " "+divider)
}

func (m *Model) renderRadarView(w int) string {
	twoCol := w >= 104
	cardW := w - 2
	leftW := cardW
	rightW := cardW
	if twoCol {
		leftW = (cardW - 1) / 2
		rightW = cardW - 1 - leftW
	}

	signalCard := m.renderSignalCard(leftW)
	speedCard := m.renderSpeedCard(rightW)
	gwCard := m.renderLatencyCard("Router Hop", m.gwStats, 10, 35, leftW)
	wanCard := m.renderLatencyCard("Internet ("+m.opts.WANTarget+")", m.wanStats, 25, 70, rightW)
	advisorStrip := m.renderQuickAdvisorCard(cardW)

	if twoCol {
		row1 := lipgloss.JoinHorizontal(lipgloss.Top, signalCard, " ", speedCard)
		row2 := lipgloss.JoinHorizontal(lipgloss.Top, gwCard, " ", wanCard)
		return lipgloss.JoinVertical(lipgloss.Left, row1, row2, advisorStrip)
	}
	return lipgloss.JoinVertical(lipgloss.Left, signalCard, speedCard, gwCard, wanCard, advisorStrip)
}

func (m *Model) renderSignalCard(w int) string {
	inner := w - 4
	header := CardHeader("◉", "SIGNAL & SWEETSPOT SCORE", m.link.QualityLabel(), inner)

	scoreCol := ColorForScore(m.score.Total)
	scoreBig := lipgloss.NewStyle().Bold(true).Foreground(scoreCol).Render(
		fmt.Sprintf("%d", m.score.Total),
	) + StyleMuted.Render("/100 ") + Badge(m.score.Grade, scoreCol)

	rssiCol := ColorForRSSI(m.link.RSSI)
	rssiStr := lipgloss.NewStyle().Bold(true).Foreground(rssiCol).Render(fmt.Sprintf("%d dBm", m.link.RSSI))
	if m.link.IsEthernet {
		rssiStr = lipgloss.NewStyle().Bold(true).Foreground(ColEmerald).Render("Wired Ethernet")
	}

	barFrac := (float64(m.link.RSSI) + 90.0) / 55.0
	if m.link.IsEthernet {
		barFrac = 1.0
	}
	meter := RenderGradientBar(barFrac, max(inner-20, 14))

	meta := fmt.Sprintf("SNR %s  ·  Noise %s  ·  PHY %s",
		lipgloss.NewStyle().Bold(true).Foreground(ColTeal).Render(fmt.Sprintf("%d dB", m.link.SNR)),
		StyleValue.Render(fmt.Sprintf("%d dBm", m.link.Noise)),
		lipgloss.NewStyle().Bold(true).Foreground(ColCyan).Render(fmt.Sprintf("%.0f Mbps", m.link.TxRateMbps)),
	)
	radio := fmt.Sprintf("%s  ·  %s  ·  Errs %d/%d",
		StyleValue.Render(m.link.PHYMode),
		StyleSubtitle.Render(m.link.Security),
		m.link.InErrors, m.link.OutErrors,
	)

	spark := RenderSparkline(m.rssiHist, max(inner-12, 16), -85, -30, true, func(v float64) lipgloss.Color {
		return ColorForRSSI(int(v))
	})

	breakdown := StyleMuted.Render(truncate(fmt.Sprintf("RF %d/35 · Lat %d/35 · Spd %d/30 · %s",
		m.score.SignalScore, m.score.LatScore, m.score.SpeedScore, m.score.Summary), inner))

	body := lipgloss.JoinVertical(lipgloss.Left,
		header,
		fmt.Sprintf("%s  %s %s", scoreBig, rssiStr, meter),
		meta,
		radio,
		StyleMuted.Render("RSSI trend ") + spark,
		breakdown,
	)
	return StyleCardHi.Width(w - 2).Height(6).Render(body)
}

func (m *Model) renderSpeedCard(w int) string {
	inner := w - 4
	right := "[b] burst · [c] cont"
	if m.speedRunning {
		right = m.spinner.View() + " " + m.speedProgress.Phase
	} else if m.speed.Source != "" {
		right = m.speed.Source + " · " + m.speed.Timestamp.Format("15:04:05")
	}
	header := CardHeader("⚡", "THROUGHPUT & BLOAT", right, inner)

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
	dlRow := fmt.Sprintf("%-7s %s %s",
		lipgloss.NewStyle().Foreground(ColCyan).Bold(true).Render("↓ DOWN"),
		RenderBar(math.Min(dl/800.0, 1.0), barW, ColCyan),
		StyleValue.Render(fmt.Sprintf("%10s", FormatMbps(dl))),
	)
	ulRow := fmt.Sprintf("%-7s %s %s",
		lipgloss.NewStyle().Foreground(ColViolet).Bold(true).Render("↑ UP"),
		RenderBar(math.Min(ul/150.0, 1.0), barW, ColViolet),
		StyleValue.Render(fmt.Sprintf("%10s", FormatMbps(ul))),
	)

	liveRow := StyleMuted.Render(truncate(fmt.Sprintf("Live %s: ↓ %s  ↑ %s",
		m.link.Interface, FormatMbps(m.link.LiveRxMbps), FormatMbps(m.link.LiveTxMbps)), inner))

	bloatText := StyleMuted.Render(truncate("Bufferbloat: press [b] burst or [n] netQuality", inner))
	if m.speed.LoadedRTTMs > 0 || m.speed.RPM > 0 {
		rpmCol := ColEmerald
		if m.speed.RPM < 400 {
			rpmCol = ColCoral
		} else if m.speed.RPM < 800 {
			rpmCol = ColAmber
		}
		bloatText = fmt.Sprintf("Loaded: %s (%s, bloat %s)",
			lipgloss.NewStyle().Bold(true).Foreground(rpmCol).Render(fmt.Sprintf("%d RPM", m.speed.RPM)),
			StyleValue.Render(fmt.Sprintf("%.0f ms", m.speed.LoadedRTTMs)),
			lipgloss.NewStyle().Foreground(rpmCol).Render(fmt.Sprintf("+%.0f ms", m.speed.BloatDeltaMs)),
		)
	}

	spark := RenderSparkline(m.downHist, max(inner-12, 16), 0, 0, false, func(float64) lipgloss.Color { return ColCyan })

	body := lipgloss.JoinVertical(lipgloss.Left,
		header,
		dlRow,
		ulRow,
		bloatText,
		StyleMuted.Render("Down trend ") + spark,
		liveRow,
	)
	return StyleCard.Width(w - 2).Height(6).Render(body)
}

func (m *Model) renderLatencyCard(title string, st probe.PingStats, goodMax, warnMax float64, w int) string {
	inner := w - 4
	right := st.Target
	if right == "" {
		right = "detecting..."
	}
	header := CardHeader("∿", strings.ToUpper(title), right, inner)

	spark := RenderSparkline(st.History, max(inner-2, 16), 0, 0, false, func(v float64) lipgloss.Color {
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

	body := lipgloss.JoinVertical(lipgloss.Left, header, spark, stats1, StyleMuted.Render(stats2))
	return StyleCard.Width(w - 2).Render(body)
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

	// Card 1: RF Channel Congestion
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

	// Card 2: DNS Resolver Race
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
	keys := "[1-4] Views  [Space] Pin  [b] Burst  [c] Cont  [n] netQuality  [s] Scan  [r] DNS  [m] Modem  [e] Export  [q] Quit"
	if m.toast != "" {
		return StyleFooter.Width(w).Render(StyleToast.Render("● "+m.toast) + "\n" + keys)
	}
	return StyleFooter.Width(w).Render(keys)
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
