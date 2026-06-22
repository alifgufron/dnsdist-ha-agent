# Configuration

## Example

```yaml
log_file: /var/log/dnsdist-ha-agent.log

agent:
  interval: 5s
  interface: "vtnet0"          # management — for node IP and peer comms
  vip_interface: "vtnet1"      # VIP/CARP — controlled up/down to trigger failover
  vhid: 1

health:
  process_check: true
  tcp_check: true
  udp_check: true
  dns_query:
    enabled: true
    domain: "google.com"
    timeout: 2s
  bind_address: "127.0.0.1:53" # change if dnsdist listens on a public IP

carp:
  demotion_healthy: 0
  demotion_degraded: 50
  demotion_unhealthy: 255

peer:
  enabled: true
  bind: "10.0.0.1"                  # management IP — listen on this IP
  port: ":8845"                     # port — MUST be identical on all nodes
  token: "${HA_TOKEN}"              # token — MUST be identical on all nodes
  peers:
    - ip: "10.0.0.2"
      name: "node-b"

policy:
  mode: "preempt"                   # "preempt" | "sticky". MUST be "preempt" on all nodes.

notify:
  email:
    enabled: true
    smtp_host: "mail.example.com"
    smtp_port: 587
    username: "notif"
    password: "${SMTP_PASS}"
    from: "dnsdist-ha@example.com"
    to:
      - "admin@example.com"
  cooldown: 5m
```

---

## Field Reference

| Field | Description |
|-------|-------------|
| `log_file` | Optional. Path to log file. If empty, logs to stdout. If set, appends to file. Example: `/var/log/dnsdist-ha-agent.log` |
| `agent.interval` | Health check interval (5s, 10s, 30s) |
| `agent.interface` | Management interface — for reading node IP, peer communication. Always UP. Typically `vtnet0` |
| `agent.vip_interface` | VIP/CARP interface — controlled by agent: **UP** when healthy, **DOWN** when unhealthy. Triggers CARP failover. Typically `vtnet1` |
| `agent.vhid` | CARP VHID, must match `/etc/rc.conf` |
| `health.process_check` | Check if dnsdist process is running (via pgrep) |
| `health.tcp_check` | Check TCP port :53 connectivity |
| `health.udp_check` | Check UDP port :53 via DNS query |
| `health.dns_query` | DNS query check (enabled, domain, timeout) |
| `health.bind_address` | Address:port where dnsdist listens (default `127.0.0.1:53`). Change if dnsdist only listens on public IP |
| `carp.demotion_healthy` | Demotion value when HEALTHY (default 0) |
| `carp.demotion_degraded` | Demotion value when DEGRADED (default 50) |
| `carp.demotion_unhealthy` | Demotion value when UNHEALTHY (default 255) |
| `peer.enabled` | Enable peer communication |
| `peer.bind` | IP address for HTTP server listen (e.g. `"10.0.0.1"`). Must be this node's own management IP. **Differs per node** |
| `peer.port` | Port for HTTP server, format `:PORT` (e.g. `":8845"`). **MUST be identical on all nodes** |
| `peer.token` | Shared secret for authentication. Can be literal string or `${ENV_VAR}`. **MUST be identical on all nodes** |
| `peer.peers` | List of other nodes. Each entry: `ip` (management IP) and `name` (optional label) |
| `policy.mode` | `"preempt"` or `"sticky"`. **MUST be `"preempt"`** on all nodes. Only `"preempt"` supports MASTER reclaim via agent-level preempt. `"sticky"` never steps down |
| `notify.email.*` | SMTP configuration |
| `notify.email.smtp_host` | SMTP server hostname |
| `notify.email.smtp_port` | SMTP server port (587 for STARTTLS, 465 for SSL) |
| `notify.email.username` | SMTP username |
| `notify.email.password` | SMTP password (supports `${ENV_VAR}` expansion) |
| `notify.email.from` | Sender email address |
| `notify.email.to` | List of recipient email addresses |
| `notify.cooldown` | Minimum interval between notifications |

---

## Token (Shared Secret) & Port

**`peer.token` and `peer.port` MUST be identical on all nodes.**

Store secrets in `/etc/rc.conf.d/dnsdist-ha-agent` — **NOT** in config file:

```bash
# /etc/rc.conf.d/dnsdist-ha-agent
export HA_TOKEN="RahasiaSuperAman123"
export SMTP_PASS="smtpsecret"
chmod 0600 /etc/rc.conf.d/dnsdist-ha-agent
```

Config file uses `${HA_TOKEN}` which gets expanded at runtime by the agent.

---

## VHID (Virtual Host ID)

VHID is the **CARP group number**. Each CARP group = one virtual IP (VIP). Nodes in the same VHID compete for MASTER of that VIP.

Multi-group topology example:

```
VHID 1 — 202.138.224.100/25 (dnsdist)
  ├── Node A: advskew 0  → MASTER
  └── Node B: advskew 100 → BACKUP

VHID 2 — 202.138.224.200/25 (web)
  ├── Node A: advskew 100 → BACKUP
  └── Node B: advskew 0  → MASTER
```

In `/etc/rc.conf`, each VHID is one `ifconfig_xxx_alias<N>` entry:

```bash
# Node A (PRIMARY)
ifconfig_vtnet0="inet 172.16.10.100/25"                               # management
ifconfig_vtnet1="inet 172.16.10.10/25 vhid 1 advbase 1 advskew 0"     # VIP

# Node B (SECONDARY)
ifconfig_vtnet0="inet 172.16.10.101/25"                               # management
ifconfig_vtnet1="inet 172.16.10.10/25 vhid 1 advbase 1 advskew 100"   # VIP
```

Multi-VHID — add aliases on `vtnet1`:

```bash
# Node A — two VHIDs on vtnet1
ifconfig_vtnet1_alias0="inet 172.16.10.10/25 vhid 1 advbase 1 advskew 0"
ifconfig_vtnet1_alias1="inet 172.16.10.20/25 vhid 2 advbase 1 advskew 100"
```

Dual-stack — IPv6 VIP is added as an alias on the same interface with the
same VHID:

```bash
ifconfig_vtnet1_alias0="inet 172.16.10.10/25 vhid 1 advbase 1 advskew 0"
ifconfig_vtnet1_ipv6="inet6 2603:9570:2::100/96 vhid 1 advbase 1 advskew 0"
```

CARP state, failover, and demotion work identically for IPv4 and IPv6.

**VHID in config (`vhid: 1`) must match `/etc/rc.conf`.** The agent only uses VHID for logging.

---

## advskew (Advertisement Skew)

advskew is the static priority per VHID set in `/etc/rc.conf`. Lower value = higher priority to become MASTER.

```
advskew 0   → highest priority (default MASTER)
advskew 100 → lower priority
advskew 254 → lowest possible (can still be MASTER)
```

advskew is added to `advbase` (default 1s) to calculate advertisement interval:

```
advertisement interval = advbase + (advskew / 256) seconds

advskew 0   → interval 1.00s
advskew 100 → interval 1.39s
advskew 254 → interval 1.99s
```

This enables **load balancing** for multi-group setups — each group can prefer a different node.

---

## Demotion Values

**MUST be 0-255. Identical on all nodes.**

```yaml
carp:
  demotion_healthy: 0      # normal
  demotion_degraded: 50    # advskew increases by 50
  demotion_unhealthy: 255  # advskew increases by 255 (cannot become MASTER)
```

> **Note:** Demotion is the **secondary** mechanism. The **primary** failover mechanism is `ifconfig <vip_iface> down` which triggers CARP link failure. Demotion is still set for compatibility and supplementary information, but cannot be relied upon to trigger takeover.

Values outside 0-255 (e.g. `550`, `999`) are **rejected** by the agent.

Effective advskew calculation:

```
effective advskew = configured advskew + global demotion

effective ≤ 254 → eligible for MASTER
effective ≥ 255 → cannot become MASTER
```

Example:

| Node | advskew (rc.conf) | demotion (agent) | effective | Can be MASTER? | Priority |
|------|------------------|------------------|-----------|---------------|----------|
| A | 0 | 0 | 0 | ✅ | Highest |
| B | 100 | 0 | 100 | ✅ | Lower than A |
| A (dnsdist crash) | 0 | 255 | 255 | ❌ (≥ 255) | Cannot |
| B (dnsdist crash) | 100 | 255 | 355 | ❌ (≥ 255) | Cannot |

---

## Policy Modes

| Mode | Behavior | Use case |
|------|----------|----------|
| `preempt` | MASTER steps down (via agent) when peer is healthy | **REQUIRED.** Enables MASTER reclaim after recovery |
| `sticky` | MASTER stays MASTER regardless of peer health | Not recommended. PRIMARY cannot reclaim MASTER after failover |

**All nodes MUST use `preempt`.** This is the only mode where a recovered PRIMARY node can reclaim MASTER from a SECONDARY.

The agent-level preempt works by having the current MASTER bring its `vip_interface` DOWN when it detects a healthy peer with higher priority (lower advskew). This causes CARP failover back to the PRIMARY.


