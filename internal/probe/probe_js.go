package probe

import (
	"context"
	"time"
)

// The browser demo simulates every measurement, so it has no network code.

func RunSpeedBurst(context.Context, time.Duration, time.Duration, int, float64, func() float64, func(SpeedProgress)) SpeedResult {
	return SpeedResult{Err: "not available in the browser"}
}

func RunMacOSNetworkQuality(context.Context) SpeedResult {
	return SpeedResult{Err: "not available in the browser"}
}

func RunDNSRace(context.Context, string) DNSRaceResult { return DNSRaceResult{} }
