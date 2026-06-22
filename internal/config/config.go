package config

import (
	"fmt"
	"os"
	"regexp"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Agent   AgentConfig   `yaml:"agent"`
	Health  HealthConfig  `yaml:"health"`
	CARP    CARPConfig    `yaml:"carp"`
	Peer    PeerConfig    `yaml:"peer"`
	Policy  PolicyConfig  `yaml:"policy"`
	Notify  NotifyConfig  `yaml:"notify"`
	LogFile string        `yaml:"log_file"`
}

type AgentConfig struct {
	Interval      time.Duration `yaml:"interval"`
	Interface     string        `yaml:"interface"`
	VIPInterface  string        `yaml:"vip_interface"`
	VHID          int           `yaml:"vhid"`
}

type HealthConfig struct {
	ProcessCheck bool        `yaml:"process_check"`
	TCPCheck     bool        `yaml:"tcp_check"`
	UDPCheck     bool        `yaml:"udp_check"`
	DNSQuery     DNSQuery    `yaml:"dns_query"`
	BindAddress  string      `yaml:"bind_address"`
}

type DNSQuery struct {
	Enabled bool          `yaml:"enabled"`
	Domain  string        `yaml:"domain"`
	Timeout time.Duration `yaml:"timeout"`
}

type CARPConfig struct {
	DemotionHealthy   int `yaml:"demotion_healthy"`
	DemotionDegraded  int `yaml:"demotion_degraded"`
	DemotionUnhealthy int `yaml:"demotion_unhealthy"`
}

type PeerConfig struct {
	Enabled bool        `yaml:"enabled"`
	Bind    string      `yaml:"bind"`
	Port    string      `yaml:"port"`
	Token   string      `yaml:"token"`
	Peers   []PeerEntry `yaml:"peers"`
}

// ListenAddr returns bind IP + port (e.g. "10.0.0.1:8080") for HTTP server
func (p PeerConfig) ListenAddr() string {
	return p.Bind + p.Port
}

// PortNum returns just the port number (e.g. "8080") for peer client URLs
func (p PeerConfig) PortNum() string {
	return strings.TrimPrefix(p.Port, ":")
}

type PeerEntry struct {
	IP   string `yaml:"ip"`
	Name string `yaml:"name"`
}

type PolicyConfig struct {
	Mode string `yaml:"mode"`
}

type NotifyConfig struct {
	Email    EmailConfig   `yaml:"email"`
	Cooldown time.Duration `yaml:"cooldown"`
}

type EmailConfig struct {
	Enabled  bool     `yaml:"enabled"`
	SMTPHost string   `yaml:"smtp_host"`
	SMTPPort int      `yaml:"smtp_port"`
	Username string   `yaml:"username"`
	Password string   `yaml:"password"`
	From     string   `yaml:"from"`
	To       []string `yaml:"to"`
}

var envPattern = regexp.MustCompile(`\$\{([^}]+)\}`)

var knownKeys = map[string]map[string]bool{
	"agent": {"interval": true, "interface": true, "vip_interface": true, "vhid": true},
	"log_file": {},
	"health": {"process_check": true, "tcp_check": true, "udp_check": true, "dns_query": true, "bind_address": true},
	"carp":   {"demotion_healthy": true, "demotion_degraded": true, "demotion_unhealthy": true},
	"peer":   {"enabled": true, "bind": true, "port": true, "token": true, "peers": true},
	"policy": {"mode": true},
	"notify": {"email": true, "cooldown": true},
}

var nestedKeys = map[string]map[string]bool{
	"dns_query":     {"enabled": true, "domain": true, "timeout": true},
	"email":         {"enabled": true, "smtp_host": true, "smtp_port": true, "username": true, "password": true, "from": true, "to": true},
	"peer.peers":    {"ip": true, "name": true},
}

func CheckConfig(path string) []string {
	data, err := os.ReadFile(path)
	if err != nil {
		return []string{fmt.Sprintf("read config: %v", err)}
	}

	var rawNode yaml.Node
	if err := yaml.Unmarshal(data, &rawNode); err != nil {
		return []string{fmt.Sprintf("YAML syntax error: %v", err)}
	}

	var errs []string
	errs = append(errs, checkUnknownKeys(&rawNode, "")...)

	// Always try to parse + validate to collect ALL errors
	data = expandEnv(data)

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		errs = append(errs, fmt.Sprintf("parse config: %v", err))
		return errs
	}

	if err := Validate(&cfg); err != nil {
		for _, e := range strings.Split(err.Error(), "\n") {
			if e != "" {
				errs = append(errs, e)
			}
		}
	}

	if len(errs) > 0 {
		return errs
	}
	return nil
}

func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}

	var rawNode yaml.Node
	if err := yaml.Unmarshal(data, &rawNode); err != nil {
		return nil, fmt.Errorf("YAML syntax error: %v", err)
	}

	errs := checkUnknownKeys(&rawNode, "")
	if len(errs) > 0 {
		return nil, fmt.Errorf("%s", strings.Join(errs, "\n"))
	}

	data = expandEnv(data)

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}

	if err := Validate(&cfg); err != nil {
		return nil, fmt.Errorf("%s", err.Error())
	}

	return &cfg, nil
}

func checkUnknownKeys(node *yaml.Node, prefix string) []string {
	var errs []string
	// DocumentNode wraps the root MappingNode
	if node.Kind == yaml.DocumentNode && len(node.Content) > 0 {
		return checkUnknownKeys(node.Content[0], prefix)
	}
	if node.Kind != yaml.MappingNode {
		return nil
	}

	for i := 0; i < len(node.Content)-1; i += 2 {
		keyNode := node.Content[i]
		valNode := node.Content[i+1]
		key := keyNode.Value

		switch prefix {
		case "":
			if _, ok := knownKeys[key]; !ok {
				errs = append(errs, fmt.Sprintf("  line %d: unknown top-level key %q — valid: agent, health, carp, peer, policy, notify, log_file", keyNode.Line, key))
			} else if valNode.Kind == yaml.MappingNode {
				errs = append(errs, checkUnknownKeys(valNode, key)...)
			}

		case "agent":
			if _, ok := knownKeys["agent"][key]; !ok {
				errs = append(errs, fmt.Sprintf("  line %d: unknown key %q under 'agent:' — valid: interval, interface, vip_interface, vhid", keyNode.Line, key))
			}

		case "health":
			if _, ok := knownKeys["health"][key]; !ok {
				errs = append(errs, fmt.Sprintf("  line %d: unknown key %q under 'health:' — valid: process_check, tcp_check, udp_check, dns_query, bind_address", keyNode.Line, key))
			} else if key == "dns_query" && valNode.Kind == yaml.MappingNode {
				errs = append(errs, checkMappingKeys(valNode, "dns_query")...)
			}

		case "carp":
			if _, ok := knownKeys["carp"][key]; !ok {
				errs = append(errs, fmt.Sprintf("  line %d: unknown key %q under 'carp:' — valid: demotion_healthy, demotion_degraded, demotion_unhealthy", keyNode.Line, key))
			}

		case "peer":
			if _, ok := knownKeys["peer"][key]; !ok {
				errs = append(errs, fmt.Sprintf("  line %d: unknown key %q under 'peer:' — valid: enabled, bind, port, token, peers", keyNode.Line, key))
			} else if key == "peers" && valNode.Kind == yaml.SequenceNode {
				errs = append(errs, checkPeerEntries(valNode)...)
			}

		case "policy":
			if _, ok := knownKeys["policy"][key]; !ok {
				errs = append(errs, fmt.Sprintf("  line %d: unknown key %q under 'policy:' — valid: mode", keyNode.Line, key))
			}

		case "notify":
			if _, ok := knownKeys["notify"][key]; !ok {
				errs = append(errs, fmt.Sprintf("  line %d: unknown key %q under 'notify:' — valid: email, cooldown", keyNode.Line, key))
			} else if key == "email" && valNode.Kind == yaml.MappingNode {
				errs = append(errs, checkMappingKeys(valNode, "email")...)
			}
		}
	}

	return errs
}

func checkMappingKeys(node *yaml.Node, context string) []string {
	var errs []string
	if node.Kind != yaml.MappingNode {
		return nil
	}

	valid, ok := nestedKeys[context]
	if !ok {
		return nil
	}

	for i := 0; i < len(node.Content)-1; i += 2 {
		key := node.Content[i].Value
		if !valid[key] {
			var validList []string
			for k := range valid {
				validList = append(validList, k)
			}
			errs = append(errs, fmt.Sprintf("  line %d: unknown key %q under '%s:' — valid: %s",
				node.Content[i].Line, key, context, strings.Join(validList, ", ")))
		}
	}
	return errs
}

func checkPeerEntries(node *yaml.Node) []string {
	var errs []string
	if node.Kind != yaml.SequenceNode {
		return nil
	}
	for _, item := range node.Content {
		if item.Kind == yaml.MappingNode {
			errs = append(errs, checkMappingKeys(item, "peer.peers")...)
		}
	}
	return errs
}

func expandEnv(data []byte) []byte {
	return envPattern.ReplaceAllFunc(data, func(match []byte) []byte {
		envVar := string(match[2 : len(match)-1])
		val := os.Getenv(envVar)
		if val == "" {
			return match
		}
		return []byte(val)
	})
}
