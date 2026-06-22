# DNSDist HA Agent

Go agent for dnsdist high-availability on FreeBSD via CARP interface control.

## Problem

dnsdist runs on a single FreeBSD host. If dnsdist crashes or the host goes down, DNS resolution stops. Traditional CARP only fails over on **host-level** failure (advertisement timeout). This agent adds **service-level** failover: if dnsdist is unhealthy (process dead, port down, DNS not resolving), the agent brings the CARP VIP interface DOWN, triggering immediate takeover by a healthy peer.

## Architecture

```
┌─────────────────────────────────────────────────────┐
│                   FreeBSD Host                      │
│                                                     │
│   ┌─────────────────────────────────────────┐       │
│   │           dnsdist-ha-agent              │       │
│   │  ┌──────────┐  ┌──────────┐  ┌───────┐ │       │
│   │  │ Health   │→ │ Policy   │→ │ CARP  │ │       │
│   │  │ Checks   │  │ Engine   │  │Control│ │       │
│   │  └──────────┘  └──────────┘  └───┬───┘ │       │
│   │          ↑                       │     │       │
│   │     ┌────┴────┐                  │     │       │
│   │     │  Peer   │◄─────────────────┘     │       │
│   │     │ Heartbeat│                       │       │
│   │     └─────────┘                       │       │
│   └─────────────────────────────────────────┘       │
│                    │           │                     │
│              ┌─────┴────┐ ┌───┴────────────┐        │
│              │ vtnet0   │ │ vtnet1         │        │
│              │ MANAGEMT │ │ VIP/CARP       │        │
│              │ always   │ │ UP/DOWN        │        │
│              │ UP       │ │ by agent       │        │
│              └──────────┘ └────────────────┘        │
└─────────────────────────────────────────────────────┘
```

## How It Works (One Cycle)

```
┌──────────────────────────────────────────────────────────┐
│  Every N seconds (default 5s):                           │
│                                                          │
│  1. Run health checks (process, TCP :53, UDP :53, DNS)   │
│     → weighted score 0-100                               │
│                                                          │
│  2. Read CARP state from ifconfig vtnet1                 │
│     → MASTER / BACKUP / (INIT → UNKNOWN)                 │
│                                                          │
│  3. Query peer(s) via HTTP heartbeat                     │
│     → peer score, CARP state, advskew, demotion          │
│                                                          │
│  4. Evaluate policy (preempt):                           │
│     UNHEALTHY (<40)  → demotion 255, vtnet1 DOWN         │
│     DEGRADED (40-79) → demotion 50,  vtnet1 UP           │
│     HEALTHY (≥80)    → demotion 0,   vtnet1 UP           │
│                                                          │
│  5. Preempt check (if MASTER + healthy peer):            │
│     Compare effective advskew:                           │
│       my_effective < peer_effective → step down          │
│       my_effective ≥ peer_effective → stay MASTER        │
│                                                          │
│  6. Send notification if state changed                   │
│     → CARP state predicted when recovery is deterministic│
│                                                          │
│  7. Log everything with timestamps and [TAG] prefixes    │
└──────────────────────────────────────────────────────────┘
```

## Quick Links

| Document | Description |
|----------|-------------|
| [Architecture](docs/architecture.md) | Full architecture: dual-interface, CARP failover, agent preempt, state machine, demotion, runner cycle, email prediction |
| [Configuration](docs/config.md) | All config fields, VHID, advskew, demotion values, policy modes |
| [Usage](docs/usage.md) | Build, install, verify, multi-node examples, failover scenarios, troubleshooting |

## Key Features

- **Dual-interface** — management (`vtnet0`, always UP) + VIP/CARP (`vtnet1`, controlled by agent)
- **Primary failover** — `ifconfig vtnet1 down` triggers CARP link failure (immediate, <1s)
- **Agent-level preempt** — MASTER steps down via `ifconfig vtnet1 down` when peer has higher priority (replaces unreliable sysctl `net.inet.carp.preempt`)
- **Effective advskew comparison** — compares `configured_advskew + demotion` with peer's `advskew + demotion` via heartbeat
- **Health checks** — process pgrep, TCP :53, UDP :53, DNS query (miekg/dns), weighted scoring
- **Peer protocol** — HTTP with shared secret auth (`X-HA-DDIST-TOKEN`), reports score + CARP state + advskew + demotion
- **Email notification** — SMTP with STARTTLS, anti-spam cooldown, dual-interface layout, CARP/ARP log tail, predicted CARP state
- **Logging** — custom format with timestamp, level, component tag (`[CHECK HEALTH]`, `[PEER]`, `[IFACE]`, `[PREEMPT]`, `[CARP]`, `[STATE]`)
- **Minimal dependencies** — 2 external (`yaml.v3`, `miekg/dns`), rest stdlib

## Prerequisites

- FreeBSD 13.x / 14.x
- **2 physical interfaces**: management + VIP/CARP
- CARP configured on VIP interface (not management)
- Root access

## Quick Start

```bash
# 1. Build
GOOS=freebsd GOARCH=amd64 go build -o build/dnsdist-ha-agent-freebsd-amd64 ./cmd/dnsdist-ha-agent/main.go

# 2. Configure (edit configs/config.yaml and /etc/rc.conf — see docs/usage.md)
# 3. Install
cp build/dnsdist-ha-agent-freebsd-amd64 /usr/local/bin/dnsdist-ha-agent
cp configs/config.yaml /usr/local/etc/dnsdist-ha-agent.yaml
cp scripts/rc.d/dnsdist-ha-agent /usr/local/etc/rc.d/
sysrc dnsdist_ha_agent_enable=YES
service dnsdist-ha-agent start
```

## File Structure

```
.
├── cmd/dnsdist-ha-agent/main.go
├── internal/
│   ├── agent/          runner, state machine, policy
│   ├── config/         config load, parse, validate
│   ├── health/         process, TCP, UDP, DNS checks
│   ├── carp/           CARP state, demotion, VIP, iface control, advskew
│   ├── peer/           HTTP server + heartbeat
│   ├── logger/         custom structured logging (timestamp + level + tag)
│   ├── notify/         email, template, cooldown
│   └── util/           exec, retry helpers
├── configs/config.yaml
├── scripts/            rc.d script, install.sh
├── build/              compiled binary
├── docs/               architecture, config, usage docs
├── README.md
├── go.mod / go.sum
```

## Dependencies

```
require (
    gopkg.in/yaml.v3 v3.0.1
    github.com/miekg/dns v1.1.62
)
```

## Roadmap (Post-MVP)

- Pairwise auth (different secret per pair)
- TLS for peer HTTP
- dnsdist API integration (stats)
- Metrics endpoint (Prometheus)
- Slack/Telegram notification
- Per-VHID demotion via `ifconfig advskew`
