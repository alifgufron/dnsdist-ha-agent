package peer

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

func peerStateFromScore(score int) string {
	if score >= 80 {
		return "HEALTHY"
	}
	if score >= 40 {
		return "DEGRADED"
	}
	return "UNHEALTHY"
}

type PeerEntry struct {
	IP   string
	Name string
}

type PeerHealth struct {
	Name    string    `json:"name"`
	IP      string    `json:"ip"`
	Score   int       `json:"score"`
	State   string    `json:"state"`
	Carp    string    `json:"carp_state"`
	Advskew int       `json:"advskew"`
	Demotion int      `json:"demotion"`
	OK      bool      `json:"ok"`
	Error   string    `json:"error,omitempty"`
	Updated time.Time `json:"updated"`
}

func CheckPeer(ip, name, token, port string, timeout time.Duration) PeerHealth {
	ph := PeerHealth{
		Name:    name,
		IP:      ip,
		Updated: time.Now(),
	}

	client := &http.Client{Timeout: timeout}

	u := fmt.Sprintf("http://%s/health", ip)
	if port != "" {
		u = fmt.Sprintf("http://%s:%s/health", ip, port)
	}

	req, err := http.NewRequest("GET", u, nil)
	if err != nil {
		ph.Error = fmt.Sprintf("create request: %v", err)
		return ph
	}
	req.Header.Set("X-HA-DDIST-TOKEN", token)

	resp, err := client.Do(req)
	if err != nil {
		ph.Error = fmt.Sprintf("connection failed: %v", err)
		return ph
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		ph.Error = fmt.Sprintf("HTTP %d: %s", resp.StatusCode, string(body))
		return ph
	}

	var peerResp struct {
		Score     int    `json:"score"`
		CarpState string `json:"carp_state"`
		Advskew   int    `json:"advskew"`
		Demotion  int    `json:"demotion"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&peerResp); err != nil {
		ph.Error = fmt.Sprintf("decode response: %v", err)
		return ph
	}

	ph.Score = peerResp.Score
	ph.Carp = peerResp.CarpState
	ph.Advskew = peerResp.Advskew
	ph.Demotion = peerResp.Demotion
	ph.State = peerStateFromScore(peerResp.Score)
	ph.OK = true

	return ph
}

func CheckAllPeers(peers []PeerEntry, token, port string, timeout time.Duration) []PeerHealth {
	results := make([]PeerHealth, 0, len(peers))
	for _, p := range peers {
		ph := CheckPeer(p.IP, p.Name, token, port, timeout)
		results = append(results, ph)
	}
	return results
}
