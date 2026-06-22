# Usage

## Build

```bash
# Build for FreeBSD amd64
GOOS=freebsd GOARCH=amd64 go build -o build/dnsdist-ha-agent cmd/dnsdist-ha-agent/main.go

# Or use the prebuilt binary
file build/dnsdist-ha-agent-freebsd-amd64

# Test config syntax
GOOS=freebsd GOARCH=amd64 go run ./cmd/dnsdist-ha-agent -t -config configs/config.yaml
```

---

## Installation

### 1. Network Setup — Dual Interface

Ensure the VM/server has **2 interfaces**:

```bash
ifconfig -a | grep -E '^[a-z]' | cut -d: -f1
# vtnet0  ← management
# vtnet1  ← VIP/CARP (added via hypervisor)
```

Physical interface mapping:

| System | Management | VIP/CARP |
|--------|-----------|----------|
| VPS FreeBSD (bhyve) | `vtnet0` | `vtnet1` |
| VMware | `em0` | `em1` |
| Bare metal Intel | `igb0` | `igb1` |

### 2. Configure /etc/rc.conf

```bash
# vtnet0 — management (static IP)
ifconfig_vtnet0="inet 172.16.10.100/25"

# vtnet1 — VIP/CARP
ifconfig_vtnet1="inet 172.16.10.10/25 vhid 1 advbase 1 advskew 0"

# Default route via management interface
defaultrouter="172.16.10.1"
```

**Node B (secondary)** — same but different advskew:

```bash
ifconfig_vtnet1="inet 172.16.10.10/25 vhid 1 advbase 1 advskew 100"
```

**Dual-stack:** add inet6 lines for each VHID (same VHID, same advskew):
```bash
ifconfig_vtnet1_ipv6="inet6  2603:9570:2::100/96 vhid 1 pass SemutMerah advbase 1 advskew 0"
```

### 3. Preempt sysctl (no need to set)

> `net.inet.carp.preempt` is **not used** for MASTER reclaim. Agent-level preempt uses interface down/up — this works without sysctl preempt. Leave it at default (`0`) on all nodes.

### 4. Manual Install

```bash
# 1. Copy binary
cp build/dnsdist-ha-agent-freebsd-amd64 /usr/local/bin/dnsdist-ha-agent
chmod 0555 /usr/local/bin/dnsdist-ha-agent

# 2. Copy config (EDIT PER NODE!)
cp configs/config.yaml /usr/local/etc/dnsdist-ha-agent.yaml
chmod 0640 /usr/local/etc/dnsdist-ha-agent.yaml

# 3. Copy rc.d script
cp scripts/rc.d/dnsdist-ha-agent /usr/local/etc/rc.d/
chmod 0555 /usr/local/etc/rc.d/dnsdist-ha-agent

# 4. Set environment variables (NOT in config file!)
mkdir -p /etc/rc.conf.d
cat > /etc/rc.conf.d/dnsdist-ha-agent << 'EOF'
export HA_TOKEN="RahasiaSuperAman123"
export SMTP_PASS="smtpsecret"
EOF
chmod 0600 /etc/rc.conf.d/dnsdist-ha-agent

# 5. Enable & start
sysrc dnsdist_ha_agent_enable=YES
service dnsdist-ha-agent start
service dnsdist-ha-agent status
```

### 5. Log Rotation (newsyslog)

If `log_file` is set in config, add newsyslog config:

```bash
cat > /etc/newsyslog.conf.d/dnsdist-ha-agent.conf << 'EOF'
/var/log/dnsdist-ha-agent.log   644  7     *    @T00   ZJ
EOF
```

Rotate 7 times, daily at 00:00, compress (Z), no restart signal (J).

### Install Script

```bash
# See docs/config.md for full config reference before installing
sudo ./scripts/install.sh
```

---

## Verification

### Check logs

If `log_file` is configured:

```bash
tail -f /var/log/dnsdist-ha-agent.log
```

If `log_file` is empty (stdout), run in foreground:

```bash
/usr/local/bin/dnsdist-ha-agent -config /usr/local/etc/dnsdist-ha-agent.yaml
```

Example output:

```
level=INFO msg="starting dnsdist-ha-agent" config=/usr/local/etc/dnsdist-ha-agent.yaml
level=INFO msg="runner started" interval=5s interface=vtnet0 vip_interface=vtnet1 vhid=1
level=INFO msg="health check complete" score=100 state=HEALTHY carp=MASTER ...
level=INFO msg="changing demotion" from=0 to=255 reason="unhealthy — demotion 255, vip_iface down"
level=INFO msg="bringing VIP interface down" interface=vtnet1 ...
```

### Check demotion (secondary)

```bash
sysctl net.inet.carp.demotion
# 0   = HEALTHY
# 50  = DEGRADED
# 255 = UNHEALTHY
```

### Check CARP state & VIP interface

```bash
# Check CARP state on VIP interface
ifconfig vtnet1 | grep carp
# carp: MASTER vhid 1
# carp: BACKUP vhid 1

# Check VIP interface link status
ifconfig vtnet1 | head -1
# vtnet1: flags=...<UP,...>  (UP = normal, DOWN = agent triggered failover)
```

### Test peer endpoint

```bash
curl -H "X-HA-DDIST-TOKEN: RahasiaSuperAman123" http://202.138.224.101:8845/health
# {"score":100,"carp_state":"MASTER","timestamp":"2026-06-21T10:00:00Z"}
```

Port is extracted from `peer.port` config. Default HTTP (80) will not work.

---

## Multi-Node Examples

All nodes use **identical config** (`policy: preempt`). Only `bind` (management IP) and `peers` differ per node.

### 2 Nodes

**Node A (202.138.224.101):**
```yaml
peer:
  enabled: true
  bind: "202.138.224.101"    # management IP (unique per node)
  port: ":8845"               # MUST be identical on all nodes
  token: "${HA_TOKEN}"        # MUST be identical on all nodes
  peers:
    - ip: "202.138.224.102"  # peer management IP
      name: "node-b"
policy:
  mode: "preempt"             # MUST be preempt on all nodes
```

**Node B (202.138.224.102):**
```yaml
peer:
  enabled: true
  bind: "202.138.224.102"    # management IP B
  port: ":8845"
  token: "${HA_TOKEN}"
  peers:
    - ip: "202.138.224.101"  # peer management IP
      name: "node-a"
policy:
  mode: "preempt"             # SAME preempt
```

### 3 Nodes

```yaml
# Node A (10.0.0.1)
peer:
  enabled: true
  bind: "10.0.0.1"
  port: ":8845"
  token: "${HA_TOKEN}"
  peers:
    - ip: "10.0.0.2"
      name: "node-b"
    - ip: "10.0.0.3"
      name: "node-c"
policy:
  mode: "preempt"

# Node B (10.0.0.2)
peer:
  enabled: true
  bind: "10.0.0.2"
  port: ":8845"
  token: "${HA_TOKEN}"
  peers:
    - ip: "10.0.0.1"
      name: "node-a"
    - ip: "10.0.0.3"
      name: "node-c"
policy:
  mode: "preempt"
```

---

## Failover Scenarios

### Normal Operation

```
A: vtnet1 UP, dnsdist HEALTHY (score 100)
   → demotion 0, decision: healthy/preempt — demotion 0, vip_iface up
   → CARP MASTER (advskew 0 < 100)

B: vtnet1 UP, dnsdist HEALTHY (score 100)
   → demotion 0, decision: healthy/preempt — demotion 0, vip_iface up
   → CARP BACKUP (advskew 100 > 0)
```

### Failover: dnsdist Crash on A (MASTER)

```
T=0:   A dnsdist crash
       A agent health=UNHEALTHY (score 0)

T=+5s: A agent: decision = "unhealthy — demotion 255, vip_iface down"
       → ifconfig vtnet1 down   (PRIMARY mechanism)
       → sysctl demotion=255    (secondary)
       → CARP detects link down → advertisement STOP

T=+5s: B agent: health=HEALTHY, peer A=UNHEALTHY
       → CARP timeout (A stopped) → B becomes MASTER ✅
       → dnsdist B serves traffic
```

### Restore: dnsdist Recovers on A (Agent Preempt)

```
T=0:   A dnsdist start
       A agent health=HEALTHY (score 100)

T=+5s: A agent: ifconfig vtnet1 up FIRST → kernel subtracts 240
       → sysctl demotion=0 (kernel penalty eliminated)
       → A sees B MASTER → A BACKUP

T=+5s: B agent: policy=preempt, MASTER, peer A HEALTHY
       → **B step down: ifconfig vtnet1 DOWN** (cooldown 60s)

T=+8s: A: CARP timeout (B stopped) → **A becomes MASTER** ✅ (advskew 0)

T=+10s: B agent: health=HEALTHY → ifconfig vtnet1 UP
       → B BACKUP (advskew 100 > 0)

Result: A MASTER back in ~10s after dnsdist recovers.
```

### Node Total Failure (including agent)

```
A total failure (power loss / kernel panic):
  → CARP advertisement stops
  → B detects timeout after ~3s (3 × advbase)
  → B becomes MASTER
  → No agent intervention needed

When A comes back:
  → A boots, dnsdist starts, agent starts
  → A agent: health=HEALTHY, vtnet1 UP
  → A BACKUP (B still MASTER)
  → B agent: policy=preempt, sees A HEALTHY → step down
  → A becomes MASTER (advskew 0)
```

### Summary Table

| Scenario | N1 (PRIMARY, advskew 0) | N2 (SECONDARY, advskew 100) | Result |
|----------|------------------------|----------------------------|--------|
| Both healthy | MASTER | BACKUP | **N1 MASTER** |
| N1 crash | `vtnet1 DOWN` | MASTER | **N2 MASTER** (link down) |
| N1 recovers | **UP** → BACKUP (wait) | MASTER → **step down** → UP | **N1 MASTER** (agent preempt) |
| N2 crash | MASTER | `vtnet1 DOWN` | **N1 MASTER** |
| N2 recovers | MASTER | **UP** → BACKUP | **N1 stays MASTER** |
| Both crash | `DOWN` | `DOWN` | **No MASTER** |
| N1 total failure | — | MASTER | **N2 MASTER** (timeout) |

---

## Email Notification

Trigger: on every **state change** (HEALTHY↔DEGRADED↔UNHEALTHY).

**Anti-spam cooldown:** After sending an email for a transition (e.g. `HEALTHY→UNHEALTHY`), the same transition will NOT trigger another email until `notify.cooldown` has elapsed (default `5m`).

Config:

```yaml
notify:
  email:
    enabled: true
    smtp_host: "mail.example.com"
    smtp_port: 587
    username: "agent"
    password: "${SMTP_PASS}"
    from: "dnsdist-ha@example.com"
    to:
      - "admin@example.com"
  cooldown: 5m   # Go duration: 5s, 1m, 10m, 30m, 1h
```

Email format:

```
Subject: DNSDist HA: HEALTHY → UNHEALTHY on gw1-dnsdist-bdg

DNSDist HA Agent — State Change

Timestamp: 2026-06-21T19:28:59+07:00
Hostname:  gw1-dnsdist-bdg
Interface: vtnet0

Node IP:   202.138.224.101
VIP:       202.138.224.100
CARP:      BACKUP

State:     UNHEALTHY
Previous:  HEALTHY

Checks:
  Process:  ✓
  TCP :53:  ✗
  UDP :53:  ✗
  DNS:      ✗
  Score:    25/100
  Demotion: 255

Reason: Failed: TCP :53 not responding, UDP :53 not responding, DNS query failed.

This is an automated notification from dnsdist-ha-agent.
```

---

## PF Firewall

If using PF, allow peer traffic between cluster nodes:

```pf
ext_if = "vtnet0"
cluster_ports = "{ 8845 }"
cluster_peers = "{ 202.138.224.101 202.138.224.102 }"

pass in quick on $ext_if proto tcp from $cluster_peers to any port $cluster_ports
pass out quick on $ext_if proto tcp to $cluster_peers port $cluster_ports
block in log on $ext_if proto tcp to any port $cluster_ports
```

Reload:

```bash
pfctl -f /etc/pf.conf
pfctl -s rules | grep 8845
```

---

## Troubleshooting

| Symptom | Check |
|---------|-------|
| Agent can't set demotion or iface up/down | Must be root: `whoami` |
| Peer connection refused | `sockstat -4 -p 8845`, check PF rules |
| VIP interface not visible | `ifconfig -a \| grep vtnet1`, check `vip_interface` in config |
| CARP state not readable | `ifconfig vtnet1 \| grep -i carp`, ensure interface name is correct |
| Agent crash when interface down | Normal — agent handles gracefully via management interface. Ensure `bind_address: "127.0.0.1:53"` |
| rc.d status inaccurate | `cat /var/run/dnsdist_ha_agent.pid` to verify pidfile |

### Test config

```bash
/usr/local/bin/dnsdist-ha-agent -config /usr/local/etc/dnsdist-ha-agent.yaml
# Agent will print error and exit if config is invalid
```

---

## Uninstall

```bash
service dnsdist-ha-agent stop
sysrc dnsdist_ha_agent_enable=NO
rm /usr/local/etc/rc.d/dnsdist-ha-agent
rm /usr/local/bin/dnsdist-ha-agent
rm /usr/local/etc/dnsdist-ha-agent.yaml
rm /etc/rc.conf.d/dnsdist-ha-agent
```
