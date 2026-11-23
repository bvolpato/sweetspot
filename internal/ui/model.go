package ui

import (
	"context"
	"fmt"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/bvolpato/sweetspot/internal/advisor"
	"github.com/bvolpato/sweetspot/internal/probe"
	"github.com/bvolpato/sweetspot/internal/spots"
	"github.com/bvolpato/sweetspot/internal/wifi"
)

type Tab int

const (
	TabRadar Tab = iota
	TabSpots
	TabSpectrum
	TabAdvisor
)

// Options configures the TUI startup behavior.
type Options struct {
	WANTarget        string
	ContinuousSpeed  bool
	InitialModemText string
	PollInterval     time.Duration
}

// Messages delivered into Bubble Tea's event loop.
type (
	tickMsg          time.Time
	linkSampleMsg    wifi.LinkInfo
	scanDoneMsg      wifi.ScanResult
	dnsDoneMsg       probe.DNSRaceResult
	speedDoneMsg     probe.SpeedResult
	speedProgressMsg probe.SpeedProgress
	contCycleMsg     struct{}
	toastClearMsg    struct{}
	modemLoadedMsg   advisor.ModemReport
)

// Model holds all live telemetry and interactive TUI state.
type Model struct {
	ctx    context.Context
	cancel context.CancelFunc
	opts   Options

	width  int
	height int
	tab    Tab

	spinner  spinner.Model
	pinInput textinput.Model
	pinning  bool

	// Live telemetry
	link       wifi.LinkInfo
	prevLink   wifi.LinkInfo
	hasPrev    bool
	lastBSSID  string
	roamCount  int
	rssiHist   []float64
	snrHist    []float64
	downHist   []float64
	upHist     []float64

	gwRing     *probe.PingRing
	wanRing    *probe.PingRing
	gwCancel   context.CancelFunc
	activeGW   string
	gwStats    probe.PingStats
	wanStats   probe.PingStats

	// Throughput & responsiveness
	speed            probe.SpeedResult
	speedRunning     bool
	speedProgress    probe.SpeedProgress
	continuousMode   bool
	progressCh       chan probe.SpeedProgress

	// RF scan, DNS, Modem, Advisor, Spots
	scan        wifi.ScanResult
	scanning    bool
	dns         probe.DNSRaceResult
	dnsRunning  bool
	modem       advisor.ModemReport
	score         advisor.ScoreBreakdown
	advice        []advisor.Advice
	playbook      []advisor.ChecklistItem
	spotsStore    *spots.Store
	selectedIdx   int
	advisorScroll int

	toast string
}

// NewModel builds an initialized SweetSpot TUI model.
func NewModel(opts Options) *Model {
	if opts.WANTarget == "" {
		opts.WANTarget = "1.1.1.1"
	}
	if opts.PollInterval <= 0 {
		opts.PollInterval = time.Second
	}

	ctx, cancel := context.WithCancel(context.Background())

	sp := spinner.New()
	sp.Spinner = spinner.MiniDot
	sp.Style = lipgloss.NewStyle().Foreground(ColCyan)

	ti := textinput.New()
	ti.Placeholder = "Desk by window, Kitchen, Conf Room..."
	ti.CharLimit = 36
	ti.Width = 38

	m := &Model{
		ctx:            ctx,
		cancel:         cancel,
		opts:           opts,
		width:          120,
		height:         36,
		spinner:        sp,
		pinInput:       ti,
		gwRing:         probe.NewPingRing("", 220, 40.0),
		wanRing:        probe.NewPingRing(opts.WANTarget, 220, 80.0),
		continuousMode: opts.ContinuousSpeed,
		progressCh:     make(chan probe.SpeedProgress, 16),
		spotsStore:     spots.NewStore(),
	}

	if opts.InitialModemText != "" {
		m.modem = advisor.ParseModemDiagnostics(opts.InitialModemText)
	}
	return m
}

func (m *Model) Init() tea.Cmd {
	// Start continuous WAN ICMP stream immediately.
	go probe.StreamPing(m.ctx, m.opts.WANTarget, 500*time.Millisecond, func(s probe.PingSample) {
		m.wanRing.Add(s)
	})

	cmds := []tea.Cmd{
		m.spinner.Tick,
		m.readLinkCmd(),
		m.scheduleTick(),
		m.waitSpeedProgressCmd(),
		m.runDNSRaceCmd(""),
	}
	if m.continuousMode {
		cmds = append(cmds, m.triggerSpeedBurstCmd(3*time.Second, 2*time.Second, "continuous"))
	}
	return tea.Batch(cmds...)
}

func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd

	case tickMsg:
		m.gwStats = m.gwRing.Stats()
		m.wanStats = m.wanRing.Stats()
		m.recompute()
		return m, tea.Batch(m.readLinkCmd(), m.scheduleTick())

	case linkSampleMsg:
		next := wifi.LinkInfo(msg)
		if m.hasPrev && next.Timestamp.After(m.prevLink.Timestamp) {
			dt := next.Timestamp.Sub(m.prevLink.Timestamp).Seconds()
			if dt > 0.1 && next.InBytes >= m.prevLink.InBytes && next.OutBytes >= m.prevLink.OutBytes {
				next.LiveRxMbps = float64(next.InBytes-m.prevLink.InBytes) * 8.0 / (dt * 1e6)
				next.LiveTxMbps = float64(next.OutBytes-m.prevLink.OutBytes) * 8.0 / (dt * 1e6)
			}
		}
		m.prevLink = next
		m.hasPrev = true
		m.link = next

		var roamToast tea.Cmd
		if next.BSSID != "" {
			if m.lastBSSID != "" && next.BSSID != m.lastBSSID {
				m.roamCount++
				roamToast = m.setToast(fmt.Sprintf("AP roam #%d -> %s (Ch %d, %d dBm)", m.roamCount, next.BSSID, next.Channel, next.RSSI))
			}
			m.lastBSSID = next.BSSID
		}

		// Start or restart gateway ping stream whenever the default gateway changes.
		if next.Gateway != "" && next.Gateway != m.activeGW {
			if m.gwCancel != nil {
				m.gwCancel()
			}
			m.activeGW = next.Gateway
			m.gwRing.Reset(next.Gateway)
			var gwCtx context.Context
			gwCtx, m.gwCancel = context.WithCancel(m.ctx)
			go probe.StreamPing(gwCtx, next.Gateway, 500*time.Millisecond, func(s probe.PingSample) {
				m.gwRing.Add(s)
			})
		}

		if next.Connected {
			m.rssiHist = appendRing(m.rssiHist, float64(next.RSSI), 220)
			m.snrHist = appendRing(m.snrHist, float64(next.SNR), 220)
		}
		if !m.speedRunning {
			m.downHist = appendRing(m.downHist, next.LiveRxMbps, 220)
			m.upHist = appendRing(m.upHist, next.LiveTxMbps, 220)
		}
		m.gwStats = m.gwRing.Stats()
		m.wanStats = m.wanRing.Stats()
		m.recompute()
		return m, roamToast

	case speedProgressMsg:
		m.speedProgress = probe.SpeedProgress(msg)
		if msg.DownloadMbps > 0 {
			m.downHist = appendRing(m.downHist, msg.DownloadMbps, 64)
		}
		if msg.UploadMbps > 0 {
			m.upHist = appendRing(m.upHist, msg.UploadMbps, 64)
		}
		return m, m.waitSpeedProgressCmd()

	case speedDoneMsg:
		m.speedRunning = false
		res := probe.SpeedResult(msg)
		m.speed = res
		if res.DownloadMbps > 0 {
			m.downHist = appendRing(m.downHist, res.DownloadMbps, 64)
		}
		if res.UploadMbps > 0 {
			m.upHist = appendRing(m.upHist, res.UploadMbps, 64)
		}
		m.recompute()
		var cmds []tea.Cmd
		if res.Err != "" {
			cmds = append(cmds, m.setToast("Speed probe warning: "+res.Err))
		} else {
			cmds = append(cmds, m.setToast(fmt.Sprintf("Speed updated: %.1f down / %.1f up Mbps (%d RPM)", res.DownloadMbps, res.UploadMbps, res.RPM)))
		}
		if m.continuousMode {
			cmds = append(cmds, tea.Tick(2500*time.Millisecond, func(time.Time) tea.Msg { return contCycleMsg{} }))
		}
		return m, tea.Batch(cmds...)

	case contCycleMsg:
		if m.continuousMode && !m.speedRunning {
			return m, m.triggerSpeedBurstCmd(3*time.Second, 2*time.Second, "continuous")
		}
		return m, nil

	case scanDoneMsg:
		m.scanning = false
		m.scan = wifi.ScanResult(msg)
		m.recompute()
		if m.scan.Err != "" {
			return m, m.setToast(m.scan.Err)
		}
		return m, m.setToast(fmt.Sprintf("RF scan complete: %d nearby APs (best 5 GHz: Ch %d / non-DFS Ch %d)",
			m.scan.TotalAPs, m.scan.Best5GHzChannel, m.scan.Best5GHzNonDFS))

	case dnsDoneMsg:
		m.dnsRunning = false
		m.dns = probe.DNSRaceResult(msg)
		m.recompute()
		return m, nil

	case modemLoadedMsg:
		rep := advisor.ModemReport(msg)
		if !rep.Parsed {
			return m, m.setToast("No DOCSIS cable modem table found in clipboard")
		}
		m.modem = rep
		m.recompute()
		m.tab = TabAdvisor
		return m, m.setToast(fmt.Sprintf("Parsed cable modem stats: %d downstream, %d OFDM, %d upstream (%s)",
			rep.DownstreamCount, rep.OFDMCount, rep.UpstreamCount, rep.Health))

	case toastClearMsg:
		m.toast = ""
		return m, nil

	case tea.KeyMsg:
		if m.pinning {
			return m.handlePinKey(msg)
		}
		return m.handleKey(msg)
	}

	return m, nil
}

func (m *Model) handlePinKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.pinning = false
		m.pinInput.Blur()
		return m, nil
	case "enter":
		name := m.pinInput.Value()
		m.pinning = false
		m.pinInput.SetValue("")
		m.pinInput.Blur()
		sp := m.spotsStore.Pin(name, m.link, m.gwStats, m.wanStats, m.speed, m.score)
		return m, m.setToast(fmt.Sprintf("Pinned %q -- Score %d (%s)", sp.Name, sp.Score.Total, sp.Score.Grade))
	default:
		var cmd tea.Cmd
		m.pinInput, cmd = m.pinInput.Update(msg)
		return m, cmd
	}
}

func (m *Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "q", "ctrl+c":
		m.cancel()
		return m, tea.Quit
	case "1":
		m.tab = TabRadar
	case "2":
		m.tab = TabSpots
	case "3":
		m.tab = TabSpectrum
	case "4":
		m.tab = TabAdvisor
	case "tab", "right", "l":
		m.tab = (m.tab + 1) % 4
	case "shift+tab", "left", "h":
		m.tab = (m.tab + 3) % 4
	case "j", "down":
		if m.tab == TabSpots && m.selectedIdx < m.spotsStore.Len()-1 {
			m.selectedIdx++
		} else if m.tab == TabAdvisor && m.advisorScroll < max(len(m.advice)-1, 0) {
			m.advisorScroll++
		}
	case "k", "up":
		if m.tab == TabSpots && m.selectedIdx > 0 {
			m.selectedIdx--
		} else if m.tab == TabAdvisor && m.advisorScroll > 0 {
			m.advisorScroll--
		}
	case "d", "backspace":
		if m.tab == TabSpots {
			ranked := m.spotsStore.Ranked()
			if m.selectedIdx >= 0 && m.selectedIdx < len(ranked) {
				removed := ranked[m.selectedIdx]
				m.spotsStore.Delete(removed.ID)
				if m.selectedIdx >= m.spotsStore.Len() && m.selectedIdx > 0 {
					m.selectedIdx--
				}
				return m, m.setToast(fmt.Sprintf("Removed spot %q", removed.Name))
			}
		}
	case " ", "p":
		m.pinning = true
		m.pinInput.SetValue("")
		m.pinInput.Focus()
		return m, textinput.Blink
	case "b":
		if !m.speedRunning {
			return m, tea.Batch(
				m.setToast("Running 8s download + upload + bufferbloat burst..."),
				m.triggerSpeedBurstCmd(5*time.Second, 3*time.Second, "burst"),
			)
		}
	case "c":
		m.continuousMode = !m.continuousMode
		if m.continuousMode {
			cmds := []tea.Cmd{m.setToast("Continuous walk-around speed probing ON (press c to pause)")}
			if !m.speedRunning {
				cmds = append(cmds, m.triggerSpeedBurstCmd(3*time.Second, 2*time.Second, "continuous"))
			}
			return m, tea.Batch(cmds...)
		}
		return m, m.setToast("Continuous speed probing PAUSED")
	case "n":
		if !m.speedRunning && runtime.GOOS == "darwin" {
			m.speedRunning = true
			m.speedProgress = probe.SpeedProgress{Phase: "networkQuality"}
			return m, tea.Batch(
				m.setToast("Running Apple networkQuality benchmark (~15-30s)..."),
				func() tea.Msg {
					return speedDoneMsg(probe.RunMacOSNetworkQuality(m.ctx))
				},
			)
		}
	case "s":
		if !m.scanning {
			m.scanning = true
			cur := m.link
			return m, tea.Batch(
				m.setToast("Scanning nearby Wi-Fi channels (brief latency blip normal)..."),
				func() tea.Msg {
					return scanDoneMsg(wifi.ScanNeighbors(m.ctx, cur))
				},
			)
		}
	case "r":
		if !m.dnsRunning {
			return m, tea.Batch(
				m.setToast("Racing DNS resolvers (System, 1.1.1.1, 9.9.9.9, 8.8.8.8, Router)..."),
				m.runDNSRaceCmd(m.link.Gateway),
			)
		}
	case "m":
		return m, m.readModemFromClipboardCmd()
	case "e":
		ts := time.Now().Format("20060102-150405")
		mdPath := fmt.Sprintf("sweetspot-report-%s.md", ts)
		if err := spots.ExportMarkdown(mdPath, m.spotsStore.Ranked(), m.advice); err != nil {
			return m, m.setToast("Export failed: " + err.Error())
		}
		return m, m.setToast("Exported report to " + mdPath)
	}
	return m, nil
}

func (m *Model) recompute() {
	m.score = advisor.ComputeScore(m.link, m.gwStats, m.wanStats, m.speed)
	m.advice = advisor.Diagnose(m.link, m.gwStats, m.wanStats, m.speed, m.scan, m.dns, m.modem)
	m.playbook = advisor.BuildPlaybook(m.link, m.gwStats, m.speed, m.scan, m.dns, m.modem)
	if m.advisorScroll >= len(m.advice) && len(m.advice) > 0 {
		m.advisorScroll = len(m.advice) - 1
	}
}

func (m *Model) scheduleTick() tea.Cmd {
	return tea.Tick(m.opts.PollInterval, func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
}

func (m *Model) readLinkCmd() tea.Cmd {
	return func() tea.Msg {
		info, _ := wifi.ReadLink(m.ctx)
		return linkSampleMsg(info)
	}
}

func (m *Model) runDNSRaceCmd(gw string) tea.Cmd {
	m.dnsRunning = true
	return func() tea.Msg {
		return dnsDoneMsg(probe.RunDNSRace(m.ctx, gw))
	}
}

func (m *Model) triggerSpeedBurstCmd(downDur, upDur time.Duration, source string) tea.Cmd {
	m.speedRunning = true
	m.speedProgress = probe.SpeedProgress{Phase: "download"}
	idle := m.wanStats.AvgMs
	return func() tea.Msg {
		res := probe.RunSpeedBurst(
			m.ctx,
			downDur,
			upDur,
			3,
			idle,
			func() float64 { return m.wanRing.Stats().LastMs },
			func(p probe.SpeedProgress) {
				select {
				case m.progressCh <- p:
				default:
				}
			},
		)
		if source == "continuous" {
			res.Source = "continuous"
		}
		return speedDoneMsg(res)
	}
}

func (m *Model) waitSpeedProgressCmd() tea.Cmd {
	return func() tea.Msg {
		select {
		case <-m.ctx.Done():
			return nil
		case p := <-m.progressCh:
			return speedProgressMsg(p)
		}
	}
}

func (m *Model) readModemFromClipboardCmd() tea.Cmd {
	return func() tea.Msg {
		var out []byte
		var err error
		switch runtime.GOOS {
		case "darwin":
			out, err = exec.CommandContext(m.ctx, "/usr/bin/pbpaste").Output()
		case "windows":
			out, err = exec.CommandContext(m.ctx, "powershell", "-NoProfile", "-Command", "Get-Clipboard").Output()
		default:
			out, err = exec.CommandContext(m.ctx, "wl-paste", "-n").Output()
			if err != nil {
				out, err = exec.CommandContext(m.ctx, "xclip", "-o", "-selection", "clipboard").Output()
			}
		}
		if err != nil || len(strings.TrimSpace(string(out))) == 0 {
			return modemLoadedMsg(advisor.ModemReport{})
		}
		return modemLoadedMsg(advisor.ParseModemDiagnostics(string(out)))
	}
}

func (m *Model) setToast(msg string) tea.Cmd {
	m.toast = msg
	return tea.Tick(5*time.Second, func(time.Time) tea.Msg {
		return toastClearMsg{}
	})
}

func appendRing(s []float64, v float64, limit int) []float64 {
	if len(s) >= limit {
		copy(s, s[1:])
		s[len(s)-1] = v
		return s
	}
	return append(s, v)
}
