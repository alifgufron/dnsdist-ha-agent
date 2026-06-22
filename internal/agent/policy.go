package agent

import (
	"github.com/anomalyco/dnsdist-ha-agent/internal/carp"
	"github.com/anomalyco/dnsdist-ha-agent/internal/peer"
)

type PolicyMode int

const (
	PolicySticky  PolicyMode = iota
	PolicyPreempt
)

type PolicyDecision struct {
	DesiredDemotion  int
	DesiredIfaceDown bool
	Action           string
}

func EvaluatePolicy(mode PolicyMode, score int, carpState carp.State, peerHealths []peer.PeerHealth) PolicyDecision {
	state := StateFromScore(score)

	if state == StateUnhealthy {
		return PolicyDecision{
			DesiredDemotion:  255,
			DesiredIfaceDown: true,
			Action:           "unhealthy — demotion 255, vip_iface down",
		}
	}

	if state == StateDegraded {
		return PolicyDecision{
			DesiredDemotion:  50,
			DesiredIfaceDown: false,
			Action:           "degraded — demotion 50, vip_iface up",
		}
	}

	// HEALTHY state
	switch mode {
	case PolicySticky:
		return PolicyDecision{
			DesiredDemotion:  0,
			DesiredIfaceDown: false,
			Action:           "healthy/sticky — demotion 0, vip_iface up",
		}

	case PolicyPreempt:
		hasHealthierPeer := false
		for _, ph := range peerHealths {
			if ph.OK && ph.Score > score {
				hasHealthierPeer = true
				break
			}
		}

		if hasHealthierPeer && carpState == carp.StateMaster {
			return PolicyDecision{
				DesiredDemotion:  50,
				DesiredIfaceDown: false,
				Action:           "preempt — healthier peer exists, stepping down",
			}
		}

		return PolicyDecision{
			DesiredDemotion:  0,
			DesiredIfaceDown: false,
			Action:           "healthy/preempt — demotion 0, vip_iface up",
		}
	}

	return PolicyDecision{
		DesiredDemotion:  0,
		DesiredIfaceDown: false,
		Action:           "default — demotion 0, vip_iface up",
	}
}

func ParsePolicyMode(mode string) PolicyMode {
	switch mode {
	case "preempt":
		return PolicyPreempt
	default:
		return PolicySticky
	}
}
