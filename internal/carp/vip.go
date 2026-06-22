package carp

import (
	"fmt"
	"net"
	"strings"
	"time"

	"github.com/anomalyco/dnsdist-ha-agent/internal/util"
)

func VIPExists(iface, vip string) (bool, error) {
	result := util.ExecTimeout(5*time.Second, "ifconfig", iface)
	if result.Err != nil {
		return false, fmt.Errorf("ifconfig %s: %w", iface, result.Err)
	}

	ip := net.ParseIP(vip)
	if ip == nil {
		return false, fmt.Errorf("invalid VIP: %s", vip)
	}

	vipStr := ip.String()
	lines := strings.Split(result.Stdout, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if (strings.HasPrefix(line, "inet ") || strings.HasPrefix(line, "inet6 ")) && strings.Contains(line, vipStr) {
			return true, nil
		}
	}

	return false, nil
}
