package agent

import (
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/anomalyco/dnsdist-ha-agent/internal/carp"
	"github.com/anomalyco/dnsdist-ha-agent/internal/config"
	"github.com/anomalyco/dnsdist-ha-agent/internal/health"
	"github.com/anomalyco/dnsdist-ha-agent/internal/notify"
	"github.com/anomalyco/dnsdist-ha-agent/internal/peer"
)

type Runner struct {
	cfg    *config.Config
	log    *slog.Logger
	done   chan struct{}
	wg     sync.WaitGroup

	lastHealth    health.HealthResult
	lastState     State
	lastDemotion  int
	lastIfaceDown bool

	preemptCooldown time.Time
	stepDownPending bool

	peerSrv  *peer.HeartbeatServer
	notifier *notify.EventDispatcher
	stateMu  sync.RWMutex
}

func NewRunner(cfg *config.Config, log *slog.Logger) *Runner {
	r := &Runner{
		cfg:           cfg,
		log:           log,
		done:          make(chan struct{}),
		lastState:     StateHealthy,
		lastDemotion:  -1,
		lastIfaceDown: false,
	}

	healthFn := func() health.HealthResult {
		r.stateMu.RLock()
		defer r.stateMu.RUnlock()
		return r.lastHealth
	}

	r.peerSrv = peer.NewHeartbeatServer(
		cfg.Peer.Token,
		cfg.Agent.VIPInterface,
		cfg.Agent.VHID,
		healthFn,
		log,
	)

	note := notify.NewEventDispatcher(log, cfg.Notify.Cooldown)
	if cfg.Notify.Email.Enabled {
		emailNotifier := notify.NewEmailNotifier(
			cfg.Notify.Email.SMTPHost,
			cfg.Notify.Email.SMTPPort,
			cfg.Notify.Email.Username,
			cfg.Notify.Email.Password,
			cfg.Notify.Email.From,
			cfg.Notify.Email.To,
		)
		note.AddNotifier(emailNotifier)
	}
	r.notifier = note

	return r
}

func (r *Runner) Run() error {
	if r.cfg.Peer.Enabled {
		r.wg.Add(1)
		go func() {
			defer r.wg.Done()
			srv := &httpServer{
				addr:    r.cfg.Peer.ListenAddr(),
				handler: r.peerSrv.Handler(),
				log:     r.log,
			}
			r.log.Info("[PEER] starting peer heartbeat server", "bind", r.cfg.Peer.Bind)
			if err := srv.ListenAndServe(); err != nil {
				r.log.Error("[PEER] heartbeat server error", "error", err)
			}
		}()
	}

	r.log.Info("[AGENT] runner started",
		"interval", r.cfg.Agent.Interval,
		"mgmt_iface", r.cfg.Agent.Interface,
		"vip_iface", r.cfg.Agent.VIPInterface,
		"vhid", r.cfg.Agent.VHID,
	)

	ticker := time.NewTicker(r.cfg.Agent.Interval)
	defer ticker.Stop()

	r.runOnce()

	for {
		select {
		case <-r.done:
			r.log.Info("[AGENT] runner stopped")
			return nil
		case <-ticker.C:
			r.runOnce()
		}
	}
}

func (r *Runner) Stop() {
	close(r.done)
	r.wg.Wait()
}

func (r *Runner) runOnce() {
	healthCfg := health.ProcessConfig{
		DNSEnabled:  r.cfg.Health.DNSQuery.Enabled,
		DNSDomain:   r.cfg.Health.DNSQuery.Domain,
		BindAddress: r.cfg.Health.BindAddress,
		Timeout:     r.cfg.Health.DNSQuery.Timeout,
	}
	if !r.cfg.Health.ProcessCheck {
		healthCfg.ProcessWeight = 0
	}
	if !r.cfg.Health.TCPCheck {
		healthCfg.TCPWeight = 0
	}
	if !r.cfg.Health.UDPCheck {
		healthCfg.UDPWeight = 0
	}

	healthResult := health.RunChecks(healthCfg)

	r.stateMu.Lock()
	r.lastHealth = healthResult
	r.stateMu.Unlock()

	currentState := StateFromScore(healthResult.Score)
	stateChanged := currentState.Transitioned(r.lastState)

	carpState, err := carp.ReadState(r.cfg.Agent.VIPInterface, r.cfg.Agent.VHID)
	if err != nil {
		r.log.Warn("[CARP] failed to read CARP state", "error", err)
		carpState = carp.StateUnknown
	}

	var peerHealths []peer.PeerHealth
	if r.cfg.Peer.Enabled {
		entries := make([]peer.PeerEntry, 0, len(r.cfg.Peer.Peers))
		for _, p := range r.cfg.Peer.Peers {
			entries = append(entries, peer.PeerEntry{IP: p.IP, Name: p.Name})
		}
		peerPort := r.cfg.Peer.PortNum()
		peerHealths = peer.CheckAllPeers(entries, r.cfg.Peer.Token, peerPort, 3*time.Second)
	}

	policyMode := ParsePolicyMode(r.cfg.Policy.Mode)
	policyState := ParsePolicyState(r.cfg.Policy.State)
	decision := EvaluatePolicy(policyMode, policyState, healthResult.Score, carpState, peerHealths)

	vipIface := r.cfg.Agent.VIPInterface

	// Step 1: Interface up/down with correct demotion ordering.
	//
	// FreeBSD CARP kernel automatically adds 240 to demotion when the
	// VIP interface goes DOWN, and subtracts 240 when it comes UP.
	// To reach the desired final demotion:
	//   - Going DOWN: write demotion FIRST, then bring interface down
	//     (kernel adds 240 on top → total = desired + 240)
	//   - Going UP:   bring interface UP FIRST (kernel subtracts 240
	//     from current value), THEN write the final desired demotion
	//   - Demotion-only: write demotion as usual
	if decision.DesiredIfaceDown && !r.lastIfaceDown {
		if decision.DesiredDemotion != r.lastDemotion {
			r.log.Info("[STATE] changing demotion",
				"from", r.lastDemotion,
				"to", decision.DesiredDemotion,
				"reason", decision.Action,
			)
			if err := carp.SetDemotion(decision.DesiredDemotion); err != nil {
				r.log.Error("[STATE] failed to set demotion", "error", err)
			}
		}
		r.log.Info("[IFACE] bringing VIP interface down",
			"interface", vipIface,
			"reason", decision.Action,
		)
		if err := carp.InterfaceDown(vipIface); err != nil {
			r.log.Error("[IFACE] failed to bring VIP interface down", "interface", vipIface, "error", err)
		}
		r.lastIfaceDown = true
		r.lastDemotion = decision.DesiredDemotion
		r.stepDownPending = false
	} else if !decision.DesiredIfaceDown && r.lastIfaceDown {
		r.log.Info("[IFACE] bringing VIP interface up",
			"interface", vipIface,
			"reason", decision.Action,
		)
		if err := carp.InterfaceUp(vipIface); err != nil {
			r.log.Error("[IFACE] failed to bring VIP interface up", "interface", vipIface, "error", err)
		}
		if decision.DesiredDemotion != r.lastDemotion {
			r.log.Info("[STATE] changing demotion",
				"from", r.lastDemotion,
				"to", decision.DesiredDemotion,
				"reason", decision.Action,
			)
			if err := carp.SetDemotion(decision.DesiredDemotion); err != nil {
				r.log.Error("[STATE] failed to set demotion", "error", err)
			}
		}
		r.lastIfaceDown = false
		r.lastDemotion = decision.DesiredDemotion
		r.stepDownPending = false
	} else if decision.DesiredDemotion != r.lastDemotion {
		r.log.Info("[STATE] changing demotion",
			"from", r.lastDemotion,
			"to", decision.DesiredDemotion,
			"reason", decision.Action,
		)
		if err := carp.SetDemotion(decision.DesiredDemotion); err != nil {
			r.log.Error("[STATE] failed to set demotion", "error", err)
		}
		r.lastDemotion = decision.DesiredDemotion
	}

	// Read actual demotion from sysctl for accurate preempt/notification calculations
	actualDemotion := decision.DesiredDemotion
	if demotion, err := carp.GetDemotion(); err == nil {
		actualDemotion = demotion
	}

	// Step 2: Preempt step-down
	//   - state=master: never step down (intended MASTER)
	//   - state=backup: step down if ANY healthy peer exists
	//   - state=auto:   compare effective advskew — only step down if peer has strictly lower
	if policyMode == PolicyPreempt && !decision.DesiredIfaceDown && carpState == carp.StateMaster && currentState == StateHealthy {
		if policyState == PolicyStateMaster {
			// Intended MASTER — never step down
		} else if policyState == PolicyStateBackup {
			r.log.Info("[PREEMPT] stepping down — backup policy, yielding to peer",
				"interface", vipIface,
			)
			if err := carp.InterfaceDown(vipIface); err != nil {
				r.log.Error("[PREEMPT] failed to bring VIP interface down", "interface", vipIface, "error", err)
			}
			r.lastIfaceDown = true
			r.preemptCooldown = time.Now()
		} else {
			// PolicyStateAuto — compare effective advskew
			localAdvskew, _ := carp.GetAdvskew(vipIface, r.cfg.Agent.VHID)
			for _, ph := range peerHealths {
				if !ph.OK || ph.Score < 80 {
					continue
				}
				peerEffective := ph.Advskew + ph.Demotion
				myEffective := localAdvskew + actualDemotion
				if peerEffective < myEffective && time.Since(r.preemptCooldown) > 60*time.Second {
					r.log.Info("[PREEMPT] stepping down — peer has higher priority",
						"interface", vipIface,
						"peer", ph.Name,
						"peer_effective", peerEffective,
						"my_effective", myEffective,
					)
					if err := carp.InterfaceDown(vipIface); err != nil {
						r.log.Error("[PREEMPT] failed to bring VIP interface down", "interface", vipIface, "error", err)
					}
					r.lastIfaceDown = true
					r.preemptCooldown = time.Now()
					break
				}
			}
		}
	}

	// Step 3: Refresh CARP state after interface changes for accurate notification
	notificationCarp := carpState
	if stateChanged || r.lastIfaceDown != decision.DesiredIfaceDown {
		newCarpState, err := carp.ReadState(r.cfg.Agent.VIPInterface, r.cfg.Agent.VHID)
		if err == nil {
			carpState = newCarpState
		}
	}
	notificationCarp = carpState

	// Step 4: Predict final CARP state for notification
	//   - state=master: always predict MASTER (intended MASTER)
	//   - state=backup: never predict MASTER (intended BACKUP)
	//   - state=auto: compare effective advskew — predict MASTER if no peer has higher priority
	if currentState == StateHealthy && notificationCarp == carp.StateBackup {
		switch policyState {
		case PolicyStateMaster:
			notificationCarp = carp.StateMaster
		case PolicyStateBackup:
			// Stay BACKUP — no prediction needed
		default:
			// PolicyStateAuto — predict based on effective advskew
			localAdvskew, err := carp.GetAdvskew(r.cfg.Agent.VIPInterface, r.cfg.Agent.VHID)
			if err == nil {
				myEffective := localAdvskew + actualDemotion
				peerHasHigherPriority := false
				for _, ph := range peerHealths {
					if ph.OK && ph.Score >= 80 {
						peerEffective := ph.Advskew + ph.Demotion
						if peerEffective < myEffective {
							peerHasHigherPriority = true
							break
						}
					}
				}
				if !peerHasHigherPriority {
					notificationCarp = carp.StateMaster
				}
			}
		}
	}

	logCarp := carpState.String()
	if notificationCarp != carpState {
		logCarp = notificationCarp.String() + " (predicted)"
	}
	r.log.Info("[CHECK HEALTH] health check complete",
		"score", healthResult.Score,
		"max_score", healthResult.MaxScore,
		"state", currentState.String(),
		"carp", logCarp,
		"demotion", actualDemotion,
		"process", healthResult.ProcessAlive,
		"tcp", healthResult.TCPAlive,
		"udp", healthResult.UDPAlive,
		"dns", healthResult.DNSAlive,
		"decision", decision.Action,
	)

	for _, ph := range peerHealths {
		if ph.OK {
			r.log.Info("[PEER] peer status",
				"peer", ph.Name,
				"score", ph.Score,
				"state", ph.State,
				"carp", ph.Carp,
			)
		} else {
			r.log.Warn("[PEER] peer unreachable",
				"peer", ph.Name,
				"error", ph.Error,
			)
		}
	}

	if stateChanged {
		nodeIP, _ := carp.GetNodeIP(r.cfg.Agent.Interface)
		vip, _ := carp.GetVIP(r.cfg.Agent.VIPInterface, r.cfg.Agent.VHID)
		r.notifier.Dispatch(
			currentState.String(),
			r.lastState.String(),
			healthResult.Score,
			decision.DesiredDemotion,
			nodeIP,
			vip,
			notificationCarp.String(),
			r.cfg.Agent.Interface,
			r.cfg.Agent.VIPInterface,
			healthResult.ProcessAlive,
			healthResult.TCPAlive,
			healthResult.UDPAlive,
			healthResult.DNSAlive,
		)
		r.lastState = currentState
	}
}

type httpServer struct {
	addr    string
	handler http.Handler
	log     *slog.Logger
}

func (s *httpServer) ListenAndServe() error {
	srv := &http.Server{
		Addr:    s.addr,
		Handler: s.handler,
	}
	return srv.ListenAndServe()
}


