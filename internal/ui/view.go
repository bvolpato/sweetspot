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
	if w < 80 {
		w = 80
	}
	h := m.height
	if h < 26 {
		h = 26
	}

	header := m.renderHeader(w)
	footer := m.renderFooter(w)
	bodyH := h - lipgloss.Height(header) - lipgloss.Height(footer)
	if bodyH < 18 {
		bodyH = 18
	}

	var body string
	if m.pinning {
		body = m.renderPinModal(w, bodyH)
	} else {
		switch m.tab {
		case TabRadar:
			body = m.renderRadarView(w, bodyH)
		case TabSpots:
			body = m.renderSpotsView(w, bodyH)
		case TabSpectrum:
			body = m.renderSpectrumView(w, bodyH)
		case TabAdvisor:
			body = m.renderAdvisorView(w, bodyH)
		}
	}

	// Pin footer cleanly at the bottom of the terminal on every tab.
	body = lipgloss.Place(w, bodyH, lipgloss.Left, lipgloss.Top, body)
	return lipgloss.JoinVertical(lipgloss.Left, header, body, footer)
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

func (m *Model) renderRadarView(w, bodyH int) string {
	twoCol := w >= 104
	cardW := w - 2
	leftW := cardW
	rightW := cardW
	if twoCol {
		leftW = (cardW - 1) / 2
		rightW = cardW - 1 - leftW
	}

	// Budget rows cleanly across: top row + latency row + quick advisor (5 rows).
	// Each card adds 2 border rows.
	advisorH := 5
	rem := bodyH - advisorH
	if !twoCol {
		rem = 24
	}
	// Split remaining height ~54% top row, ~46% latency row.
	topTotalH := max((rem*54)/100, 12)
	latTotalH := max(rem-topTotalH, 9)

	topCardInnerH := topTotalH - 2
	latCardInnerH := latTotalH - 2

	topChartH := max(topCardInnerH-7, 3)
	latChartH := max(latCardInnerH-3, 3)

	signalCard := m.renderSignalCard(leftW, topCardInnerH, topChartH)
	speedCard := m.renderSpeedCard(rightW, topCardInnerH, topChartH)
	gwCard := m.renderLatencyCard("Router Hop", m.gwStats, 10, 35, leftW, latCardInnerH, latChartH)
	wanCard := m.renderLatencyCard("Internet ("+m.opts.WANTarget+")", m.wanStats, 25, 70, rightW, latCardInnerH, latChartH)
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
	rightHeader := m.link.QualityLabel()
	if m.link.Scheduler != "" {
		q := m.link.Scheduler
		if m.link.QoSMode != "" {
			q += "/" + m.link.QoSMode
		}
		rightHeader = q + " · " + rightHeader
	}
	header := CardHeader("◉", "SIGNAL RADAR & SWEETSPOT", rightHeader, inner)

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

	sep := lipgloss.NewStyle().Foreground(ColBorder).Render(" │ \n │ \n │ ")
	heroParts := []string{
		lipgloss.JoinHorizontal(lipgloss.Bottom, tower, " ", bigRSSI, " ", unitCol),
		sep,
		lipgloss.JoinHorizontal(lipgloss.Bottom, bigScore, " ", scoreMeta),
	}

	// Fill right side of wide hero row with live CCA Airtime or Effective Link Tile!
	if inner >= 66 {
		var thirdTile string
		if m.link.HasCCA {
			ccaCol := ColEmerald
			ccaLbl := "CLEAN"
			if m.link.CCAPct >= 65 {
				ccaCol = ColCoral
				ccaLbl = "BUSY"
			} else if m.link.CCAPct >= 45 {
				ccaCol = ColAmber
				ccaLbl = "WARM"
			}
			thirdTile = lipgloss.JoinVertical(lipgloss.Left,
				lipgloss.NewStyle().Bold(true).Foreground(ccaCol).Render(fmt.Sprintf("CCA %d%% ", m.link.CCAPct))+Badge(ccaLbl, ccaCol),
				RenderBar(float64(m.link.CCAPct)/100.0, 16, ccaCol),
				StyleMuted.Render("Channel airtime"),
			)
		} else if m.link.EffDownMbps > 0 {
			thirdTile = lipgloss.JoinVertical(lipgloss.Left,
				lipgloss.NewStyle().Bold(true).Foreground(ColTeal).Render(fmt.Sprintf("Eff %.0f Mbps", m.link.EffDownMbps)),
				RenderBar(math.Min(m.link.EffDownMbps/500.0, 1.0), 16, ColTeal),
				StyleMuted.Render(fmt.Sprintf("LQM %d/100", m.link.LinkQuality)),
			)
		}
		if thirdTile != "" {
			heroParts = append(heroParts, sep, thirdTile)
		}
	}
	hero := lipgloss.JoinHorizontal(lipgloss.Bottom, heroParts...)

	barFrac := (float64(m.link.RSSI) + 90.0) / 55.0
	if m.link.IsEthernet {
		barFrac = 1.0
	}
	meter := RenderGradientBar(barFrac, max(inner-1, 16))

	// Compact single-line radio strip (never wraps!).
	parts := []string{
		fmt.Sprintf("PHY %s", lipgloss.NewStyle().Bold(true).Foreground(ColCyan).Render(fmt.Sprintf("%.0fM", m.link.TxRateMbps))),
		StyleValue.Render(compactPHY(m.link.PHYMode)),
	}
	if m.link.EffDownMbps > 0 {
		parts = append(parts, StyleSubtitle.Render(fmt.Sprintf("Eff %.0fM", m.link.EffDownMbps)))
	}
	if m.link.LinkQuality > 0 {
		parts = append(parts, StyleSubtitle.Render(fmt.Sprintf("LQM %d", m.link.LinkQuality)))
	}
	if m.link.HasMCS {
		mcs := fmt.Sprintf("MCS %d", m.link.MCS)
		if m.link.NSS > 0 {
			mcs += fmt.Sprintf(" %dx%d", m.link.NSS, m.link.NSS)
		}
		parts = append(parts, StyleSubtitle.Render(mcs))
	}
	if m.link.BSSID != "" {
		b := m.link.BSSID
		if m.roamCount > 0 {
			b += fmt.Sprintf(" (%dr)", m.roamCount)
		}
		parts = append(parts, StyleMuted.Render(b))
	} else {
		parts = append(parts, StyleMuted.Render(m.link.Security))
	}
	radio := strings.Join(parts, " · ")

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
	header := CardHeader("✦", "LIVE ADVISOR", "Press [4] for full advisor & playbook · [Space] to pin spot", inner)
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

// Tab 2: Full-height Spots Leaderboard + Live Current vs. Selected Spot Comparison
func (m *Model) renderSpotsView(w, bodyH int) string {
	cardW := w - 2
	inner := cardW - 4
	ranked := m.spotsStore.Ranked()

	topH := max((bodyH*56)/100-2, 9)
	botH := max(bodyH-topH-4, 8)

	header := CardHeader("★", "SPOT FINDER LEADERBOARD", "[Space] pin here · [j/k] select · [d] delete · [e] export", inner)
	var rows []string
	rows = append(rows, header, "")

	if len(ranked) == 0 {
		rows = append(rows,
			StyleValue.Render("No spots pinned yet -- walk to a desk or room and press [Space] to pin it."),
			"",
			StyleSubtitle.Render("Tip: run [b] for an 8-second speed burst at each spot before pinning so spots rank by RF, jitter, throughput, and bufferbloat."),
		)
	} else {
		rows = append(rows, StyleMuted.Render(fmt.Sprintf("%-5s %-20s %-18s %-26s %-15s %-12s %-15s %-7s",
			"RANK", "SPOT", "SCORE", "CHANNEL", "RSSI / SNR", "GW / WAN", "DOWN / UP", "RPM")))

		maxRows := max(topH-4, 3)
		for i, sp := range ranked {
			if i >= maxRows {
				break
			}
			rankLabel := fmt.Sprintf("#%d", i+1)
			if i == 0 {
				rankLabel = "★#1"
			}
			spd := "     -     "
			if sp.DownloadMbps > 0 {
				spd = fmt.Sprintf("%5.0f / %4.0fM", sp.DownloadMbps, sp.UploadMbps)
			}
			rpm := "-"
			if sp.LoadedRPM > 0 {
				rpm = fmt.Sprintf("%d", sp.LoadedRPM)
			}
			scoreBar := RenderBar(float64(sp.Score.Total)/100.0, 8, ColorForScore(sp.Score.Total))
			line := fmt.Sprintf("%-5s %-20s %3d (%-2s) %s  %-26s %3d / %2d dB     %4.1f / %4.1f  %-15s %-7s",
				rankLabel,
				truncate(sp.Name, 20),
				sp.Score.Total, sp.Score.Grade, scoreBar,
				truncate(sp.ChannelLabel, 26),
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
	}
	topCard := StyleCardHi.Width(cardW - 2).Height(topH).Render(lipgloss.JoinVertical(lipgloss.Left, rows...))

	// Bottom split: Selected Spot Inspector (left) vs. Live Here Now (right)
	leftW := (cardW - 1) / 2
	rightW := cardW - 1 - leftW

	var selLines []string
	selInner := leftW - 4
	if len(ranked) > 0 && m.selectedIdx < len(ranked) {
		sp := ranked[m.selectedIdx]
		best := ranked[0]
		selLines = append(selLines,
			CardHeader("◎", "SELECTED SPOT: "+strings.ToUpper(sp.Name), sp.Timestamp.Format("15:04:05"), selInner),
			"",
			fmt.Sprintf("Score %s  ·  %s  ·  PHY %.0f Mbps",
				Badge(fmt.Sprintf("%d/100 · %s", sp.Score.Total, sp.Score.Grade), ColorForScore(sp.Score.Total)),
				StyleValue.Render(sp.ChannelLabel), sp.TxRateMbps),
			fmt.Sprintf("RF: %d dBm / %d dB SNR (Noise %d dBm)  ·  Loss %.1f%%", sp.RSSI, sp.SNR, sp.Noise, sp.LossPct),
			fmt.Sprintf("Latency: Router %.1f ms (jitter %.1f ms)  ·  WAN %.1f ms", sp.GatewayAvgMs, sp.GatewayJitMs, sp.WANAvgMs),
		)
		if sp.DownloadMbps > 0 {
			selLines = append(selLines, fmt.Sprintf("Speed: ↓ %.1f Mbps  ↑ %.1f Mbps  ·  Loaded %d RPM (+%.0f ms)",
				sp.DownloadMbps, sp.UploadMbps, sp.LoadedRPM, sp.BloatDeltaMs))
		} else {
			selLines = append(selLines, StyleMuted.Render("Speed: not measured at pin time (run [b] before pinning)"))
		}
		if m.selectedIdx == 0 {
			selLines = append(selLines, "", lipgloss.NewStyle().Foreground(ColEmerald).Bold(true).Render("★ Currently your #1 ranked spot!"))
		} else {
			selLines = append(selLines, "", StyleSubtitle.Render(fmt.Sprintf("Vs #1 (%s): %+d score, %+d dB SNR, %+.1f ms router RTT",
				best.Name, sp.Score.Total-best.Score.Total, sp.SNR-best.SNR, sp.GatewayAvgMs-best.GatewayAvgMs)))
		}
	} else {
		selLines = append(selLines,
			CardHeader("◎", "HOW TO FIND YOUR SWEET SPOT", "", selInner),
			"",
			StyleSubtitle.Render("1. Stand at spot #1 (e.g. your desk) and press [b] then [Space]."),
			StyleSubtitle.Render("2. Walk to spot #2 (window, couch, another room) and press [Space]."),
			StyleSubtitle.Render("3. Compare RSSI, SNR, router jitter, and throughput side by side."),
			StyleSubtitle.Render("4. Press [e] to export a Markdown leaderboard + advice report."),
		)
	}
	leftCard := StyleCard.Width(leftW - 2).Height(botH).Render(lipgloss.JoinVertical(lipgloss.Left, selLines...))

	nowInner := rightW - 4
	tower := RenderSignalTower(m.link.RSSI, m.link.Connected, m.link.IsEthernet)
	nowCol := ColorForScore(m.score.Total)
	nowSummary := lipgloss.JoinVertical(lipgloss.Left,
		fmt.Sprintf("%s  %s", Badge(fmt.Sprintf("%d/100 · %s", m.score.Total, m.score.Grade), nowCol), StyleValue.Render(m.link.ChannelLabel())),
		fmt.Sprintf("RF: %d dBm / %d dB SNR · PHY %.0f Mbps", m.link.RSSI, m.link.SNR, m.link.TxRateMbps),
		fmt.Sprintf("Router %.1f ms (jit %.1f) · WAN %.1f ms", m.gwStats.AvgMs, m.gwStats.JitterMs, m.wanStats.AvgMs),
		StyleMuted.Render("Press [Space] to pin your current location"),
	)
	diffLine := StyleMuted.Render("Pin at least one spot to see live delta vs. your best spot.")
	if len(ranked) > 0 {
		best := ranked[0]
		delta := m.score.Total - best.Score.Total
		dCol := ColAmber
		if delta > 0 {
			dCol = ColEmerald
		} else if delta < -5 {
			dCol = ColCoral
		}
		diffLine = lipgloss.NewStyle().Bold(true).Foreground(dCol).Render(
			fmt.Sprintf("Live vs #1 (%s): %+d score, %+d dB SNR", best.Name, delta, m.link.SNR-best.SNR))
	}
	rightCard := StyleCard.Width(rightW - 2).Height(botH).Render(lipgloss.JoinVertical(lipgloss.Left,
		CardHeader("●", "LIVE HERE NOW", "Walk-around preview", nowInner),
		"",
		lipgloss.JoinHorizontal(lipgloss.Bottom, tower, "  ", nowSummary),
		"",
		diffLine,
	))

	botRow := lipgloss.JoinHorizontal(lipgloss.Top, leftCard, " ", rightCard)
	return lipgloss.JoinVertical(lipgloss.Left, topCard, botRow)
}

// Tab 3: Full-height RF Channel Spectrum + Single-Line DNS Resolver Race
func (m *Model) renderSpectrumView(w, bodyH int) string {
	cardW := w - 2
	inner := cardW - 4

	dnsH := 10
	rfH := max(bodyH-dnsH-4, 12)

	rfRight := "Press [s] to scan nearby channels"
	if m.scanning {
		rfRight = m.spinner.View() + " scanning..."
	} else if m.scan.TotalAPs > 0 {
		rfRight = fmt.Sprintf("%d APs · Best 5 GHz: Ch %d (non-DFS Ch %d) · Best 2.4 GHz: Ch %d",
			m.scan.TotalAPs, m.scan.Best5GHzChannel, m.scan.Best5GHzNonDFS, m.scan.Best2GHzChannel)
	}
	rfHeader := CardHeader("▤", "RF CHANNEL CONGESTION", rfRight, inner)

	var rfLines []string
	rfLines = append(rfLines, rfHeader, "")
	if len(m.scan.Buckets) == 0 {
		rfLines = append(rfLines,
			fmt.Sprintf("%s  %s", Badge("PRESS [s]", ColCyan), StyleValue.Render("Run an on-demand RF neighborhood scan")),
			"",
			StyleSubtitle.Render("Scanning is on-demand so idle latency monitoring never suffers off-channel ping spikes."),
			"",
			StyleMuted.Render("Channel guide:"),
			StyleSubtitle.Render("  • 2.4 GHz:  Only Channels 1, 6, and 11 are non-overlapping (keep 20 MHz in apartments)."),
			StyleSubtitle.Render("  • 5 GHz:    80 MHz blocks are UNII-1 (36-48), DFS (52-64, 100-144), and UNII-3 (149-161)."),
			StyleSubtitle.Render("  • 6 GHz:    Cleanest wide 80/160 MHz channels (requires Wi-Fi 6E/7 router + WPA3)."),
		)
	} else {
		maxCount := 1
		for _, b := range m.scan.Buckets {
			if b.Count > maxCount {
				maxCount = b.Count
			}
		}
		barW := max(inner-60, 12)
		maxRows := max(rfH-4, 6)
		for i, b := range m.scan.Buckets {
			if i >= maxRows {
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
			sigStr := "           "
			if b.MaxRSSI != 0 {
				sigStr = fmt.Sprintf("max %3d dBm", b.MaxRSSI)
			}
			rfLines = append(rfLines, fmt.Sprintf("%-7s Ch %-3d (%3dMHz) %s %2d APs  %s%s",
				b.Band, b.Channel, b.MaxWidthMHz,
				RenderBar(float64(b.Count)/float64(maxCount), barW, col),
				b.Count, StyleMuted.Render(sigStr), tag,
			))
		}
	}
	rfCard := StyleCardHi.Width(cardW - 2).Height(rfH).Render(lipgloss.JoinVertical(lipgloss.Left, rfLines...))

	// DNS Resolver Race -- strictly single-line rows!
	dnsRight := "Press [r] to re-race"
	if m.dnsRunning {
		dnsRight = m.spinner.View() + " racing..."
	} else if len(m.link.DNSServers) > 0 {
		dnsRight = "Configured: " + strings.Join(m.link.DNSServers, ", ")
	}
	dnsHeader := CardHeader("⌁", "DNS RESOLVER RACE", dnsRight, inner)

	var dnsLines []string
	dnsLines = append(dnsLines, dnsHeader, "")
	if len(m.dns.Entries) == 0 {
		dnsLines = append(dnsLines, StyleSubtitle.Render("Racing DNS resolvers..."))
	} else {
		maxMs := 55.0
		for _, e := range m.dns.Entries {
			if e.UncachedMs > maxMs {
				maxMs = e.UncachedMs
			}
		}
		// Reserve fixed space: prefix (27) + metrics (34) + FASTEST badge (10) = 71 chars
		barW := max(inner-72, 10)
		for _, e := range m.dns.Entries {
			if e.Failed {
				dnsLines = append(dnsLines, fmt.Sprintf("%-11s (%-11s)  %s", e.Name, e.Server, StyleMuted.Render("timeout")))
				continue
			}
			col := ColorForLatency(e.UncachedMs, 25, 45)
			tag := ""
			if !e.IsSystem && e.Server == m.dns.Fastest.Server {
				tag = " " + Badge("FASTEST", ColEmerald)
			}
			dnsLines = append(dnsLines, fmt.Sprintf("%-11s (%-11s) %s  uncached %5.1fms · cached %5.1fms%s",
				e.Name, e.Server,
				RenderBar(e.UncachedMs/maxMs, barW, col),
				e.UncachedMs, e.CachedMs, tag,
			))
		}
		if !m.dns.System.Failed && !m.dns.Fastest.Failed && m.dns.System.UncachedMs-m.dns.Fastest.UncachedMs >= 10 {
			dnsLines = append(dnsLines, "", StyleMuted.Render(fmt.Sprintf(
				"Note: %s (%s) is %.1f ms faster uncached than System DNS (corporate DNS proxies like Scout add ~15-20 ms on managed laptops).",
				m.dns.Fastest.Name, m.dns.Fastest.Server, m.dns.System.UncachedMs-m.dns.Fastest.UncachedMs)))
		}
	}
	dnsCard := StyleCard.Width(cardW - 2).Height(dnsH).Render(lipgloss.JoinVertical(lipgloss.Left, dnsLines...))

	return lipgloss.JoinVertical(lipgloss.Left, rfCard, dnsCard)
}

// Tab 4: Full-height 2-column Active Advisor + Complete Optimization Playbook & Modem Doctor
func (m *Model) renderAdvisorView(w, bodyH int) string {
	cardW := w - 2
	twoCol := w >= 112
	leftW := cardW
	rightW := cardW
	if twoCol {
		leftW = (cardW * 62) / 100
		rightW = cardW - 1 - leftW
	}
	cardH := max(bodyH-2, 14)

	// Left: Active prioritized findings (scrollable with j/k)
	leftInner := leftW - 4
	scrollHint := "[j/k] scroll · [m] paste cable modem stats"
	if len(m.advice) > 0 && m.advisorScroll > 0 {
		scrollHint = fmt.Sprintf("showing from #%d/%d · [j/k] scroll", m.advisorScroll+1, len(m.advice))
	}
	leftHeader := CardHeader("✦", fmt.Sprintf("ACTIVE FINDINGS (%d)", len(m.advice)), scrollHint, leftInner)

	var leftLines []string
	leftLines = append(leftLines, leftHeader, "")
	usedRows := 2
	for i := m.advisorScroll; i < len(m.advice); i++ {
		if usedRows+4 > cardH {
			leftLines = append(leftLines, StyleMuted.Render(fmt.Sprintf("↓ %d more finding(s) -- press [j] to scroll down", len(m.advice)-i)))
			break
		}
		a := m.advice[i]
		leftLines = append(leftLines,
			fmt.Sprintf("%s %s %s", severityBadge(a.Severity), StyleMuted.Render("["+a.Category+"]"), StyleValue.Render(a.Title)),
			"   "+StyleSubtitle.Render(truncate(a.Detail, leftInner-4)),
			"   "+lipgloss.NewStyle().Foreground(ColTeal).Render("→ "+truncate(a.Action, leftInner-6)),
			"",
		)
		usedRows += 4
	}
	if len(m.advice) == 0 {
		leftLines = append(leftLines, StyleSubtitle.Render("Collecting live measurements..."))
	}
	leftCard := StyleCardHi.Width(leftW - 2).Height(cardH).Render(lipgloss.JoinVertical(lipgloss.Left, leftLines...))

	if !twoCol {
		return leftCard
	}

	// Right: Vetted Optimization Playbook + Cable Modem Doctor
	rightInner := rightW - 4
	rightHeader := CardHeader("✓", "OPTIMIZATION PLAYBOOK", "Vetted checklist", rightInner)
	var rightLines []string
	rightLines = append(rightLines, rightHeader, "")
	for _, item := range m.playbook {
		var icon string
		switch item.Status {
		case "ok":
			icon = lipgloss.NewStyle().Bold(true).Foreground(ColEmerald).Render("✓")
		case "warn":
			icon = lipgloss.NewStyle().Bold(true).Foreground(ColAmber).Render("▲")
		default:
			icon = lipgloss.NewStyle().Foreground(ColCyan).Render("○")
		}
		rightLines = append(rightLines,
			fmt.Sprintf("%s %s", icon, StyleValue.Render(truncate(item.Title, rightInner-4))),
			"  "+StyleMuted.Render(truncate(item.Note, rightInner-4)),
		)
	}

	rightLines = append(rightLines, "", lipgloss.NewStyle().Foreground(ColBorder).Render(strings.Repeat("─", max(rightInner, 10))))
	if m.modem.Parsed {
		rightLines = append(rightLines,
			lipgloss.NewStyle().Bold(true).Foreground(ColCyan).Render("DOCSIS Cable Modem ("+m.modem.Health+")"),
			StyleSubtitle.Render(fmt.Sprintf("Down: %.1f..%.1f dBmV (%d QAM, %d OFDM)", m.modem.DownPowerMin, m.modem.DownPowerMax, m.modem.DownstreamCount, m.modem.OFDMCount)),
			StyleSubtitle.Render(fmt.Sprintf("SNR: %.1f..%.1f dB · Up: %.1f dBmV (%d ch)", m.modem.DownSNRMin, m.modem.DownSNRMax, m.modem.UpPowerMax, m.modem.UpstreamCount)),
			StyleMuted.Render(fmt.Sprintf("Uncorrectables: %d · Uptime: %s", m.modem.TotalUncorrectables, m.modem.Uptime)),
		)
	} else {
		rightLines = append(rightLines,
			lipgloss.NewStyle().Bold(true).Foreground(ColCyan).Render("Modem Doctor (DOCSIS 3.0/3.1)"),
			StyleMuted.Render("Copy your router's Cable Connection table"),
			StyleMuted.Render("(NETGEAR / ARRIS / Motorola) & press [m]."),
		)
	}
	rightCard := StyleCard.Width(rightW - 2).Height(cardH).Render(lipgloss.JoinVertical(lipgloss.Left, rightLines...))

	return lipgloss.JoinHorizontal(lipgloss.Top, leftCard, " ", rightCard)
}

func (m *Model) renderPinModal(w, bodyH int) string {
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
	return lipgloss.Place(w, bodyH, lipgloss.Center, lipgloss.Center, card)
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

func compactPHY(phy string) string {
	switch {
	case strings.Contains(phy, "Wi-Fi 7"):
		return "Wi-Fi 7 (be)"
	case strings.Contains(phy, "Wi-Fi 6"):
		return "Wi-Fi 6 (ax)"
	case strings.Contains(phy, "Wi-Fi 5"):
		return "Wi-Fi 5 (ac)"
	case strings.Contains(phy, "Wi-Fi 4"):
		return "Wi-Fi 4 (n)"
	default:
		return phy
	}
}

func truncate(s string, maxLen int) string {
	if maxLen <= 3 || len(s) <= maxLen {
		return s
	}
	return s[:maxLen-1] + "…"
}
