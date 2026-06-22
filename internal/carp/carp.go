package carp

import (
	"fmt"
	"net"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/anomalyco/dnsdist-ha-agent/internal/util"
)

type State int

const (
	StateUnknown State = iota
	StateMaster
	StateBackup
)

func (s State) String() string {
	switch s {
	case StateMaster:
		return "MASTER"
	case StateBackup:
		return "BACKUP"
	default:
		return "UNKNOWN"
	}
}

var carpStateRe = regexp.MustCompile(`carp:\s+(MASTER|BACKUP)`)
var vhidVIP4Re = regexp.MustCompile(`inet\s+(\S+)\s+.*vhid\s+(\d+)`)
var vhidVIP6Re = regexp.MustCompile(`inet6\s+(\S+)\s+.*vhid\s+(\d+)`)
var inetRe = regexp.MustCompile(`inet\s+(\S+)`)
var advskewRe = regexp.MustCompile(`carp:.*vhid\s+(\d+).*advskew\s+(\d+)`)

func ReadState(iface string, vhid int) (State, error) {
	result := util.ExecTimeout(5*time.Second, "ifconfig", iface)
	if result.Err != nil {
		return StateUnknown, fmt.Errorf("ifconfig %s: %w (stderr: %s)", iface, result.Err, result.Stderr)
	}

	matches := carpStateRe.FindStringSubmatch(result.Stdout)
	if len(matches) < 2 {
		return StateUnknown, fmt.Errorf("no carp state found on interface %s", iface)
	}

	switch matches[1] {
	case "MASTER":
		return StateMaster, nil
	case "BACKUP":
		return StateBackup, nil
	default:
		return StateUnknown, fmt.Errorf("unknown carp state: %s", matches[1])
	}
}

func GetNodeIP(iface string) (string, error) {
	result := util.ExecTimeout(5*time.Second, "ifconfig", iface)
	if result.Err != nil {
		return "", fmt.Errorf("ifconfig %s: %w", iface, result.Err)
	}

	lines := strings.Split(result.Stdout, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if !strings.Contains(line, "vhid") {
			matches := inetRe.FindStringSubmatch(line)
			if matches != nil {
				ip := net.ParseIP(matches[1])
				if ip != nil {
					return ip.String(), nil
				}
			}
		}
	}

	return "", fmt.Errorf("no non-VIP IP found on %s", iface)
}

func GetVIP(iface string, vhid int) (string, error) {
	result := util.ExecTimeout(5*time.Second, "ifconfig", iface)
	if result.Err != nil {
		return "", fmt.Errorf("ifconfig %s: %w", iface, result.Err)
	}

	vhidStr := fmt.Sprintf("%d", vhid)
	var vips []string
	lines := strings.Split(result.Stdout, "\n")

	for _, line := range lines {
		line = strings.TrimSpace(line)
		matches := vhidVIP4Re.FindStringSubmatch(line)
		if matches != nil && matches[2] == vhidStr {
			if ip := net.ParseIP(matches[1]); ip != nil {
				vips = append(vips, ip.String())
			}
		}
		matches = vhidVIP6Re.FindStringSubmatch(line)
		if matches != nil && matches[2] == vhidStr {
			if ip := net.ParseIP(matches[1]); ip != nil {
				vips = append(vips, ip.String())
			}
		}
	}

	if len(vips) == 0 {
		return "", fmt.Errorf("no VIP found for vhid %d on %s", vhid, iface)
	}

	return strings.Join(vips, ", "), nil
}

func GetAdvskew(iface string, vhid int) (int, error) {
	result := util.ExecTimeout(5*time.Second, "ifconfig", iface)
	if result.Err != nil {
		return 0, fmt.Errorf("ifconfig %s: %w", iface, result.Err)
	}

	lines := strings.Split(result.Stdout, "\n")
	for _, line := range lines {
		matches := advskewRe.FindStringSubmatch(line)
		if matches != nil && matches[1] == fmt.Sprintf("%d", vhid) {
			skew, err := strconv.Atoi(matches[2])
			if err != nil {
				return 0, fmt.Errorf("parse advskew: %w", err)
			}
			return skew, nil
		}
	}

	return 0, fmt.Errorf("no advskew found for vhid %d on %s", vhid, iface)
}
