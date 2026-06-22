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

Supports dual-stack (IPv4 + IPv6) VIPs on the same VHID. The agent detects both address families and shows all VIPs in email notifications.

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
│     → detects both IPv4 and IPv6 VIPs                    │
│                                                          │
│  3. Query peer(s) via HTTP heartbeat                     │
│     → peer score, CARP state, advskew, demotion          │
│                                                          │
│  4. Evaluate policy (preempt) + policy.state:            │
│     UNHEALTHY (<40)  → demotion 255, vtnet1 DOWN         │
│     DEGRADED (40-79) → demotion 50,  vtnet1 UP           │
│     HEALTHY (≥80)    → demotion 0,   vtnet1 UP           │
│     policy.state: auto/master/backup                      │
│                                                          │
│  5. Apply demotion + interface (ordering fixed):         │
│     → UP:   iface UP first, then sysctl demotion         │
│     → DOWN: sysctl demotion first, then iface DOWN       │
│                                                          │
│  6. Preempt check (if MASTER + healthy peer):            │
│     state=auto:  compare effective advskew               │
│     state=master: NEVER step down                        │
│     state=backup: ALWAYS step down if peer healthy       │
│                                                          │
│  7. Predict CARP state for email                         │
│     state=master → always predicts MASTER when healthy   │
│     state=backup → never predicts MASTER                 │
│                                                          │
│  8. Send notification if state changed                   │
│                                                          │
│  9. Log everything with timestamps and [TAG] prefixes    │
└──────────────────────────────────────────────────────────┘
```

## Quick Links

| Document | Description |
|----------|-------------|
| [Architecture](docs/architecture.md) | Full architecture: dual-interface, CARP failover, agent preempt, state machine, demotion ordering, runner cycle, policy.state, dual-stack |
| [Configuration](docs/config.md) | All config fields, VHID, advskew, demotion values, policy modes, policy.state, dual-stack |
| [Usage](docs/usage.md) | Build, install, verify, multi-node examples, failover scenarios, troubleshooting |

## Key Features

- **Dual-interface** — management (`vtnet0`, always UP) + VIP/CARP (`vtnet1`, controlled by agent)
- **Dual-stack** — detects both IPv4 and IPv6 VIPs for the same VHID, shows both in email
- **Primary failover** — `ifconfig vtnet1 down` triggers CARP link failure (immediate, <1s)
- **Agent-level preempt** — MASTER steps down via `ifconfig vtnet1 down` when peer has higher priority (replaces unreliable sysctl `net.inet.carp.preempt`)
- **policy.state** — `auto` (compare advskew), `master` (never step down, always reclaim), `backup` (yield to any healthy peer)
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
```

Node A (PRIMARY, `policy.state=master`) — `/etc/rc.conf`:
```
ifconfig_vtnet0="inet 202.138.224.101 netmask 255.255.255.128"
ifconfig_vtnet1="inet 202.138.224.100/25 vhid 1 pass SemutMerah advbase 1 advskew 0"
ifconfig_vtnet1_ipv6="inet6 2403:9500:2::100/96 vhid 1 pass SemutMerah advbase 1 advskew 0"
```

Node B (SECONDARY, `policy.state=backup`) — `/etc/rc.conf`:
```
ifconfig_vtnet0="inet 202.138.224.102 netmask 255.255.255.128"
ifconfig_vtnet1="inet 202.138.224.100/25 vhid 1 pass SemutMerah advbase 1 advskew 100"
ifconfig_vtnet1_ipv6="inet6 2403:9500:2::100/96 vhid 1 pass SemutMerah advbase 1 advskew 100"
```

`/usr/local/etc/dnsdist-ha-agent.yaml` — Node A:
```yaml
policy:
  mode: "preempt"
  state: "master"
```

Node B:
```yaml
policy:
  mode: "preempt"
  state: "backup"
```

```bash
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
