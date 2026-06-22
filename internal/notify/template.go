package notify

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"text/template"
	"time"
)

const defaultTemplate = `DNSDist HA Agent — State Change

Timestamp: {{.Timestamp}}
Hostname:  {{.Hostname}}

── Management Interface ({{.MgmtInterface}}) ──
Node IP:   {{.NodeIP}}

── VIP/CARP Interface ({{.VIPInterface}}) ──
VIP:       {{.VIP}}
CARP:      {{.CarpState}}

State:     {{.NewState}}
Previous:  {{.OldState}}

Checks:
  Process:  {{.ProcessOK}}
  TCP :53:  {{.TCPOK}}
  UDP :53:  {{.UDPOK}}
  DNS:      {{.DNSOK}}
  Score:    {{.Score}}/100
  Demotion: {{.Demotion}}

Reason: {{.Reason}}

Recent CARP/ARP messages from /var/log/messages:
{{.CarpArpLog}}

This is an automated notification from dnsdist-ha-agent.
`

type NotificationData struct {
	Timestamp    string
	Hostname     string
	MgmtInterface string
	VIPInterface string
	NodeIP       string
	VIP          string
	CarpState    string
	CarpArpLog   string
	NewState     string
	OldState     string
	Score        int
	Demotion     int
	Reason       string
	ProcessOK    string
	TCPOK        string
	UDPOK        string
	DNSOK        string
}

func checkSymbol(ok bool) string {
	if ok {
		return "✓"
	}
	return "✗"
}

func generateReason(processOK, tcpOK, udpOK, dnsOK bool) string {
	var failed []string
	if !processOK {
		failed = append(failed, "dnsdist process not running")
	}
	if !tcpOK {
		failed = append(failed, "TCP :53 not responding")
	}
	if !udpOK {
		failed = append(failed, "UDP :53 not responding")
	}
	if !dnsOK {
		failed = append(failed, "DNS query failed")
	}

	if len(failed) == 0 {
		return "all checks passed"
	}
	return "Failed: " + strings.Join(failed, ", ") + "."
}

func getCarpArpLog() string {
	cmd := exec.Command("sh", "-c", "grep -E 'carp:|arp:' /var/log/messages 2>/dev/null | tail -10")
	out, err := cmd.Output()
	if err != nil || len(out) == 0 {
		return "  (no recent CARP/ARP messages)"
	}
	return "  " + strings.TrimSpace(strings.ReplaceAll(string(out), "\n", "\n  "))
}

func RenderNotification(newState, oldState string, score, demotion int, nodeIP, vip, carpState, mgmtIface, vipIface string, processOK, tcpOK, udpOK, dnsOK bool) (string, string) {
	hostname, _ := os.Hostname()

	data := NotificationData{
		Timestamp:    time.Now().Format(time.RFC3339),
		Hostname:     hostname,
		MgmtInterface: mgmtIface,
		VIPInterface: vipIface,
		NodeIP:       nodeIP,
		VIP:          vip,
		CarpState:    carpState,
		CarpArpLog:   getCarpArpLog(),
		NewState:     newState,
		OldState:     oldState,
		Score:        score,
		Demotion:     demotion,
		Reason:       generateReason(processOK, tcpOK, udpOK, dnsOK),
		ProcessOK:    checkSymbol(processOK),
		TCPOK:        checkSymbol(tcpOK),
		UDPOK:        checkSymbol(udpOK),
		DNSOK:        checkSymbol(dnsOK),
	}

	tmpl, err := template.New("notification").Parse(defaultTemplate)
	if err != nil {
		return fmt.Sprintf("DNSDist HA: %s → %s", oldState, newState),
			fmt.Sprintf("State changed from %s to %s (score: %d)", oldState, newState, score)
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return fmt.Sprintf("DNSDist HA: %s → %s", oldState, newState),
			fmt.Sprintf("State changed from %s to %s (score: %d)", oldState, newState, score)
	}

	subject := fmt.Sprintf("DNSDist HA: %s → %s on %s", oldState, newState, hostname)

	return subject, buf.String()
}
