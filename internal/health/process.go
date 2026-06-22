package health

import (
	"time"

	"github.com/anomalyco/dnsdist-ha-agent/internal/util"
)

const processName = "dnsdist"

func CheckProcess(timeout time.Duration) bool {
	result := util.ExecTimeout(timeout, "pgrep", "-x", processName)
	return result.Err == nil
}
