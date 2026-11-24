package wifi

import (
	"testing"
	"time"
)

func TestSummarizeScanPicksCleanestChannels(t *testing.T) {
	neighbors := []NeighborAP{
		// Heavy crowding on UNII-1 (Ch 36-48) and UNII-3 (Ch 149-161)
		{Band: Band5GHz, Channel: 36, WidthMHz: 80, RSSI: -45},
		{Band: Band5GHz, Channel: 36, WidthMHz: 80, RSSI: -52},
		{Band: Band5GHz, Channel: 40, WidthMHz: 80, RSSI: -60},
		{Band: Band5GHz, Channel: 44, WidthMHz: 80, RSSI: -62},
		{Band: Band5GHz, Channel: 157, WidthMHz: 80, RSSI: -50},
		{Band: Band5GHz, Channel: 157, WidthMHz: 80, RSSI: -55},
		{Band: Band5GHz, Channel: 153, WidthMHz: 80, RSSI: -58},
		{Band: Band5GHz, Channel: 149, WidthMHz: 80, RSSI: -61},
		{Band: Band5GHz, Channel: 161, WidthMHz: 80, RSSI: -64},
		// 6 GHz present
		{Band: Band6GHz, Channel: 133, WidthMHz: 160, RSSI: -58},
		// 2.4 GHz crowded on 1 and 6, empty on 11
		{Band: Band2GHz, Channel: 1, WidthMHz: 20, RSSI: -55},
		{Band: Band2GHz, Channel: 1, WidthMHz: 20, RSSI: -60},
		{Band: Band2GHz, Channel: 6, WidthMHz: 20, RSSI: -50},
	}
	cur := LinkInfo{Connected: true, Band: Band5GHz, Channel: 36, WidthMHz: 40, RSSI: -41}
	res := SummarizeScan(neighbors, cur, 1200*time.Millisecond)

	if !res.Has6GHzNearby {
		t.Fatal("expected Has6GHzNearby == true")
	}
	if res.CurrentOverlapAPs != 4 {
		t.Fatalf("expected 4 overlapping APs on UNII-1, got %d", res.CurrentOverlapAPs)
	}
	if res.Best2GHzChannel != 11 {
		t.Fatalf("expected cleanest 2.4 GHz channel 11, got %d", res.Best2GHzChannel)
	}
	// Non-DFS: UNII-1 has 4 APs (Ch 48 has 0 primary), UNII-3 has 5 APs -> expect Ch 48
	if res.Best5GHzNonDFS != 48 {
		t.Fatalf("expected cleanest non-DFS 5 GHz channel 48, got %d", res.Best5GHzNonDFS)
	}
	// Overall 5 GHz should pick an empty DFS channel (52..144)
	if res.Best5GHzChannel < 52 || res.Best5GHzChannel > 144 {
		t.Fatalf("expected empty DFS 5 GHz channel, got %d", res.Best5GHzChannel)
	}
}
