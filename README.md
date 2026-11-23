# sweetspot

`sweetspot` is an interactive terminal dashboard for finding the best Wi-Fi spot in a home or office and diagnosing real network bottlenecks.

It reads live Wi-Fi telemetry without triggering off-channel scan stutters on macOS, tracks router and internet latency in parallel, measures download speed, upload speed, and bufferbloat, races DNS resolvers, and analyzes DOCSIS cable modem diagnostics.

## Features

- **Zero-stutter live Wi-Fi telemetry:** On macOS, `sweetspot` calls `CoreWLAN` directly through cgo with no `sudo` prompt and no background channel scans, so RSSI, noise, SNR, PHY rate, band, channel, and channel width update every second without causing ping spikes. On Linux, it falls back to `iw` and `/proc/net/dev`.
- **Dual-hop latency and jitter:** Streams continuous ICMP pings to your router gateway and to a WAN target (`1.1.1.1` by default) to separate local Wi-Fi contention from ISP issues.
- **On-demand and continuous throughput:** Run an 8-second multi-stream Cloudflare download and upload burst (`b`), toggle continuous walk-around speed probing (`c`), or invoke macOS `networkQuality` (`n`) to measure loaded responsiveness (RPM) and bufferbloat.
- **Spot Finder leaderboard:** Walk around your home or office, press `Space` to pin named spots, compare them side by side with a 0 to 100 SweetSpot score, and export a Markdown report (`e`).
- **RF spectrum scan and DNS racer:** Trigger an explicit nearby channel scan (`s`) to find the cleanest 2.4 GHz, 5 GHz, and 6 GHz channels, and race System DNS against `1.1.1.1`, `9.9.9.9`, `8.8.8.8`, and your router (`r`).
- **Network and cable modem advisor:** Flags concrete bottlenecks such as 40 MHz 5 GHz width caps, crowded channel blocks, uplink bufferbloat, slower DNS resolvers, macOS AirDrop off-channel hops, and DOCSIS cable modem issues (paste a modem status page and press `m`, or pass `--modem`).

## Install

Requires Go 1.23 or newer (and Xcode Command Line Tools on macOS for the `CoreWLAN` bridge).

```sh
git clone https://github.com/bvolpato/sweetspot.git
cd sweetspot
go build -o sweetspot ./cmd/sweetspot
```

Or install directly with Go:

```sh
go install github.com/bvolpato/sweetspot/cmd/sweetspot@latest
```

## Usage

Launch the interactive TUI:

```sh
./sweetspot
```

Start directly in continuous walk-around speed mode:

```sh
./sweetspot --continuous
```

Run a non-interactive snapshot (or JSON output):

```sh
./sweetspot --once --speed --scan
./sweetspot --json --modem cable-stats.txt
```

## Keybindings

| Key | Action |
|---|---|
| `1` - `4` or `Tab` | Switch between Live Radar, Spots, RF and DNS, and Advisor |
| `Space` or `p` | Pin the current location to the Spot Leaderboard |
| `b` | Run an 8-second Cloudflare download, upload, and bufferbloat burst |
| `c` | Toggle continuous walk-around throughput probing |
| `n` | Run macOS `networkQuality` (macOS only) |
| `s` | Scan nearby Wi-Fi channels for RF congestion |
| `r` | Race DNS resolvers (`1.1.1.1`, `9.9.9.9`, `8.8.8.8`, router, system) |
| `m` | Parse DOCSIS cable modem diagnostics from the clipboard |
| `e` | Export a Markdown report (`sweetspot-report-<timestamp>.md`) |
| `q` | Quit |

## License

MIT License. See [LICENSE](LICENSE).
