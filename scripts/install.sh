#!/bin/sh

set -e

BINDIR="${DESTDIR}/usr/local/bin"
CONFDIR="${DESTDIR}/usr/local/etc"
RCDIR="${DESTDIR}/usr/local/etc/rc.d"
LOGDIR="${DESTDIR}/var/log"
DBDIR="${DESTDIR}/var/db"

echo "Installing dnsdist-ha-agent..."

mkdir -p "${BINDIR}" "${CONFDIR}" "${RCDIR}" "${LOGDIR}" "${DBDIR}"

install -m 0555 build/dnsdist-ha-agent-freebsd-amd64 "${BINDIR}/dnsdist-ha-agent"

if [ ! -f "${CONFDIR}/dnsdist-ha-agent.yaml" ]; then
	install -m 0640 configs/config.yaml "${CONFDIR}/dnsdist-ha-agent.yaml"
	echo "  => Installed default config to ${CONFDIR}/dnsdist-ha-agent.yaml"
	echo "  => EDIT this file and set your token, password, and peers"
else
	echo "  => Config already exists at ${CONFDIR}/dnsdist-ha-agent.yaml, skipping"
fi

install -m 0555 scripts/rc.d/dnsdist-ha-agent "${RCDIR}/dnsdist-ha-agent"

echo ""
echo "Installation complete."
echo ""
echo "Next steps:"
echo "  1. Edit ${CONFDIR}/dnsdist-ha-agent.yaml"
echo "  2. Set HA token in /etc/rc.conf.d/dnsdist-ha-agent:"
echo "       mkdir -p /etc/rc.conf.d"
echo "       cat > /etc/rc.conf.d/dnsdist-ha-agent << 'EOF'"
echo "       export HA_TOKEN=\"YOUR_SHARED_SECRET\""
echo "       export SMTP_PASS=\"your-smtp-password\""
echo "       EOF"
echo "       chmod 0600 /etc/rc.conf.d/dnsdist-ha-agent"
echo "  3. Enable the service:"
echo "       sysrc dnsdist_ha_agent_enable=YES"
echo "  4. Start the service:"
echo "       service dnsdist-ha-agent start"
echo "  5. Check status:"
echo "       service dnsdist-ha-agent status"
echo ""
echo "See docs/usage.md for detailed documentation."
