package health

import (
	"time"
)

type Weights struct {
	Process int
	TCP     int
	UDP     int
	DNS     int
}

var DefaultWeights = Weights{
	Process: 25,
	TCP:     25,
	UDP:     25,
	DNS:     25,
}

type HealthResult struct {
	Score        int
	ProcessAlive bool
	TCPAlive     bool
	UDPAlive     bool
	DNSAlive     bool
	DNSDetail    DNSResult
	MaxScore     int
}

func RunChecks(cfg ProcessConfig) HealthResult {
	w := DefaultWeights
	if cfg.ProcessWeight > 0 {
		w.Process = cfg.ProcessWeight
	}
	if cfg.TCPWeight > 0 {
		w.TCP = cfg.TCPWeight
	}
	if cfg.UDPWeight > 0 {
		w.UDP = cfg.UDPWeight
	}
	if cfg.DNSWeight > 0 {
		w.DNS = cfg.DNSWeight
	}
	if !cfg.DNSEnabled {
		w.DNS = 0
	}

	timeout := 2 * time.Second
	if cfg.Timeout > 0 {
		timeout = cfg.Timeout
	}

	address := "127.0.0.1:53"
	if cfg.BindAddress != "" {
		address = cfg.BindAddress
	}

	result := HealthResult{
		MaxScore: w.Process + w.TCP + w.UDP + w.DNS,
	}

	result.ProcessAlive = CheckProcess(timeout)
	if result.ProcessAlive {
		result.Score += w.Process
	}

	result.TCPAlive = CheckTCP(address, timeout)
	if result.TCPAlive {
		result.Score += w.TCP
	}

	result.UDPAlive = CheckUDP(address, timeout)
	if result.UDPAlive {
		result.Score += w.UDP
	}

	if cfg.DNSEnabled {
		domain := cfg.DNSDomain
		if domain == "" {
			domain = "google.com"
		}
		result.DNSDetail = CheckDNS(domain, address, timeout)
		result.DNSAlive = result.DNSDetail.Success
		if result.DNSAlive {
			result.Score += w.DNS
		}
	}

	return result
}

type ProcessConfig struct {
	ProcessWeight int
	TCPWeight     int
	UDPWeight     int
	DNSWeight     int
	DNSEnabled    bool
	DNSDomain     string
	BindAddress   string
	Timeout       time.Duration
}
