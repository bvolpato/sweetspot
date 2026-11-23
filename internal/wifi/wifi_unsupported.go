//go:build !darwin && !linux && !windows

package wifi

import (
	"context"
	"time"
)

func ReadLink(_ context.Context) (LinkInfo, error) {
	return LinkInfo{Timestamp: time.Now(), Band: BandUnknown}, nil
}

func ScanNeighbors(_ context.Context, current LinkInfo) ScanResult {
	res := SummarizeScan(nil, current, 0)
	res.Err = "RF scanning is supported on macOS, Linux, and Windows"
	return res
}
