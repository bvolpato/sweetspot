<p align="center">
  <img src="assets/logo.svg" alt="sweetspot Go Gopher Wi-Fi logo" width="180" />
</p>

<h1 align="center">sweetspot</h1>

`sweetspot` is an interactive terminal dashboard for finding the best Wi-Fi spot in a home or office and diagnosing real network bottlenecks on macOS, Linux, and Windows.

It renders live multi-row Braille charts for signal strength, throughput, and dual-hop latency without triggering off-channel Wi-Fi scan stutters, compares pinned spots side by side, races DNS resolvers, and analyzes DOCSIS cable modem diagnostics.

## Features

- **Cross-platform live Wi-Fi telemetry:** On macOS, `sweetspot` calls `CoreWLAN` directly through cgo and parses `ifconfig -v` for zero-sudo RSSI, noise, SNR, PHY rate, effective throughput estimate, link quality (LQM), band, channel, width, and QoS scheduler state. On Linux, it uses `iw`, `/proc/net/dev`, and `ip route`. On Windows, it uses `netsh wlan`, `route`, `ipconfig`, and `netstat`.
- **Dual-hop latency and jitter:** Streams continuous ICMP pings to your router gateway and to a WAN target (`1.1.1.1` by default) to separate local Wi-Fi contention from ISP issues.
- **On-demand and continuous throughput:** Run an 8-second multi-stream Cloudflare download and upload burst (`b`), toggle continuous walk-around speed probing (`c`), or run macOS `networkQuality` (`n`) to measure loaded responsiveness (RPM) and bufferbloat.
- **Spot Finder leaderboard:** Walk around your home or office, press `Space` to pin named spots, rank them by a 0 to 100 SweetSpot score, and export a Markdown report (`e`).
- **RF spectrum scan and DNS racer:** Trigger an explicit nearby channel scan (`s`) to identify the cleanest 2.4 GHz, 5 GHz, and 6 GHz channels, and race System DNS against `1.1.1.1`, `9.9.9.9`, `8.8.8.8`, and your router (`r`).
- **Network and cable modem advisor:** Flags concrete bottlenecks including 40 MHz 5 GHz width caps, crowded channel blocks, high channel airtime utilization (CCA), 1x1 MIMO drops, uplink bufferbloat, slower DNS resolvers, macOS AirDrop off-channel hops, and DOCSIS cable modem power, SNR, or uncorrectable codeword issues (paste a modem status page and press `m`, or pass `--modem`).

## Install

### Homebrew (macOS and Linux)

```sh
brew tap bvolpato/tap
brew install sweetspot
```

### Go

Requires Go 1.23 or newer (and Xcode Command Line Tools on macOS for the `CoreWLAN` bridge).

```sh
go install github.com/bvolpato/sweetspot/cmd/sweetspot@latest
```

### Prebuilt binaries (macOS, Linux, Windows)

Download the archive for your OS and architecture from the [GitHub Releases](https://github.com/bvolpato/sweetspot/releases) page.

## Usage

Launch the interactive TUI:

```sh
sweetspot
```

Start directly in continuous walk-around speed mode:

```sh
sweetspot --continuous
```

Run a non-interactive snapshot or JSON export:

```sh
sweetspot --once --speed --scan
sweetspot --json --modem cable-stats.txt
```

## Running with `sudo` (Optional)

`sudo` is **not required**. Running `sweetspot` as a normal user gives full live RSSI, noise, SNR, channel, channel width, PHY rate, dual-hop latency, speed bursts, DNS racing, spot pinning, and modem analysis.

If you start `sweetspot` with elevated privileges (`sudo sweetspot`), extra radio telemetry automatically appears (indicated by a `SUDO+` badge in the header):

- **macOS (`sudo sweetspot` via `wdutil info`):**
  - **Unredacted SSID and BSSID:** macOS hides the connected network name and AP MAC address from unprivileged processes without Location Services. With `sudo`, `sweetspot` displays the exact BSSID and counts silent **AP roams** as you walk across a floor.
  - **Channel airtime utilization (CCA %):** Shows how busy the radio channel actually is across all neighboring clients and APs, enabling the Advisor to detect airtime saturation even when RSSI is strong.
  - **MCS index, spatial streams (NSS), and guard interval:** Shows exact modulation (`MCS`) and whether the link is running 2x2 MIMO or dropped to 1x1.
- **Linux (`sudo sweetspot` via `iw`):**
  - Unlocks active `iw dev <iface> scan` neighborhood scanning and `iw dev <iface> survey dump` channel busy time (CCA %) on kernels and distributions that restrict netlink Wi-Fi queries to `CAP_NET_ADMIN`.
- **Windows:**
  - `netsh wlan` works without Administrator privileges. Windows Location Services must be enabled for `netsh` to return SSID and BSSID fields on Windows 11 24H2 and newer.

## Platform Support

| Platform | Live RF Metrics | RF Neighbor Scan | Dual-Hop Ping | Speed & Bufferbloat | Clipboard Modem Import |
|---|---|---|---|---|---|
| **macOS** | `CoreWLAN` (cgo) + `ifconfig -v` (+ optional `sudo wdutil`) | `CoreWLAN` / `system_profiler` | `/sbin/ping` | Cloudflare + `networkQuality` | `pbpaste` |
| **Linux** | `iw link` / `station` / `survey` + `/proc/net/dev` | `iw scan` / `nmcli` | `ping` | Cloudflare | `wl-paste` / `xclip` |
| **Windows** | `netsh wlan show interfaces` + `netstat -e` | `netsh wlan show networks mode=bssid` | `ping` | Cloudflare | PowerShell `Get-Clipboard` |

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
