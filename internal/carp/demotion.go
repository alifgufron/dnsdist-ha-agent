package carp

import (
	"fmt"
	"strconv"

	"github.com/anomalyco/dnsdist-ha-agent/internal/util"
)

func SetDemotion(level int) error {
	result := util.Exec("sysctl", fmt.Sprintf("net.inet.carp.demotion=%d", level))
	if result.Err != nil {
		return fmt.Errorf("set demotion %d: %w (stderr: %s)", level, result.Err, result.Stderr)
	}
	return nil
}

func GetDemotion() (int, error) {
	result := util.Exec("sysctl", "-n", "net.inet.carp.demotion")
	if result.Err != nil {
		return 0, fmt.Errorf("get demotion: %w (stderr: %s)", result.Err, result.Stderr)
	}
	return strconv.Atoi(result.Stdout)
}
