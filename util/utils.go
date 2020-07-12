package util

import (
	"fmt"
	"time"
)

func PrintTimeDiff(start int64) string {
	//builder := strings.Builder{}
	diff := time.Now().UnixNano() - start

	return fmt.Sprintf("%d", diff)

}
