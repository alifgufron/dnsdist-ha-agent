# Architecture

## Dual-Interface Model

Each node requires **2 physical interfaces**:

```
┌──────────────────────────────────┐
│          FreeBSD Host            │
│                                  │
│  ┌───────────────────────────┐   │
│  │    dnsdist-ha-agent        │   │
│  │    (Go binary)             │   │
│  └──────┬──────────┬──────────┘   │
│         │          │              │
│  ┌──────┴────┐ ┌───┴────────┐    │
│  │ vtnet0    │ │ vtnet1     │    │
│  │ MANAGEMENT│ │ VIP/CARP   │    │
│  │           │ │            │    │
│  │ ● Peer    │ │ ● CARP VIP │    │
│  │ ● SSH     │ │ ● dnsdist  │    │
│  │ ● Agent   │ │ ● UP/DOWN  │    │
│  │   always  │ │   by agent │    │
│  │   UP      │ │            │    │
│  └───────────┘ └────────────┘    │
└──────────────────────────────────┘
```

| Interface | Config field | Role | Status |
|-----------|-------------|------|--------|
| `vtnet0` | `interface` | Management: node IP, peer HTTP, SSH, monitoring | **Always UP** |
| `vtnet1` | `vip_interface` | VIP/CARP: carries the virtual dnsdist IP | **UP/DOWN by agent** |

**Why 2 interfaces?** When agent brings `vip_interface` DOWN to trigger failover, the management interface stays UP so the agent can still communicate with peers and accept health checks via loopback (`127.0.0.1:53`).

---

## CARP Failover Mechanism

### Primary: Interface Down

Agent runs `ifconfig <vip_iface> down` when dnsdist is UNHEALTHY. This triggers a **link state change** on the VIP/CARP interface — the FreeBSD CARP kernel detects the interface went down and the BACKUP node takes over immediately (no advertisement timeout wait).

```
HEALTHY:   vip_iface UP   → CARP MASTER (or BACKUP, per advskew)
UNHEALTHY: vip_iface DOWN → CARP MASTER → INIT (link down)
                            peer BACKUP → MASTER (immediate takeover)
RECOVERED: vip_iface UP   → CARP INIT → BACKUP (initialization complete)
                            then → MASTER (if peer steps down via preempt)
                            or   → BACKUP (if peer has higher priority)
```

### CARP State Transitions

```
                  ┌─────────┐
                  │  INIT   │ ← interface DOWN, or just came UP
                  └────┬────┘
                       │ initialization complete
                       ▼
                  ┌─────────┐
            ┌────→│ BACKUP  │ ← listening for peer advertisements
            │     └────┬────┘
            │          │ master timed out
            │          ▼
            │     ┌─────────┐
            │     │ MASTER  │ ← advertising as VIP owner
            │     └────┬────┘
            │          │ interface DOWN (by agent or link loss)
            │          ▼
            │     ┌─────────┐
            └─────│  INIT   │ ← back to init, lose VIP
                  └─────────┘
```

- **INIT** → Occurs when interface is down or just came up. No CARP running yet.
- **BACKUP** → Node sees a peer MASTER advertising. Waits for timeout.
- **MASTER** → Node is the active VIP owner. Sends advertisements.
- **Timeout** → 3 × advbase (default 3 seconds). If MASTER stops advertising, BACKUP transitions to MASTER.

### Secondary: Demotion (sysctl)

`sysctl net.inet.carp.demotion` is set as supplementary information. Demotion adds to the effective advskew but does **not** trigger CARP state transition on its own — the CARP state machine only reacts to advertisement timeout, preempt trigger, or link state change, not demotion changes.

| | advskew | demotion |
|--|---------|----------|
| Set in | `/etc/rc.conf` (static) | `sysctl net.inet.carp.demotion` (dynamic by agent) |
| Scope | Per VHID | Global (all VHIDs) |
| Range | 0-254 | 0-255 |
| Change time | Reboot / manual | Every agent interval (5s) |

**Formula:**
```
effective advskew = configured advskew + global demotion

effective ≤ 254 → eligible for MASTER
effective ≥ 255 → will never become MASTER
lower effective  → higher priority (more likely to be MASTER)
```

**Example:**

| Node | advskew (rc.conf) | demotion (agent) | effective | Can be MASTER? | Priority |
|------|------------------|------------------|-----------|---------------|----------|
| A | 0 | 0 | 0 | ✅ | Highest |
| B | 100 | 0 | 100 | ✅ | Lower than A |
| A (dnsdist crash) | 0 | 255 | 255 | ❌ (≥ 255) | Cannot |
| B (dnsdist crash) | 100 | 255 | 355 | ❌ (≥ 255) | Cannot |

---

## Runner Cycle (Every Interval)

The agent's main loop (`runOnce()`) executes the following steps in order every N seconds:

```
┌──────────────────────────────────────────────────────────────┐
│                     ONE RUNNER CYCLE                         │
├──────────────────────────────────────────────────────────────┤
│                                                              │
│  ┌──────────────┐                                            │
│  │ Health Check │ → process alive, TCP :53, UDP :53, DNS    │
│  │              │ → weighted score 0-100 (default 25 each)  │
│  └──────┬───────┘                                            │
│         ▼                                                    │
│  ┌──────────────┐                                            │
│  │ Read CARP    │ → ifconfig <vip_iface>                     │
│  │ State        │ → MASTER / BACKUP / UNKNOWN                │
│  └──────┬───────┘                                            │
│         ▼                                                    │
│  ┌──────────────┐                                            │
│  │ Peer Check   │ → HTTP GET /health to each peer            │
│  │              │ → score, carp_state, advskew, demotion     │
│  └──────┬───────┘                                            │
│         ▼                                                    │
│  ┌──────────────┐                                            │
│  │ Policy       │ → UNHEALTHY → demotion 255, iface DOWN     │
│  │ Evaluate     │ → DEGRADED  → demotion 50,  iface UP      │
│  │              │ → HEALTHY   → demotion 0,   iface UP      │
│  │              │ → Preempt: compare effective advskew       │
│  └──────┬───────┘                                            │
│         ▼                                                    │
│  ┌──────────────┐                                            │
│  │ Apply        │ → sysctl net.inet.carp.demotion = N       │
│  │ Demotion     │                                            │
│  └──────┬───────┘                                            │
│         ▼                                                    │
│  ┌──────────────┐                                            │
│  │ Interface    │ → ifconfig <vip_iface> up/down             │
│  │ Control      │ ← PRIMARY mechanism                       │
│  └──────┬───────┘                                            │
│         ▼                                                    │
│  ┌──────────────┐                                            │
│  │ Preempt      │ → if MASTER + peer healthy:                │
│  │ Step-down    │   compare peer_effective < my_effective →  │
│  │              │   ifconfig <vip_iface> down                │
│  └──────┬───────┘                                            │
│         ▼                                                    │
│  ┌──────────────┐                                            │
│  │ Predict CARP │ → if BACKUP + our effective < peer          │
│  │ for Email    │   → predict MASTER (deterministic)         │
│  └──────┬───────┘                                            │
│         ▼                                                    │
│  ┌──────────────┐                                            │
│  │ Notify       │ → if state changed: send email              │
│  │ (optional)   │   → includes predicted CARP state          │
│  │              │   → includes /var/log/messages CARP/ARP    │
│  └──────────────┘                                            │
│                                                              │
└──────────────────────────────────────────────────────────────┘
```

---

## State Machine

```
HEALTHY (score >= 80)      → demotion 0,  vip_iface UP
    │
    │ score drops below 80
    ▼
DEGRADED (score >= 40)     → demotion 50,  vip_iface UP
    │
    │ score drops below 40  │ score recovers above 80
    ▼                        │
UNHEALTHY (score < 40) ─────┘ → demotion 255, vip_iface DOWN
                               (triggers CARP failover)
```

### Score Calculation

Each check has a weight (default 25, all enabled):

| Check | Weight | Enabled by |
|-------|--------|------------|
| Process (pgrep) | 25 | `health.process_check: true` |
| TCP :53 connect | 25 | `health.tcp_check: true` |
| UDP :53 query | 25 | `health.udp_check: true` |
| DNS resolution | 25 | `health.dns_query.enabled: true` |

Score = sum of weights where check passes. Max = 100.

---

## Agent Preempt (Replaces sysctl `net.inet.carp.preempt`)

`net.inet.carp.preempt` sysctl is **NOT used** for MASTER reclaim. In the FreeBSD version in use, sysctl preempt does not reliably trigger BACKUP→MASTER based on advskew.

Instead, **agent-level preempt** uses interface down/up:

| Mechanism | How it works | Reliability |
|-----------|-------------|-------------|
| **Agent preempt** (used) | MASTER steps down: `ifconfig <vip_iface> down` | ✅ 100% — link state change |
| sysctl `preempt=1` (not used) | BACKUP reclaims MASTER via advskew | ❌ Unreliable on this FreeBSD |

### Effective Advskew Comparison

The preempt decision compares **effective advskew** (configured advskew + current global demotion) between the local node and its peers. This data is exchanged via the peer heartbeat protocol:

```json
// Each node's /health endpoint returns:
{
  "score": 100,
  "carp_state": "MASTER",
  "advskew": 0,       // configured advskew from rc.conf
  "demotion": 0,      // current sysctl net.inet.carp.demotion
  "timestamp": "2026-06-22T12:00:00Z"
}
```

Step-down occurs only when:

```
peer_effective (peer.advskew + peer.demotion) < my_effective (my.advskew + my.demotion)
```

Meaning the peer has strictly higher priority (lower effective advskew) to be MASTER.

**Example:**

| Node | advskew | demotion | effective | Decision |
|------|---------|----------|-----------|----------|
| A (MASTER) | 0 | 0 | 0 | Peer B effective=100 → 100 < 0? No → **stay MASTER** |
| B (MASTER) | 100 | 0 | 100 | Peer A effective=0 → 0 < 100? Yes → **step down** |

**Cooldown:** Agent steps down at most once per 60 seconds to prevent flapping.

### Complete Failover Flow

```
Normal:
  A: vtnet1 UP, advskew 0  → MASTER
  B: vtnet1 UP, advskew 100 → BACKUP

Step 1 — A dnsdist crash:
  T=0:   A dnsdist process exits
  T=+5s: A agent: health=UNHEALTHY → ifconfig vtnet1 down
         A: vtnet1 DOWN → CARP MASTER → INIT → advertisement STOP
         A: demotion=255 (supplementary)
  T=+5s: B: CARP timeout (~3s from A's last advertisement)
         B: BACKUP → MASTER ✅
         B: dnsdist serves traffic

Step 2 — A dnsdist recovers:
  T=0:   A dnsdist starts
  T=+5s: A agent: health=HEALTHY → ifconfig vtnet1 up
         A: vtnet1 UP → CARP INIT → BACKUP (B is still MASTER)
         A: demotion=0
  T=+5s: A agent sends notification (UNHEALTHY→HEALTHY)
         → CARP state predicted: MASTER (my_effective=0 < peer_effective=100)
         → will become MASTER after B steps down

Step 3 — B step down (agent preempt):
  T=+5s: B agent: policy=preempt, MASTER, peer A HEALTHY
         B: my_effective=100, peer_effective=0
         → 0 < 100 → step down → ifconfig vtnet1 DOWN
         B: vtnet1 DOWN → CARP MASTER → INIT → advertisement STOP
  T=+8s: A: CARP timeout → BACKUP → MASTER ✅ (advskew 0, highest priority)
  T=+10s: B agent: health=HEALTHY → ifconfig vtnet1 up
          B: vtnet1 UP → CARP INIT → BACKUP (A is MASTER, advskew 0 < 100)

Result: A is MASTER again ~15s after dnsdist recovers.
```

### Failover Timeline Diagram

```
Time  A (advskew=0)              B (advskew=100)
────  ───────────────────────    ───────────────────────
T=0   dnsdist crash
T+5s  vtnet1 DOWN                 BACKUP → MASTER (timeout)
      demotion=255                serves traffic ✅
T+5s  ───────────────────────►   B is now MASTER
  ~   [UNHEALTHY state]
T+15s dnsdist starts
T+20s vtnet1 UP
      CARP: INIT → BACKUP
      email: HEALTHY, MASTER(predicted)
T+25s                               peer A HEALTHY
                                    my=100 < peer=0? YES
                                    vtnet1 DOWN (step down)
T+28s BACKUP → MASTER (timeout)   vtnet1 UP → BACKUP
T+30s MASTER ✅                     BACKUP ✅
```

---

## Policy Decision Matrix

| Condition | Demotion | VIP Interface | Preempt Action |
|-----------|----------|---------------|----------------|
| UNHEALTHY (score < 40) | 255 | DOWN | — |
| DEGRADED (score 40-79) | 50 | UP | — |
| HEALTHY (score ≥ 80), BACKUP | 0 | UP | — |
| HEALTHY, MASTER, no healthy peer | 0 | UP | — |
| HEALTHY, MASTER, peer with lower effective advskew | 0 | UP | — (we have higher priority) |
| HEALTHY, MASTER, peer with higher priority (lower effective advskew) | 0 | UP → DOWN | **Step down** via ifconfig down |
| HEALTHY, MASTER, peer healthy but interface was DOWN | 0 | UP (brought up) | Step down already done in previous cycle |

> All nodes **MUST** use `policy: preempt`. `sticky` mode would never step down, so the PRIMARY node would never reclaim MASTER after recovery.

---

## Email Notification

### Trigger

Email is sent on every **state change**: HEALTHY↔DEGRADED↔UNHEALTHY.

### Cooldown

Per-transition cooldown prevents spam. Same transition (e.g. `HEALTHY→UNHEALTHY`) won't trigger again until `notify.cooldown` (default 5m) has elapsed.

```
10:00 HEALTHY→UNHEALTHY → email sent ✅
10:01 still UNHEALTHY   → no email ❌
10:03 UNHEALTHY→HEALTHY → email sent ✅ (different transition)
10:03 HEALTHY→UNHEALTHY → no email ❌ (cooldown: last was at 10:00)
10:06 HEALTHY→UNHEALTHY → email sent ✅ (5m passed)
```

### CARP State Prediction

When a node recovers to HEALTHY and brings its VIP interface UP, the CARP state may temporarily be BACKUP (because the peer is still MASTER). The agent predicts the final CARP state by comparing effective advskew:

```
If my_effective < all_healthy_peers_effective:
  → I WILL become MASTER (either immediately or via peer preempt step-down)
  → Email shows "MASTER"

If any peer has my_effective > peer_effective:
  → Peer has higher priority, I should stay BACKUP
  → Email shows "BACKUP"
```

This prevents misleading "CARP: BACKUP" in emails when the node will deterministically become MASTER.

### Body Format

```
DNSDist HA Agent — State Change

Timestamp: 2026-06-22T12:24:06+07:00
Hostname:  gw1-main-dnsdist-bdg.melsa.net.id

── Management Interface (vtnet0) ──
Node IP:   202.138.224.101

── VIP/CARP Interface (vtnet1) ──
VIP:       202.138.224.100
CARP:      MASTER      ← predicted when deterministic

State:     HEALTHY
Previous:  UNHEALTHY

Checks:
  Process:  ✓
  TCP :53:  ✓
  UDP :53:  ✓
  DNS:      ✓
  Score:    100/100
  Demotion: 0

Reason: all checks passed

Recent CARP/ARP messages from /var/log/messages:
  Jun 22 12:23:26 kernel: carp: demoted by 255 to 1275 (sysctl)
  Jun 22 12:23:26 kernel: carp: 1@vtnet1: MASTER -> INIT (hardware interface down)
  Jun 22 12:23:26 kernel: carp: demoted by 240 to 1515 (interface down)
  Jun 22 12:24:06 kernel: carp: demoted by 0 to 1515 (sysctl)
  Jun 22 12:24:06 kernel: carp: 1@vtnet1: INIT -> BACKUP (initialization complete)
  Jun 22 12:24:06 kernel: carp: demoted by -240 to 1275 (interface up)

This is an automated notification from dnsdist-ha-agent.
```

### CARP/ARP Log Tail

The email includes the last 10 lines from `/var/log/messages` matching `carp:` or `arp:`. This gives immediate visibility into kernel-level CARP state transitions and ARP updates at the time of the event.

---

## Policy

All nodes **MUST** use `policy: preempt`. This is the only mode that supports MASTER reclaim via agent-level preempt.

### Preempt

```
When MASTER + healthy peer with higher priority (lower effective advskew):
  → Step down: ifconfig <vip_iface> DOWN
  → Peer detects CARP timeout → becomes MASTER
  → Bring interface UP → stays BACKUP (peer has higher priority)
```

### Sticky (not recommended)

```
sticky mode:
  → MASTER stays MASTER regardless of peer health or priority
  → Never steps down
  → PRIMARY cannot reclaim MASTER after failover
  → Requires manual intervention or node reboot
```

---

## Log Format

All logs use a custom format with timestamp, level, and component tag:

```
2026-06-22T12:24:06+07:00 [INFO][CHECK HEALTH] health check complete score=100 state=HEALTHY carp=MASTER demotion=0 process=true tcp=true udp=true dns=true decision=healthy/preempt — demotion 0, vip_iface up
2026-06-22T12:24:06+07:00 [INFO][PEER] peer status peer=gw1-secondary-dnsdist-bdg score=100 state=HEALTHY carp=BACKUP
2026-06-22T12:24:06+07:00 [WARN][CARP] failed to read CARP state error=no carp state found on interface vtnet1
2026-06-22T12:24:06+07:00 [INFO][STATE] changing demotion from=255 to=0 reason=healthy/preempt — demotion 0, vip_iface up
2026-06-22T12:24:06+07:00 [INFO][IFACE] bringing VIP interface up interface=vtnet1 reason=healthy/preempt — demotion 0, vip_iface up
2026-06-22T12:24:06+07:00 [INFO][PREEMPT] stepping down — peer has higher priority interface=vtnet1 peer=gw1-primary peer_effective=0 my_effective=100
2026-06-22T12:24:06+07:00 [INFO][NOTIFY] notification sent notifier=email transition=UNHEALTHY->HEALTHY
2026-06-22T12:24:06+07:00 [INFO][AGENT] starting dnsdist-ha-agent config=/usr/local/etc/dnsdist-ha-agent.yaml
```

Tags used:

| Tag | Source |
|-----|--------|
| `[CHECK HEALTH]` | Health check result |
| `[PEER]` | Peer heartbeat status |
| `[CARP]` | CARP state read errors |
| `[STATE]` | Demotion changes |
| `[IFACE]` | Interface up/down operations |
| `[PREEMPT]` | Preempt step-down decisions |
| `[NOTIFY]` | Notification (email) dispatch |
| `[AGENT]` | Agent startup/shutdown |
