package peer

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/anomalyco/dnsdist-ha-agent/internal/carp"
	"github.com/anomalyco/dnsdist-ha-agent/internal/health"
)

type HeartbeatServer struct {
	token    string
	vipIface string
	vhid     int
	healthFn func() health.HealthResult
	mu       sync.RWMutex
	log      *slog.Logger
}

func NewHeartbeatServer(token, vipIface string, vhid int, healthFn func() health.HealthResult, log *slog.Logger) *HeartbeatServer {
	return &HeartbeatServer{
		token:    token,
		vipIface: vipIface,
		vhid:     vhid,
		healthFn: healthFn,
		log:      log,
	}
}

func (s *HeartbeatServer) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/health", s.handleHealth)
	return s.authMiddleware(mux)
}

func (s *HeartbeatServer) authMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token := r.Header.Get("X-HA-DDIST-TOKEN")
		if token == "" || token != s.token {
			s.log.Warn("unauthorized peer request", "remote", r.RemoteAddr)
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *HeartbeatServer) handleHealth(w http.ResponseWriter, r *http.Request) {
	h := s.healthFn()

	cs, err := carp.ReadState(s.vipIface, s.vhid)
	carpState := "UNKNOWN"
	if err == nil {
		carpState = cs.String()
	}

	advskew, _ := carp.GetAdvskew(s.vipIface, s.vhid)
	demotion, _ := carp.GetDemotion()

	resp := struct {
		Score     int    `json:"score"`
		CarpState string `json:"carp_state"`
		Advskew   int    `json:"advskew"`
		Demotion  int    `json:"demotion"`
		Timestamp string `json:"timestamp"`
	}{
		Score:     h.Score,
		CarpState: carpState,
		Advskew:   advskew,
		Demotion:  demotion,
		Timestamp: time.Now().UTC().Format(time.RFC3339),
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}
