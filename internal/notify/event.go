package notify

import (
	"log/slog"
	"time"
)

type EventDispatcher struct {
	notifiers []Notifier
	cooldown  *Cooldown
	log       *slog.Logger
}

func NewEventDispatcher(log *slog.Logger, cooldownDuration time.Duration) *EventDispatcher {
	return &EventDispatcher{
		cooldown: NewCooldown(cooldownDuration),
		log:      log,
	}
}

func (d *EventDispatcher) AddNotifier(n Notifier) {
	d.notifiers = append(d.notifiers, n)
}

func (d *EventDispatcher) Dispatch(newState, oldState string, score, demotion int, nodeIP, vip, carpState, mgmtIface, vipIface string, processOK, tcpOK, udpOK, dnsOK bool) {
	key := oldState + "->" + newState

	if !d.cooldown.Allow(key) {
		d.log.Info("[NOTIFY] notification suppressed by cooldown",
			"transition", key,
		)
		return
	}

	subject, body := RenderNotification(newState, oldState, score, demotion, nodeIP, vip, carpState, mgmtIface, vipIface, processOK, tcpOK, udpOK, dnsOK)

	for _, n := range d.notifiers {
		if err := n.Send(subject, body); err != nil {
			d.log.Error("[NOTIFY] notification failed",
				"notifier", n.Name(),
				"error", err,
				"transition", key,
			)
		} else {
			d.log.Info("[NOTIFY] notification sent",
				"notifier", n.Name(),
				"transition", key,
			)
		}
	}
}
