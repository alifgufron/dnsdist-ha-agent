package health

import (
	"net"
	"time"

	"github.com/miekg/dns"
)

func CheckUDP(address string, timeout time.Duration) bool {
	if address == "" {
		address = "127.0.0.1:53"
	}

	m := new(dns.Msg)
	m.SetQuestion(dns.Fqdn("google.com"), dns.TypeA)
	m.RecursionDesired = true

	query, err := m.Pack()
	if err != nil {
		return false
	}

	conn, err := net.DialTimeout("udp", address, timeout)
	if err != nil {
		return false
	}
	defer conn.Close()

	if err := conn.SetDeadline(time.Now().Add(timeout)); err != nil {
		return false
	}

	if _, err := conn.Write(query); err != nil {
		return false
	}

	buf := make([]byte, 512)
	n, err := conn.Read(buf)
	if err != nil {
		return false
	}

	// Verify we got at least a valid DNS response header (12+ bytes)
	return n >= 12
}
