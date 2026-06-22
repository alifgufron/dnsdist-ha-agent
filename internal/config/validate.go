package config

import (
	"errors"
	"fmt"
	"net"
	"strings"
)

func Validate(cfg *Config) error {
	var errs []error

	if cfg.Agent.Interval <= 0 {
		errs = append(errs, errors.New("agent.interval must be positive"))
	}
	if cfg.Agent.Interface == "" {
		errs = append(errs, errors.New("agent.interface is required"))
	}
	if cfg.Agent.VHID <= 0 {
		errs = append(errs, errors.New("agent.vhid must be positive"))
	}
	if cfg.Agent.VIPInterface == "" {
		errs = append(errs, errors.New("agent.vip_interface is required"))
	}

	if cfg.Health.DNSQuery.Enabled {
		if cfg.Health.DNSQuery.Domain == "" {
			errs = append(errs, errors.New("health.dns_query.domain is required when enabled"))
		}
	}

	if cfg.Health.BindAddress != "" {
		_, _, err := net.SplitHostPort(cfg.Health.BindAddress)
		if err != nil {
			errs = append(errs, fmt.Errorf("health.bind_address %q is not valid host:port", cfg.Health.BindAddress))
		}
	}

	if cfg.CARP.DemotionHealthy < 0 || cfg.CARP.DemotionHealthy > 255 {
		errs = append(errs, fmt.Errorf("carp.demotion_healthy must be 0-255, got %d", cfg.CARP.DemotionHealthy))
	}
	if cfg.CARP.DemotionDegraded < 0 || cfg.CARP.DemotionDegraded > 255 {
		errs = append(errs, fmt.Errorf("carp.demotion_degraded must be 0-255, got %d", cfg.CARP.DemotionDegraded))
	}
	if cfg.CARP.DemotionUnhealthy < 0 || cfg.CARP.DemotionUnhealthy > 255 {
		errs = append(errs, fmt.Errorf("carp.demotion_unhealthy must be 0-255, got %d", cfg.CARP.DemotionUnhealthy))
	}
	if cfg.CARP.DemotionUnhealthy <= cfg.CARP.DemotionDegraded {
		errs = append(errs, fmt.Errorf("carp.demotion_unhealthy (%d) must be > demotion_degraded (%d)", cfg.CARP.DemotionUnhealthy, cfg.CARP.DemotionDegraded))
	}
	if cfg.CARP.DemotionDegraded < cfg.CARP.DemotionHealthy {
		errs = append(errs, fmt.Errorf("carp.demotion_degraded (%d) must be >= demotion_healthy (%d)", cfg.CARP.DemotionDegraded, cfg.CARP.DemotionHealthy))
	}

	if cfg.Peer.Enabled {
		if cfg.Peer.Token == "" {
			errs = append(errs, errors.New("peer.token is required when enabled"))
		}

		if cfg.Peer.Bind == "" {
			errs = append(errs, errors.New("peer.bind is required when enabled"))
		} else if net.ParseIP(cfg.Peer.Bind) == nil {
			errs = append(errs, fmt.Errorf("peer.bind %q is not a valid IP address", cfg.Peer.Bind))
		}

		if cfg.Peer.Port == "" {
			errs = append(errs, errors.New("peer.port is required when enabled"))
		} else if !strings.HasPrefix(cfg.Peer.Port, ":") {
			errs = append(errs, fmt.Errorf("peer.port %q must start with ':' (e.g. \":8845\")", cfg.Peer.Port))
		}

		// port and token must be same on all nodes (documentation note, validated per-node)

		for i, p := range cfg.Peer.Peers {
			if net.ParseIP(p.IP) == nil {
				errs = append(errs, fmt.Errorf("peer.peers[%d].ip %q is not a valid IP", i, p.IP))
			}
			if p.Name == "" {
				errs = append(errs, fmt.Errorf("peer.peers[%d].name is required", i))
			}
		}
	}

	if cfg.Policy.Mode != "" && cfg.Policy.Mode != "sticky" && cfg.Policy.Mode != "preempt" {
		errs = append(errs, fmt.Errorf("policy.mode must be 'sticky' or 'preempt', got %q", cfg.Policy.Mode))
	}

	if cfg.Policy.State != "" && cfg.Policy.State != "auto" && cfg.Policy.State != "master" && cfg.Policy.State != "backup" {
		errs = append(errs, fmt.Errorf("policy.state must be 'auto', 'master', or 'backup', got %q", cfg.Policy.State))
	}

	if cfg.Notify.Email.Enabled {
		if cfg.Notify.Email.SMTPHost == "" {
			errs = append(errs, errors.New("notify.email.smtp_host is required when enabled"))
		}
		if cfg.Notify.Email.From == "" {
			errs = append(errs, errors.New("notify.email.from is required when enabled"))
		}
		if len(cfg.Notify.Email.To) == 0 {
			errs = append(errs, errors.New("notify.email.to must have at least one recipient"))
		}
	}

	if cfg.Notify.Cooldown <= 0 {
		errs = append(errs, errors.New("notify.cooldown must be positive"))
	}

	return errors.Join(errs...)
}
