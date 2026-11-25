package ui

import (
	"fmt"
	"math"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/bvolpato/sweetspot/internal/advisor"
	"github.com/bvolpato/sweetspot/internal/probe"
	"github.com/bvolpato/sweetspot/internal/spots"
	"github.com/bvolpato/sweetspot/internal/wifi"
)

// Layout breakpoints, in terminal cells.
const (
	compactW    = 44  // narrower bodies drop the cards and use plain lines
	compactH    = 12  // shorter radar bodies do the same
	listH       = 10  // the same limit for the table views
	minTwoColW  = 72  // two columns are possible from here
	wideTwoColW = 104 // two columns are always used from here
	advisorColW = 112 // the playbook gets its own column from here
	stackedH    = 36  // four stacked radar cards need this many body rows
)

// View always returns exactly m.height rows that are at most m.width cells wide. A taller
// frame would make Bubble Tea drop rows from the top, which hides the header.
func (m *Model) View() string {
	w, h := max(m.width, 1), max(m.height, 1)

	header := m.renderHeader(w, h)
	footer := m.renderFooter(w, h)
	bodyH := h
	if header != "" {
		bodyH -= lipgloss.Height(header)
	}
	if footer != "" {
		bodyH -= lipgloss.Height(footer)
	}

	var body string
	switch {
	case m.pinning:
		body = m.renderPinModal(w, bodyH)
	case m.tab == TabSpots:
		body = m.renderSpotsView(w, bodyH)
	case m.tab == TabSpectrum:
		body = m.renderSpectrumView(w, bodyH)
	case m.tab == TabAdvisor:
		body = m.renderAdvisorView(w, bodyH)
	default:
		body = m.renderRadarView(w, bodyH)
	}

	// clip pads the body too, so the footer stays at the bottom on every tab.
	return vstackRows(header, clip(body, w, bodyH), footer)
}

// vstackRows joins header, body, and footer. The body may hold blank rows, so it is kept as is.
func vstackRows(header, body, footer string) string {
	parts := []string{body}
	if header != "" {
		parts = append([]string{header}, parts...)
	}
	if footer != "" {
		parts = append(parts, footer)
	}
	return strings.Join(parts, "\n")
}

func (m *Model) renderHeader(w, h int) string {
	if h < 4 {
		return ""
	}

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

	scoreLabel := fmt.Sprintf("%d/100 · %s", m.score.Total, m.score.Grade)
	if m.speed.DownloadMbps == 0 && !m.speedRunning {
		scoreLabel = "~" + scoreLabel
	}
	scorePill := Badge(scoreLabel, ColorForScore(m.score.Total))

	tabs := m.renderTabs(w - 1)
	if h < compactH {
		row := " " + tabs
		if lipgloss.Width(row)+lipgloss.Width(scorePill)+1 <= w {
			row = pad(row, w-lipgloss.Width(scorePill)) + scorePill
		}
		return fit(row, w)
	}

	logo := lipgloss.NewStyle().Bold(true).Foreground(ColCyan).Render("◈ sweetspot")
	badge := lipgloss.NewStyle().Foreground(ColText).Background(ColTrack).Padding(0, 1)
	lefts := []string{
		logo + "  " + badge.Render(m.link.ChannelLabel()),
		logo + "  " + badge.Render(m.shortChannel()),
		logo,
	}
	if m.link.SSID != "" {
		lefts = append([]string{lefts[0] + " " + StyleMuted.Render(m.link.SSID)}, lefts...)
	}
	rights := []string{statusPill + " " + scorePill, scorePill}
	if m.link.Privileged {
		rights = append([]string{Badge("SUDO+", ColViolet) + " " + rights[0]}, rights...)
	}

	row1 := " " + logo
	for _, r := range rights {
		found := false
		for _, l := range lefts {
			if gap := w - 2 - lipgloss.Width(l) - lipgloss.Width(r); gap >= 1 {
				row1 = " " + l + strings.Repeat(" ", gap) + r
				found = true
				break
			}
		}
		if found {
			break
		}
	}

	rows := []string{fit(row1, w), fit(" "+tabs, w)}
	if h >= 22 && w > 2 {
		rows = append(rows, " "+lipgloss.NewStyle().Foreground(ColBorder).Render(strings.Repeat("─", w-2)))
	}
	return strings.Join(rows, "\n")
}

// renderTabs picks the longest tab labels that fit.
func (m *Model) renderTabs(w int) string {
	tabs := []struct{ full, short, tiny string }{
		{"1 Radar", "1 Radar", "1"},
		{fmt.Sprintf("2 Spots (%d)", m.spotsStore.Len()), "2 Spots", "2"},
		{"3 RF & DNS", "3 RF", "3"},
		{fmt.Sprintf("4 Advisor (%d)", len(m.advice)), "4 Advice", "4"},
	}
	var full, short, tiny []string
	for i, t := range tabs {
		if m.tab == Tab(i) {
			full = append(full, StyleTabActive.Render(t.full))
			short = append(short, StyleTabActive.Render(t.short))
			tiny = append(tiny, StyleTabActive.Render(t.short))
			continue
		}
		full = append(full, StyleTabInactive.Render(t.full))
		short = append(short, StyleTabInactive.Render(t.short))
		tiny = append(tiny, StyleTabInactive.Render(t.tiny))
	}
	return firstFit(w, strings.Join(full, " "), strings.Join(short, " "), strings.Join(tiny, " "), strings.Join(tiny, ""))
}

func (m *Model) shortChannel() string {
	if m.link.IsEthernet || !m.link.Connected {
		return m.link.ChannelLabel()
	}
	if m.link.WidthMHz > 0 {
		return fmt.Sprintf("Ch %d · %d MHz", m.link.Channel, m.link.WidthMHz)
	}
	return fmt.Sprintf("Ch %d", m.link.Channel)
}

// renderRadarView picks a layout from both dimensions: two columns, stacked cards, or plain
// lines. Each card then keeps its most important rows for the size it gets.
func (m *Model) renderRadarView(w, h int) string {
	if w < compactW || h < compactH {
		return m.renderRadarCompact(w, h)
	}

	gw := func(w, h int) string {
		return m.renderLatencyCard("Router Hop", m.gwStats, 10, 35, w, h)
	}
	wan := func(w, h int) string {
		return m.renderLatencyCard("Internet ("+m.opts.WANTarget+")", m.wanStats, 25, 70, w, h)
	}

	if w >= wideTwoColW || (w >= minTwoColW && h < stackedH) {
		advH := 0
		switch {
		case h >= 28:
			advH = 5
		case h >= 20:
			advH = 3
		case h >= 15:
			advH = 1
		}
		rem := h - advH
		topH := max(rem*54/100, 6)
		latH := rem - topH
		if latH < 5 {
			latH = 5
			topH = rem - latH
		}
		leftW := (w - 1) / 2
		rightW := w - 1 - leftW
		return vstack(
			lipgloss.JoinHorizontal(lipgloss.Top, m.renderSignalCard(leftW, topH), " ", m.renderSpeedCard(rightW, topH)),
			lipgloss.JoinHorizontal(lipgloss.Top, gw(leftW, latH), " ", wan(rightW, latH)),
			m.renderQuickAdvisor(w, advH),
		)
	}

	if h >= stackedH {
		advH := 3
		if h >= 42 {
			advH = 5
		}
		rem := h - advH
		sigH := rem * 32 / 100
		latH := rem * 21 / 100
		spdH := rem - sigH - 2*latH
		return vstack(m.renderSignalCard(w, sigH), m.renderSpeedCard(w, spdH), gw(w, latH), wan(w, latH), m.renderQuickAdvisor(w, advH))
	}

	// Narrow and short: one latency card for both hops.
	advH := 0
	if h >= 18 {
		advH = 1
	}
	const latH = 4
	rem := h - latH - advH
	sigH := (rem + 1) / 2
	lat := cardBox(StyleCard, w, latH, m.latencyLines(w-4))
	return vstack(m.renderSignalCard(w, sigH), m.renderSpeedCard(w, rem-sigH), lat, m.renderQuickAdvisor(w, advH))
}

// renderRadarCompact shows the live numbers as plain lines when cards do not fit.
func (m *Model) renderRadarCompact(w, h int) string {
	lat := m.latencyLines(w)
	blocks := []block{
		text(0, m.signalLine(w)),
		text(5, RenderGradientBar(m.signalFraction(), w)),
		text(6, m.radioLine(w)),
		text(1, lat[0]),
		text(2, lat[1]),
		text(3, m.throughputLine(w)),
		text(4, m.liveLine(w)),
	}
	if len(m.advice) > 0 {
		blocks = append(blocks, text(7, m.adviceLine(m.advice[0], w)))
	}
	return strings.Join(stack(h, blocks...), "\n")
}

func (m *Model) signalFraction() float64 {
	if m.link.IsEthernet {
		return 1
	}
	if !m.link.Connected {
		return 0
	}
	return (float64(m.link.RSSI) + 90.0) / 55.0
}

// signalLine is the one-row form of the signal hero.
func (m *Model) signalLine(w int) string {
	score := Badge(fmt.Sprintf("%d/100 %s", m.score.Total, m.score.Grade), ColorForScore(m.score.Total))
	switch {
	case m.link.IsEthernet:
		return joinFit(w, lipgloss.NewStyle().Bold(true).Foreground(ColEmerald).Render("Wired link"), score)
	case !m.link.Connected:
		return joinFit(w, lipgloss.NewStyle().Bold(true).Foreground(ColCoral).Render("No Wi-Fi link"), score)
	}
	return joinFit(w,
		lipgloss.NewStyle().Bold(true).Foreground(ColorForRSSI(m.link.RSSI)).Render(fmt.Sprintf("%d dBm", m.link.RSSI)),
		score,
		StyleSubtitle.Render(fmt.Sprintf("SNR %d dB", m.link.SNR)),
		StyleMuted.Render(fmt.Sprintf("Noise %d", m.link.Noise)),
	)
}

// radioLine is the compact single-line radio strip.
func (m *Model) radioLine(w int) string {
	parts := []string{
		fmt.Sprintf("PHY %s", lipgloss.NewStyle().Bold(true).Foreground(ColCyan).Render(fmt.Sprintf("%.0fM", m.link.TxRateMbps))),
	}
	if phy := compactPHY(m.link.PHYMode); phy != "" {
		parts = append(parts, StyleValue.Render(phy))
	}
	if m.link.HasCCA {
		parts = append(parts, StyleSubtitle.Render(fmt.Sprintf("CCA %d%%", m.link.CCAPct)))
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
	} else if m.link.Security != "" {
		parts = append(parts, StyleMuted.Render(m.link.Security))
	}
	return joinFit(w, parts...)
}

// signalHero returns the widest big-digit hero that fits in w x h cells, or "" if none fits.
func (m *Model) signalHero(w, h int) string {
	if h < 3 {
		return ""
	}
	rssiCol := ColorForRSSI(m.link.RSSI)
	bigRSSI := RenderBigNumber("0", ColCoral)
	unitCol := lipgloss.JoinVertical(lipgloss.Left,
		lipgloss.NewStyle().Bold(true).Foreground(rssiCol).Render("dBm"),
		StyleSubtitle.Render(fmt.Sprintf("SNR %d dB", m.link.SNR)),
		StyleMuted.Render(fmt.Sprintf("Noise %d", m.link.Noise)),
	)
	if m.link.IsEthernet {
		bigRSSI = RenderBigNumber("100", ColEmerald)
		unitCol = lipgloss.JoinVertical(lipgloss.Left,
			lipgloss.NewStyle().Bold(true).Foreground(ColEmerald).Render("ETH"),
			StyleSubtitle.Render("Wired Link"),
			StyleMuted.Render("No RF loss"),
		)
	} else if m.link.Connected {
		bigRSSI = RenderBigNumber(fmt.Sprintf("%d", m.link.RSSI), rssiCol)
	}
	signal := lipgloss.JoinHorizontal(lipgloss.Bottom, bigRSSI, " ", unitCol)

	scoreCol := ColorForScore(m.score.Total)
	score := lipgloss.JoinHorizontal(lipgloss.Bottom,
		RenderBigNumber(fmt.Sprintf("%d", m.score.Total), scoreCol), " ",
		lipgloss.JoinVertical(lipgloss.Left,
			Badge(m.score.Grade, scoreCol),
			StyleSubtitle.Render(fmt.Sprintf("RF %d · Lat %d", m.score.SignalScore, m.score.LatScore)),
			StyleMuted.Render(fmt.Sprintf("Spd %d/30", m.score.SpeedScore)),
		),
	)
	smallScore := lipgloss.JoinVertical(lipgloss.Left,
		Badge(m.score.Grade, scoreCol),
		StyleValue.Render(fmt.Sprintf("%d/100", m.score.Total)),
		StyleMuted.Render("score"),
	)

	// Third tile: live CCA airtime, or the effective link rate.
	third := ""
	if m.link.HasCCA {
		ccaCol, ccaLbl := ColEmerald, "CLEAN"
		if m.link.CCAPct >= 65 {
			ccaCol, ccaLbl = ColCoral, "BUSY"
		} else if m.link.CCAPct >= 45 {
			ccaCol, ccaLbl = ColAmber, "WARM"
		}
		third = lipgloss.JoinVertical(lipgloss.Left,
			lipgloss.NewStyle().Bold(true).Foreground(ccaCol).Render(fmt.Sprintf("CCA %d%% ", m.link.CCAPct))+Badge(ccaLbl, ccaCol),
			RenderBar(float64(m.link.CCAPct)/100.0, 16, ccaCol),
			StyleMuted.Render("Channel airtime"),
		)
	} else if m.link.EffDownMbps > 0 {
		third = lipgloss.JoinVertical(lipgloss.Left,
			lipgloss.NewStyle().Bold(true).Foreground(ColTeal).Render(fmt.Sprintf("Eff %.0f Mbps", m.link.EffDownMbps)),
			RenderBar(math.Min(m.link.EffDownMbps/500.0, 1.0), 16, ColTeal),
			StyleMuted.Render(fmt.Sprintf("LQM %d/100", m.link.LinkQuality)),
		)
	}

	sep := lipgloss.NewStyle().Foreground(ColBorder).Render(" │ \n │ \n │ ")
	row := func(parts ...string) string { return lipgloss.JoinHorizontal(lipgloss.Bottom, parts...) }
	tower := row(RenderSignalTower(m.link.RSSI, m.link.Connected, m.link.IsEthernet), " ")

	var variants []string
	if third != "" {
		variants = append(variants, row(tower, signal, sep, score, sep, third))
	}
	variants = append(variants,
		row(tower, signal, sep, score),
		row(signal, sep, score),
		row(signal, sep, smallScore),
		signal,
	)
	for _, v := range variants {
		if lipgloss.Width(v) <= w && lipgloss.Height(v) <= h {
			return v
		}
	}
	return ""
}

func (m *Model) renderSignalCard(w, h int) string {
	inner, rows := w-4, h-2
	quality := m.link.QualityLabel()
	rights := []string{quality}
	if m.link.Scheduler != "" {
		q := m.link.Scheduler
		if m.link.QoSMode != "" {
			q += "/" + m.link.QoSMode
		}
		rights = []string{q + " · " + quality, quality}
	}

	// The big digits need room for the header, the meter, and a small chart too.
	hero := ""
	switch {
	case rows >= 9:
		hero = m.signalHero(inner, 4)
	case rows >= 7:
		hero = m.signalHero(inner, 3)
	}
	if hero == "" {
		hero = m.signalLine(inner)
	}

	return cardBox(StyleCardHi, w, h, stack(rows,
		text(1, CardHeader("◉", "SIGNAL RADAR & SWEETSPOT", inner, rights...)),
		text(0, hero),
		text(4, RenderGradientBar(m.signalFraction(), inner)),
		text(3, m.radioLine(inner)),
		flexible(2, 2, func(n int) string {
			return BrailleChart(m.rssiHist, inner, n, -85, -35, true, "dBm", func(v float64) lipgloss.Color {
				return ColorForRSSI(int(v))
			})
		}),
	))
}

// throughput returns the rates to show: the live progress during a test, else the last result.
func (m *Model) throughput() (down, up float64) {
	down, up = m.speed.DownloadMbps, m.speed.UploadMbps
	if m.speedRunning {
		if m.speedProgress.DownloadMbps > 0 {
			down = m.speedProgress.DownloadMbps
		}
		if m.speedProgress.UploadMbps > 0 {
			up = m.speedProgress.UploadMbps
		}
	}
	return down, up
}

func (m *Model) throughputLine(w int) string {
	down, up := m.throughput()
	if down == 0 && up == 0 && !m.speedRunning {
		return firstFit(w, StyleMuted.Render("Speed: press [b] to measure"), StyleMuted.Render("[b] speed test"))
	}
	parts := []string{
		lipgloss.NewStyle().Bold(true).Foreground(ColCyan).Render("↓ " + FormatMbps(down)),
		lipgloss.NewStyle().Bold(true).Foreground(ColViolet).Render("↑ " + FormatMbps(up)),
	}
	if m.speed.RPM > 0 {
		parts = append(parts, StyleValue.Render(fmt.Sprintf("%d RPM", m.speed.RPM)))
	}
	return joinFit(w, parts...)
}

func (m *Model) liveLine(w int) string {
	rx := lipgloss.NewStyle().Foreground(ColCyan).Render("↓ " + FormatMbps(m.link.LiveRxMbps))
	tx := lipgloss.NewStyle().Foreground(ColViolet).Render("↑ " + FormatMbps(m.link.LiveTxMbps))
	label := "Live: "
	if m.link.Interface != "" {
		label = "Live " + StyleMuted.Render(m.link.Interface) + ": "
	}
	return joinFit(w, label+rx, tx)
}

func (m *Model) bloatLine(w int) string {
	if m.speed.LoadedRTTMs <= 0 && m.speed.RPM <= 0 {
		return fit(StyleMuted.Render("Bufferbloat: sampling loaded latency..."), w)
	}
	col, grade := ColEmerald, "LOW BLOAT"
	if m.speed.RPM < 400 {
		col, grade = ColCoral, "HIGH BLOAT"
	} else if m.speed.RPM < 800 {
		col, grade = ColAmber, "MED BLOAT"
	}
	badge := Badge(grade, col)
	rpm := lipgloss.NewStyle().Bold(true).Foreground(col).Render(fmt.Sprintf("%d RPM", m.speed.RPM))
	loaded := StyleValue.Render(fmt.Sprintf("%.0f ms", m.speed.LoadedRTTMs))
	delta := lipgloss.NewStyle().Foreground(col).Render(fmt.Sprintf("+%.0f ms", m.speed.BloatDeltaMs))
	return firstFit(w,
		fmt.Sprintf("%s  Loaded %s  (%s · %s bloat)", badge, rpm, loaded, delta),
		fmt.Sprintf("%s %s (%s)", badge, rpm, delta),
		badge+" "+rpm,
		rpm,
	)
}

func (m *Model) renderSpeedCard(w, h int) string {
	inner, rows := w-4, h-2
	rights := []string{"[b] burst · [c] cont · [n] netQuality", "[b] burst · [c] cont", "[b] test"}
	if m.speedRunning {
		rights = []string{m.spinner.View() + " running " + m.speedProgress.Phase, m.spinner.View() + " running", m.spinner.View()}
	} else if m.speed.Source != "" {
		at := m.speed.Timestamp.Format("15:04:05")
		rights = []string{m.speed.Source + " · " + at, at}
	}
	header := CardHeader("⚡", "THROUGHPUT & BUFFERBLOAT", inner, rights...)

	const labelW, valueW = 8, 10
	row := func(label string, v, full float64, col lipgloss.Color) string {
		l := lipgloss.NewStyle().Foreground(col).Bold(true).Width(labelW).Render(label)
		value := StyleValue.Render(fmt.Sprintf("%*s", valueW, FormatMbps(v)))
		if barW := inner - labelW - valueW - 2; barW >= 4 {
			return l + " " + RenderBar(math.Min(v/full, 1.0), barW, col) + " " + value
		}
		return l + value
	}
	ask := func(label, alt string, col lipgloss.Color) string {
		l := lipgloss.NewStyle().Foreground(col).Bold(true).Width(labelW).Render(label) + " "
		call := lipgloss.NewStyle().Bold(true).Foreground(col)
		return l + firstFit(inner-labelW-1,
			call.Render("press [b] to measure")+StyleMuted.Render("  ("+alt+")"),
			call.Render("press [b] to measure"),
			call.Render("press [b]"),
		)
	}

	title := "Live RX Throughput (Mbps)"
	if m.speedRunning || m.speed.DownloadMbps > 0 {
		title = "Throughput History (Mbps)"
	}
	chart := func(n int) string {
		return BrailleChart(m.downHist, inner, n, 0, 0, false, "M", func(v float64) lipgloss.Color {
			switch {
			case v >= 250:
				return ColEmerald
			case v >= 60:
				return ColCyan
			}
			return ColViolet
		})
	}

	down, up := m.throughput()
	var lines []string
	if down == 0 && up == 0 && !m.speedRunning {
		note := Badge("NOT TESTED", ColTrackHi)
		lines = stack(rows,
			text(1, header),
			text(7, ""),
			text(2, ask("↓ DOWN", "or [c] continuous", ColCyan)),
			text(4, ask("↑ UP", "or [n] netQuality", ColViolet)),
			text(5, firstFit(inner,
				note+"  "+StyleSubtitle.Render("Live "+m.link.Interface+" traffic charted below (zero extra bandwidth)"),
				note+"  "+StyleSubtitle.Render("Live traffic charted below"),
				note+" "+StyleSubtitle.Render("live traffic"),
				note,
			)),
			text(0, m.liveLine(inner)),
			text(6, StyleMuted.Render(title)),
			flexible(3, 2, chart),
		)
	} else {
		// With one row the rates share a line.
		if rows < 3 {
			lines = stack(rows, text(1, header), text(0, m.throughputLine(inner)))
		} else {
			lines = stack(rows,
				text(0, header),
				text(7, ""),
				text(1, row("↓ DOWN", down, 800, ColCyan)),
				text(2, row("↑ UP", up, 150, ColViolet)),
				text(3, m.bloatLine(inner)),
				text(5, m.liveLine(inner)),
				text(6, StyleMuted.Render(title)),
				flexible(4, 2, chart),
			)
		}
	}
	return cardBox(StyleCard, w, h, lines)
}

func (m *Model) renderLatencyCard(title string, st probe.PingStats, goodMax, warnMax float64, w, h int) string {
	inner, rows := w-4, h-2
	right := st.Target
	if right == "" {
		right = "detecting..."
	}
	lossCol := ColEmerald
	if st.LossPct > 0 {
		lossCol = ColCoral
	}
	ms := func(v float64) string { return fmt.Sprintf("%.1fms", v) }
	colored := func(v float64) string {
		return lipgloss.NewStyle().Bold(true).Foreground(ColorForLatency(v, goodMax, warnMax)).Render(ms(v))
	}

	return cardBox(StyleCard, w, h, stack(rows,
		text(1, CardHeader("∿", strings.ToUpper(title), inner, right)),
		flexible(2, 2, func(n int) string {
			return BrailleChart(st.History, inner, n, 0, 0, false, "ms", func(v float64) lipgloss.Color {
				return ColorForLatency(v, goodMax, warnMax)
			})
		}),
		text(0, joinFit(inner,
			"Now "+colored(st.LastMs),
			"Avg "+StyleValue.Render(ms(st.AvgMs)),
			"P95 "+StyleValue.Render(ms(st.P95Ms)),
			"Max "+colored(st.MaxMs),
		)),
		text(3, joinFit(inner,
			StyleMuted.Render("Jitter ")+StyleValue.Render(ms(st.JitterMs)),
			StyleMuted.Render("Loss ")+lipgloss.NewStyle().Bold(true).Foreground(lossCol).Render(fmt.Sprintf("%.1f%%", st.LossPct)),
			StyleMuted.Render("Spikes ")+StyleValue.Render(fmt.Sprintf("%d", st.SpikeCount)),
			StyleMuted.Render(fmt.Sprintf("Samples %d", st.Sent)),
		)),
	))
}

// latencyLines is the one-row-per-hop form of the latency cards, with a sparkline.
func (m *Model) latencyLines(w int) []string {
	line := func(label string, st probe.PingStats, goodMax, warnMax float64) string {
		color := func(v float64) lipgloss.Color { return ColorForLatency(v, goodMax, warnMax) }
		lossCol := ColSubtle
		if st.LossPct > 0 {
			lossCol = ColCoral
		}
		head := StyleMuted.Render(fmt.Sprintf("%-8s ", label)) +
			lipgloss.NewStyle().Bold(true).Foreground(color(st.LastMs)).Render(fmt.Sprintf("%7s", fmt.Sprintf("%.1fms", st.LastMs)))
		avg := StyleSubtitle.Render(fmt.Sprintf(" avg %.1f", st.AvgMs))
		loss := lipgloss.NewStyle().Foreground(lossCol).Render(fmt.Sprintf(" loss %.0f%%", st.LossPct))
		for _, tail := range []string{avg + loss, loss} {
			if spark := w - lipgloss.Width(head) - lipgloss.Width(tail) - 1; spark >= 8 {
				return head + " " + RenderSparkline(st.History, spark, 0, 0, false, color) + tail
			}
		}
		return firstFit(w, head+avg+loss, head+loss, head)
	}
	return []string{
		line("Router", m.gwStats, 10, 35),
		line("Internet", m.wanStats, 25, 70),
	}
}

func (m *Model) adviceLine(a advisor.Advice, w int) string {
	s := severityBadge(a.Severity) + " " + StyleValue.Render(a.Title)
	if w-lipgloss.Width(s) >= 20 {
		s += " -- " + StyleSubtitle.Render(a.Action)
	}
	return fit(s, w)
}

// renderQuickAdvisor is the advisor strip: a card, or one plain line when h is 1.
func (m *Model) renderQuickAdvisor(w, h int) string {
	if h <= 0 {
		return ""
	}
	inner := w - 4
	if h < 3 {
		inner = w - 1
	}
	var lines []string
	if h >= 5 {
		lines = append(lines, CardHeader("✦", "LIVE ADVISOR", inner,
			"Press [4] for full advisor & playbook · [Space] to pin spot",
			"[4] full advisor · [Space] pin spot",
			"[4] full advisor",
		))
	}
	for i := 0; i < len(m.advice) && i < 2; i++ {
		lines = append(lines, m.adviceLine(m.advice[i], inner))
	}
	if len(m.advice) == 0 {
		lines = append(lines, fit(StyleMuted.Render("Collecting baseline telemetry..."), inner))
	}
	if h < 3 {
		return clip(" "+lines[0], w, h)
	}
	return cardBox(StyleCard, w, h, lines)
}

// column is one column of the spots table. Columns with a high prio are dropped first.
type column struct {
	title string
	width int
	prio  int
	cell  func(i int, sp spots.Spot) string
}

func spotColumns(w int) []column {
	scoreBar := func(_ int, sp spots.Spot) string {
		return fmt.Sprintf("%3d (%-2s) ", sp.Score.Total, sp.Score.Grade) +
			RenderBar(float64(sp.Score.Total)/100.0, 8, ColorForScore(sp.Score.Total))
	}
	all := []column{
		{"RANK", 4, 0, func(i int, _ spots.Spot) string {
			if i == 0 {
				return "★#1"
			}
			return fmt.Sprintf("#%d", i+1)
		}},
		{"SPOT", 12, 0, func(_ int, sp spots.Spot) string { return sp.Name }},
		{"SCORE", 8, 0, func(_ int, sp spots.Spot) string {
			return fmt.Sprintf("%3d (%s)", sp.Score.Total, sp.Score.Grade)
		}},
		{"CHANNEL", 25, 6, func(_ int, sp spots.Spot) string { return sp.ChannelLabel }},
		{"RSSI / SNR", 12, 1, func(_ int, sp spots.Spot) string {
			return fmt.Sprintf("%3d / %2d dB", sp.RSSI, sp.SNR)
		}},
		{"GW / WAN", 15, 3, func(_ int, sp spots.Spot) string {
			return fmt.Sprintf("%5.1f / %5.1f", sp.GatewayAvgMs, sp.WANAvgMs)
		}},
		{"DOWN / UP", 13, 2, func(_ int, sp spots.Spot) string {
			if sp.DownloadMbps <= 0 {
				return "    -"
			}
			return fmt.Sprintf("%4.0f / %4.0fM", sp.DownloadMbps, sp.UploadMbps)
		}},
		{"RPM", 5, 5, func(_ int, sp spots.Spot) string {
			if sp.LoadedRPM <= 0 {
				return "-"
			}
			return fmt.Sprintf("%d", sp.LoadedRPM)
		}},
	}

	// Keep the lowest prio columns that fit, with two cells between columns.
	keep := make([]bool, len(all))
	used := -2
	for prio := 0; prio <= 6; prio++ {
		for i, c := range all {
			if c.prio == prio && (prio == 0 || used+2+c.width <= w) {
				keep[i] = true
				used += 2 + c.width
			}
		}
	}
	var cols []column
	for i, c := range all {
		if keep[i] {
			cols = append(cols, c)
		}
	}
	// Spend the spare cells: longer spot names first, then a score bar.
	spare := w - used
	for i := range cols {
		if cols[i].title == "SPOT" && spare != 0 {
			grow := max(min(spare, 16), 4-cols[i].width)
			cols[i].width += grow
			spare -= grow
		}
	}
	for i := range cols {
		if cols[i].title == "SCORE" && spare >= 9 {
			cols[i].width, cols[i].cell = 17, scoreBar
		}
	}
	return cols
}

// Tab 2: spots leaderboard, with the selected spot and the live spot below it when they fit.
func (m *Model) renderSpotsView(w, h int) string {
	ranked := m.spotsStore.Ranked()
	if w < compactW || h < listH {
		return strings.Join(m.spotTable(ranked, w, h), "\n")
	}

	topH, botH := h, 0
	stacked := w < minTwoColW
	switch {
	case stacked && h >= 34:
		botH = 18
	case h >= 18:
		botH = max(h*42/100, 9)
	}
	if botH > 0 {
		topH = h - botH
	}

	inner := w - 4
	header := CardHeader("★", "SPOT FINDER LEADERBOARD", inner,
		"[Space] pin here · [j/k] select · [d] delete · [e] export",
		"[Space] pin · [j/k] select · [d] delete",
		"[Space] pin",
	)
	top := cardBox(StyleCardHi, w, topH, stack(topH-2,
		text(0, header),
		text(2, ""),
		flexible(1, 1, func(n int) string { return strings.Join(m.spotTable(ranked, inner, n), "\n") }),
	))
	switch {
	case botH == 0:
		return top
	case stacked && botH >= 18:
		return vstack(top, m.renderSelectedSpot(ranked, w, 9), m.renderLiveHere(ranked, w, botH-9))
	case stacked:
		return vstack(top, m.renderSelectedSpot(ranked, w, botH))
	}
	leftW := (w - 1) / 2
	return vstack(top, lipgloss.JoinHorizontal(lipgloss.Top,
		m.renderSelectedSpot(ranked, leftW, botH), " ", m.renderLiveHere(ranked, w-1-leftW, botH)))
}

// spotTable renders the leaderboard rows and keeps the selected row in view.
func (m *Model) spotTable(ranked []spots.Spot, w, rows int) []string {
	if len(ranked) == 0 {
		tip := "No spots pinned yet -- walk to a desk or room and press [Space] to pin it."
		more := "Tip: run [b] for an 8-second speed burst at each spot before pinning so spots rank by RF, jitter, throughput, and bufferbloat."
		lines := strings.Split(StyleValue.Render(ansi.Wrap(tip, max(w, 1), "")), "\n")
		lines = append(lines, "")
		return append(lines, strings.Split(StyleSubtitle.Render(ansi.Wrap(more, max(w, 1), "")), "\n")...)
	}

	cols := spotColumns(w)
	var out []string
	if rows >= 3 {
		var titles []string
		for _, c := range cols {
			titles = append(titles, pad(c.title, c.width))
		}
		out = append(out, fit(StyleMuted.Render(strings.Join(titles, "  ")), w))
		rows--
	}
	start := windowStart(m.selectedIdx, len(ranked), rows)
	for i := start; i < len(ranked) && i < start+rows; i++ {
		style := lipgloss.NewStyle().Foreground(ColText)
		if i == 0 {
			style = style.Foreground(ColEmerald).Bold(true)
		}
		if i == m.selectedIdx {
			style = style.Background(ColTrack)
		}
		// Style each cell alone, so the color reset after a bar does not end the row style.
		var cells []string
		for _, c := range cols {
			cells = append(cells, style.Render(pad(c.cell(i, ranked[i]), c.width)))
		}
		out = append(out, fit(strings.Join(cells, style.Render("  ")), w))
	}
	return out
}

func (m *Model) renderSelectedSpot(ranked []spots.Spot, w, h int) string {
	inner := w - 4
	if len(ranked) == 0 || m.selectedIdx >= len(ranked) {
		return cardBox(StyleCard, w, h, stack(h-2,
			text(0, CardHeader("◎", "HOW TO FIND YOUR SWEET SPOT", inner)),
			text(5, ""),
			text(1, StyleSubtitle.Render("1. Stand at spot #1 (e.g. your desk) and press [b] then [Space].")),
			text(2, StyleSubtitle.Render("2. Walk to spot #2 (window, couch, another room) and press [Space].")),
			text(3, StyleSubtitle.Render("3. Compare RSSI, SNR, router jitter, and throughput side by side.")),
			text(4, StyleSubtitle.Render("4. Press [e] to export a Markdown leaderboard + advice report.")),
		))
	}

	sp, best := ranked[m.selectedIdx], ranked[0]
	speed := StyleMuted.Render(firstFit(inner,
		"Speed: not measured at pin time (run [b] before pinning)",
		"Speed: not measured"))
	if sp.DownloadMbps > 0 {
		speed = joinFit(inner,
			fmt.Sprintf("Speed: ↓ %.1f ↑ %.1f Mbps", sp.DownloadMbps, sp.UploadMbps),
			fmt.Sprintf("Loaded %d RPM (+%.0f ms)", sp.LoadedRPM, sp.BloatDeltaMs))
	}
	verdict := lipgloss.NewStyle().Foreground(ColEmerald).Bold(true).Render("★ Currently your #1 ranked spot!")
	if m.selectedIdx > 0 {
		verdict = StyleSubtitle.Render(firstFit(inner,
			fmt.Sprintf("Vs #1 (%s): %+d score, %+d dB SNR, %+.1f ms router RTT",
				best.Name, sp.Score.Total-best.Score.Total, sp.SNR-best.SNR, sp.GatewayAvgMs-best.GatewayAvgMs),
			fmt.Sprintf("Vs #1: %+d score, %+d dB SNR, %+.1f ms",
				sp.Score.Total-best.Score.Total, sp.SNR-best.SNR, sp.GatewayAvgMs-best.GatewayAvgMs),
		))
	}
	at := sp.Timestamp.Format("15:04:05")
	return cardBox(StyleCard, w, h, stack(h-2,
		text(0, CardHeader("◎", "SELECTED SPOT: "+strings.ToUpper(sp.Name), inner, at)),
		text(6, ""),
		text(1, joinFit(inner,
			"Score "+Badge(fmt.Sprintf("%d/100 · %s", sp.Score.Total, sp.Score.Grade), ColorForScore(sp.Score.Total)),
			StyleValue.Render(sp.ChannelLabel),
			fmt.Sprintf("PHY %.0f Mbps", sp.TxRateMbps))),
		text(2, joinFit(inner,
			fmt.Sprintf("RF: %d dBm / %d dB SNR", sp.RSSI, sp.SNR),
			fmt.Sprintf("Loss %.1f%%", sp.LossPct),
			fmt.Sprintf("Noise %d dBm", sp.Noise))),
		text(3, joinFit(inner,
			fmt.Sprintf("Router %.1f ms (jitter %.1f ms)", sp.GatewayAvgMs, sp.GatewayJitMs),
			fmt.Sprintf("WAN %.1f ms", sp.WANAvgMs))),
		text(4, speed),
		text(7, ""),
		text(5, verdict),
	))
}

func (m *Model) renderLiveHere(ranked []spots.Spot, w, h int) string {
	inner, rows := w-4, h-2
	diff := StyleMuted.Render("Pin at least one spot to see live delta vs. your best spot.")
	if len(ranked) > 0 {
		best := ranked[0]
		delta := m.score.Total - best.Score.Total
		col := ColAmber
		if delta > 0 {
			col = ColEmerald
		} else if delta < -5 {
			col = ColCoral
		}
		diff = lipgloss.NewStyle().Bold(true).Foreground(col).Render(firstFit(inner,
			fmt.Sprintf("Live vs #1 (%s): %+d score, %+d dB SNR", best.Name, delta, m.link.SNR-best.SNR),
			fmt.Sprintf("Live vs #1: %+d score, %+d dB SNR", delta, m.link.SNR-best.SNR),
		))
	}

	summary := func(w int) string {
		return strings.Join([]string{
			joinFit(w, Badge(fmt.Sprintf("%d/100 · %s", m.score.Total, m.score.Grade), ColorForScore(m.score.Total)), StyleValue.Render(m.link.ChannelLabel())),
			joinFit(w, fmt.Sprintf("RF: %d dBm / %d dB SNR", m.link.RSSI, m.link.SNR), fmt.Sprintf("PHY %.0f Mbps", m.link.TxRateMbps)),
			joinFit(w, fmt.Sprintf("Router %.1f ms (jit %.1f)", m.gwStats.AvgMs, m.gwStats.JitterMs), fmt.Sprintf("WAN %.1f ms", m.wanStats.AvgMs)),
			StyleMuted.Render(firstFit(w, "Press [Space] to pin your current location", "[Space] pin this spot")),
		}, "\n")
	}
	tower := RenderSignalTower(m.link.RSSI, m.link.Connected, m.link.IsEthernet)
	now := summary(inner)
	if room := inner - lipgloss.Width(tower) - 2; room >= 30 {
		now = lipgloss.JoinHorizontal(lipgloss.Bottom, tower, "  ", summary(room))
	}
	return cardBox(StyleCard, w, h, stack(rows,
		text(1, CardHeader("●", "LIVE HERE NOW", inner, "Walk-around preview")),
		text(3, ""),
		text(0, now),
		text(4, ""),
		text(2, diff),
	))
}

// Tab 3: RF channel spectrum and DNS resolver race.
func (m *Model) renderSpectrumView(w, h int) string {
	if w < compactW || h < listH {
		dnsLines := m.dnsLines(w)
		dnsH := min(len(dnsLines), h/2)
		rfH := h - dnsH
		return vstack(m.rfRows(w, rfH), listRows(dnsLines, dnsH))
	}

	dnsH := max(h*45/100, 4)
	switch {
	case h >= 26:
		dnsH = 11
	case h >= 16:
		dnsH = 8
	}
	rfH := h - dnsH
	inner := w - 4

	rfRights := []string{"Press [s] to scan nearby channels", "[s] scan"}
	if m.scanning {
		rfRights = []string{m.spinner.View() + " scanning..."}
	} else if m.scan.TotalAPs > 0 {
		rfRights = []string{
			fmt.Sprintf("%d APs · Best 5 GHz: Ch %d (non-DFS Ch %d) · Best 2.4 GHz: Ch %d",
				m.scan.TotalAPs, m.scan.Best5GHzChannel, m.scan.Best5GHzNonDFS, m.scan.Best2GHzChannel),
			fmt.Sprintf("%d APs · 5G Ch %d · 2.4G Ch %d", m.scan.TotalAPs, m.scan.Best5GHzChannel, m.scan.Best2GHzChannel),
			fmt.Sprintf("%d APs", m.scan.TotalAPs),
		}
	}
	rf := cardBox(StyleCardHi, w, rfH, stack(rfH-2,
		text(0, CardHeader("▤", "RF CHANNEL CONGESTION", inner, rfRights...)),
		text(2, ""),
		flexible(1, 1, func(n int) string { return m.rfRows(inner, n) }),
	))

	dnsRights := []string{"Press [r] to re-race", "[r] race"}
	if m.dnsRunning {
		dnsRights = []string{m.spinner.View() + " racing..."}
	} else if len(m.link.DNSServers) > 0 {
		dnsRights = []string{"Configured: " + strings.Join(m.link.DNSServers, ", "), strings.Join(m.link.DNSServers, ", "), "[r] race"}
	}
	dnsLines := m.dnsLines(inner)
	blocks := []block{
		text(0, CardHeader("⌁", "DNS RESOLVER RACE", inner, dnsRights...)),
		text(3, ""),
		// The list asks for all its rows, so the note and the blank row go first.
		flexible(1, min(len(dnsLines), max(dnsH-3, 1)), func(n int) string { return listRows(dnsLines, n) }),
	}
	if note := m.dnsNote(inner); note != "" {
		blocks = append(blocks, text(2, note))
	}
	return vstack(rf, cardBox(StyleCard, w, dnsH, stack(dnsH-2, blocks...)))
}

// rfRows fills exactly the given rows with the channel list, or with the guide before a scan.
func (m *Model) rfRows(w, rows int) string {
	if len(m.scan.Buckets) > 0 {
		return listRows(m.rfLines(w), rows)
	}
	wrap := func(st lipgloss.Style, s string) string { return st.Render(ansi.Wrap(s, max(w, 1), "")) }
	return clip(strings.Join(stack(rows,
		text(0, firstFit(w,
			Badge("PRESS [s]", ColCyan)+"  "+StyleValue.Render("Run an on-demand RF neighborhood scan"),
			Badge("PRESS [s]", ColCyan)+" "+StyleValue.Render("Scan channels"),
		)),
		text(6, ""),
		text(4, wrap(StyleSubtitle, "Scanning is on-demand so idle latency monitoring never suffers off-channel ping spikes.")),
		text(7, ""),
		text(5, StyleMuted.Render("Channel guide:")),
		text(1, wrap(StyleSubtitle, "• 2.4 GHz: Only Channels 1, 6, and 11 are non-overlapping (keep 20 MHz in apartments).")),
		text(2, wrap(StyleSubtitle, "• 5 GHz: 80 MHz blocks are UNII-1 (36-48), DFS (52-64, 100-144), and UNII-3 (149-161).")),
		text(3, wrap(StyleSubtitle, "• 6 GHz: Cleanest wide 80/160 MHz channels (requires Wi-Fi 6E/7 router + WPA3).")),
	), "\n"), w, rows)
}

func (m *Model) rfLines(w int) []string {
	maxCount := 1
	for _, b := range m.scan.Buckets {
		maxCount = max(maxCount, b.Count)
	}
	// Fixed parts: band and channel (14), APs (7), and the tag (9).
	showSig, showWidth := w >= 64, w >= 52
	fixed := 14 + 7 + 9
	if showSig {
		fixed += 13
	}
	if showWidth {
		fixed += 9
	}
	barW := w - fixed - 1

	var lines []string
	for _, b := range m.scan.Buckets {
		col := ColCyan
		if b.Count >= 5 {
			col = ColCoral
		} else if b.Count >= 3 {
			col = ColAmber
		} else if b.Band == wifi.Band6GHz {
			col = ColEmerald
		}
		line := fmt.Sprintf("%-7s Ch %-3d", b.Band, b.Channel)
		if showWidth {
			line += fmt.Sprintf(" (%3dMHz)", b.MaxWidthMHz)
		}
		if barW >= 4 {
			line += " " + RenderBar(float64(b.Count)/float64(maxCount), barW, col)
		}
		line += lipgloss.NewStyle().Foreground(col).Render(fmt.Sprintf(" %2d APs", b.Count))
		if showSig {
			sig := ""
			if b.MaxRSSI != 0 {
				sig = fmt.Sprintf("max %3d dBm", b.MaxRSSI)
			}
			line += StyleMuted.Render("  " + pad(sig, 11))
		}
		if b.IsCurrent {
			line += " " + Badge("YOU", ColCyan)
		} else if b.IsRecommended {
			line += " " + Badge("CLEAN", ColEmerald)
		}
		lines = append(lines, fit(line, w))
	}
	return lines
}

// dnsLines renders one row per resolver. It drops the server, the cached time, and the bar
// as the width shrinks.
func (m *Model) dnsLines(w int) []string {
	if len(m.dns.Entries) == 0 {
		return []string{fit(StyleSubtitle.Render("Racing DNS resolvers..."), w)}
	}
	maxMs, serverW := 55.0, 0
	for _, e := range m.dns.Entries {
		maxMs = math.Max(maxMs, e.UncachedMs)
		serverW = max(serverW, lipgloss.Width(e.Server))
	}
	const nameW, uncachedW, cachedW, tagW = 11, 18, 18, 10
	showServer, showCached := false, false
	left := w - nameW - uncachedW - tagW
	if left >= cachedW {
		showCached = true
		left -= cachedW
	}
	if left >= serverW+3 {
		showServer = true
		left -= serverW + 3
	}
	barW := left - 1

	var lines []string
	for _, e := range m.dns.Entries {
		line := pad(e.Name, nameW)
		if showServer {
			line += " (" + pad(e.Server, serverW) + ")"
		}
		if e.Failed {
			lines = append(lines, fit(line+" "+StyleMuted.Render("timeout"), w))
			continue
		}
		if barW >= 4 {
			line += " " + RenderBar(e.UncachedMs/maxMs, barW, ColorForLatency(e.UncachedMs, 25, 45))
		}
		line += fmt.Sprintf(" uncached %5.1fms", e.UncachedMs)
		if showCached {
			line += fmt.Sprintf(" · cached %5.1fms", e.CachedMs)
		}
		if !e.IsSystem && e.Server == m.dns.Fastest.Server {
			line += " " + Badge("FASTEST", ColEmerald)
		}
		lines = append(lines, fit(line, w))
	}
	return lines
}

func (m *Model) dnsNote(w int) string {
	gain := m.dns.System.UncachedMs - m.dns.Fastest.UncachedMs
	if len(m.dns.Entries) == 0 || m.dns.System.Failed || m.dns.Fastest.Failed || gain < 10 {
		return ""
	}
	lead := fmt.Sprintf("Note: %s (%s) is %.1f ms faster uncached than System DNS", m.dns.Fastest.Name, m.dns.Fastest.Server, gain)
	return StyleMuted.Render(firstFit(w,
		lead+" (corporate DNS proxies like Scout add ~15-20 ms on managed laptops).",
		lead+".",
		fmt.Sprintf("Note: %s is %.1f ms faster than System DNS.", m.dns.Fastest.Name, gain),
	))
}

// advisorTwoCol reports whether the playbook has its own column.
func (m *Model) advisorTwoCol() bool { return m.width >= advisorColW }

// advisorMaxScroll is the last item that [j] can reach. In one column the playbook and the
// modem section follow the findings in the same list.
func (m *Model) advisorMaxScroll() int {
	n := len(m.advice)
	if !m.advisorTwoCol() {
		n += len(m.playbook) + 1
	}
	return max(n-1, 0)
}

func (m *Model) findingLines(a advisor.Advice, w int) []string {
	lines := []string{fit(fmt.Sprintf("%s %s %s", severityBadge(a.Severity), StyleMuted.Render("["+a.Category+"]"), StyleValue.Render(a.Title)), w)}
	wrapped := func(st lipgloss.Style, lead, s string) {
		for i, l := range strings.Split(ansi.Wrap(s, max(w-3-lipgloss.Width(lead), 1), ""), "\n") {
			p := strings.Repeat(" ", lipgloss.Width(lead))
			if i == 0 {
				p = lead
			}
			lines = append(lines, fit("   "+st.Render(p+l), w))
		}
	}
	wrapped(StyleSubtitle, "", a.Detail)
	wrapped(lipgloss.NewStyle().Foreground(ColTeal), "→ ", a.Action)
	return lines
}

func (m *Model) playbookLines(item advisor.ChecklistItem, w int) []string {
	icon := lipgloss.NewStyle().Foreground(ColCyan).Render("○")
	switch item.Status {
	case "ok":
		icon = lipgloss.NewStyle().Bold(true).Foreground(ColEmerald).Render("✓")
	case "warn":
		icon = lipgloss.NewStyle().Bold(true).Foreground(ColAmber).Render("▲")
	}
	lines := []string{fit(icon+" "+StyleValue.Render(item.Title), w)}
	for _, l := range strings.Split(ansi.Wrap(item.Note, max(w-2, 1), ""), "\n") {
		lines = append(lines, fit("  "+StyleMuted.Render(l), w))
	}
	return lines
}

func (m *Model) modemLines(w int) []string {
	title := lipgloss.NewStyle().Bold(true).Foreground(ColCyan)
	if !m.modem.Parsed {
		return []string{
			fit(title.Render("Modem Doctor (DOCSIS 3.0/3.1)"), w),
			fit(StyleMuted.Render("Copy your router's Cable Connection table"), w),
			fit(StyleMuted.Render("(NETGEAR / ARRIS / Motorola) & press [m]."), w),
		}
	}
	return []string{
		fit(title.Render("DOCSIS Cable Modem ("+m.modem.Health+")"), w),
		fit(StyleSubtitle.Render(fmt.Sprintf("Down: %.1f..%.1f dBmV (%d QAM, %d OFDM)", m.modem.DownPowerMin, m.modem.DownPowerMax, m.modem.DownstreamCount, m.modem.OFDMCount)), w),
		fit(StyleSubtitle.Render(fmt.Sprintf("SNR: %.1f..%.1f dB · Up: %.1f dBmV (%d ch)", m.modem.DownSNRMin, m.modem.DownSNRMax, m.modem.UpPowerMax, m.modem.UpstreamCount)), w),
		fit(StyleMuted.Render(fmt.Sprintf("Uncorrectables: %d · Uptime: %s", m.modem.TotalUncorrectables, m.modem.Uptime)), w),
	}
}

// Tab 4: findings and the optimization playbook, side by side or in one scrolling list.
func (m *Model) renderAdvisorView(w, h int) string {
	twoCol := m.advisorTwoCol()
	boxed := w >= compactW && h >= listH
	leftW := w
	if twoCol {
		leftW = w * 62 / 100
	}
	inner, rows := leftW, h
	if boxed {
		inner, rows = leftW-4, h-2
	}

	var items [][]string
	for _, a := range m.advice {
		items = append(items, m.findingLines(a, inner))
	}
	if len(m.advice) == 0 {
		items = append(items, []string{fit(StyleSubtitle.Render("Collecting live measurements..."), inner)})
	}
	if !twoCol {
		for i, p := range m.playbook {
			lines := m.playbookLines(p, inner)
			if i == 0 {
				lines = append([]string{StyleTitle.Render(fit("✓ OPTIMIZATION PLAYBOOK", inner))}, lines...)
			}
			items = append(items, lines)
		}
		items = append(items, m.modemLines(inner))
	}

	scroll := min(m.advisorScroll, max(len(items)-1, 0))
	hints := []string{"[j/k] scroll · [m] paste cable modem stats", "[j/k] scroll"}
	if scroll > 0 {
		hints = []string{fmt.Sprintf("showing from #%d/%d · [j/k] scroll", scroll+1, len(items)), fmt.Sprintf("#%d/%d", scroll+1, len(items))}
	}
	var lines []string
	if rows >= 4 {
		lines = append(lines, CardHeader("✦", fmt.Sprintf("ACTIVE FINDINGS (%d)", len(m.advice)), inner, hints...))
	}
	if rows >= 8 {
		lines = append(lines, "")
	}
	for i := scroll; i < len(items); i++ {
		room := rows - len(lines)
		last := i == len(items)-1
		// Keep one row for the scroll hint, unless this is the last item.
		if need := len(items[i]); i > scroll && ((last && need > room) || (!last && need+1 > room)) {
			lines = append(lines, fit(StyleMuted.Render(fmt.Sprintf("↓ %d more -- press [j] to scroll down", len(items)-i)), inner))
			break
		}
		lines = append(lines, items[i]...)
		if rows-len(lines) > 2 {
			lines = append(lines, "")
		}
	}
	if !boxed {
		return clip(strings.Join(lines, "\n"), w, h)
	}
	left := cardBox(StyleCardHi, leftW, h, lines)
	if !twoCol {
		return left
	}

	rightW := w - 1 - leftW
	rightInner := rightW - 4
	modem := append([]string{lipgloss.NewStyle().Foreground(ColBorder).Render(strings.Repeat("─", max(rightInner, 1)))}, m.modemLines(rightInner)...)
	var full, short []string
	for _, p := range m.playbook {
		pl := m.playbookLines(p, rightInner)
		full = append(full, pl...)
		short = append(short, pl[0])
	}
	list := full
	if len(full)+len(modem)+2 > h-2 {
		list = short // titles only when the notes do not fit
	}
	right := cardBox(StyleCard, rightW, h, stack(h-2,
		text(0, CardHeader("✓", "OPTIMIZATION PLAYBOOK", rightInner, "Vetted checklist")),
		text(4, ""),
		text(1, listRows(list, min(len(list), max(h-3, 1)))),
		text(5, ""),
		text(3, strings.Join(modem, "\n")),
	))
	return lipgloss.JoinHorizontal(lipgloss.Top, left, " ", right)
}

func (m *Model) renderPinModal(w, h int) string {
	boxW, boxH := min(w, 62), min(h, 9)
	inner := max(boxW-6, 1)
	input := m.pinInput
	input.Width = max(min(38, inner-lipgloss.Width(input.Prompt)-1), 1)
	lines := stack(boxH-2,
		text(1, lipgloss.NewStyle().Bold(true).Foreground(ColCyan).Render("★ Pin Current Spot")),
		text(4, ""),
		text(3, joinFit(inner,
			StyleSubtitle.Render(fmt.Sprintf("Score %d (%s)", m.score.Total, m.score.Grade)),
			StyleSubtitle.Render(fmt.Sprintf("%d dBm / %d dB SNR", m.link.RSSI, m.link.SNR)),
			StyleSubtitle.Render(m.link.ChannelLabel()))),
		text(5, ""),
		text(0, input.View()),
		text(6, ""),
		text(2, StyleMuted.Render(firstFit(inner,
			"Press [Enter] to save (blank = auto name) · [Esc] to cancel",
			"[Enter] save · [Esc] cancel",
		))),
	)
	if boxH < 3 || boxW < 8 {
		return clip(input.View(), w, h)
	}
	box := StyleCardHi.Padding(0, 2).Width(boxW - 2).Height(boxH - 2).Render(clip(strings.Join(lines, "\n"), inner, boxH-2))
	return lipgloss.Place(w, h, lipgloss.Center, lipgloss.Center, box)
}

func (m *Model) renderFooter(w, h int) string {
	if h < 8 {
		return ""
	}
	// Keys are dropped from the highest prio when they do not fit.
	keys := []struct {
		key, full, short string
		prio             int
	}{
		{"1-4", "Views", "Views", 1},
		{"Space", "Pin", "Pin", 1},
		{"b", "Speed Burst", "Speed", 2},
		{"c", "Continuous", "Cont", 4},
		{"n", "netQuality", "netQ", 5},
		{"s", "RF Scan", "Scan", 3},
		{"r", "DNS", "DNS", 6},
		{"m", "Modem", "Modem", 8},
		{"e", "Export", "Export", 7},
		{"q", "Quit", "Quit", 0},
	}
	bar := func(short bool, maxPrio int) string {
		var parts []string
		for _, k := range keys {
			if k.prio > maxPrio {
				continue
			}
			label := k.full
			if short {
				label = k.short
			}
			parts = append(parts, lipgloss.NewStyle().Bold(true).Foreground(ColCyan).Background(ColTrack).Render(" "+k.key+" ")+StyleMuted.Render(" "+label))
		}
		sep := "  "
		if short {
			sep = " "
		}
		return " " + strings.Join(parts, sep)
	}
	variants := []string{bar(false, 8)}
	for prio := 8; prio >= 0; prio-- {
		variants = append(variants, bar(true, prio))
	}
	keyRow := firstFit(w, variants...)
	if m.toast == "" {
		return keyRow
	}
	toast := fit(" "+StyleToast.Render("● "+m.toast), w)
	if h < 30 {
		return toast // no room for two rows, so the toast replaces the keys until it clears
	}
	return toast + "\n" + keyRow
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
