package util

import (
	"fmt"
	humanize "github.com/dustin/go-humanize"
	"strings"
	"time"
)

func PrintTimeDiff(start int64) string {
	builder := make([]string, 0)
	diff := time.Now().UnixNano() - start

	nano := diff % 1_000_000
	msDiff := diff / 1_000_000
	secDiff := msDiff / 1000
	minDiff := secDiff / 60
	hourDiff := minDiff / 60

	if hourDiff > 0 {
		builder = append(builder, fmt.Sprintf("%sh", humanize.Comma(hourDiff)))
	}
	if minDiff > 0 {
		builder = append(builder, fmt.Sprintf("%dm", minDiff%60))
	}
	if secDiff > 0 {
		builder = append(builder, fmt.Sprintf("%ds", secDiff%60))
	}
	if msDiff > 0 {
		builder = append(builder, fmt.Sprintf("%dms", msDiff%1000))
	}
	if nano > 0 && msDiff == 0 {
		builder = append(builder, fmt.Sprintf("%sμs", humanize.Comma(nano / 1000)))
	}
	return strings.TrimSpace(strings.Join(builder, " "))
}
