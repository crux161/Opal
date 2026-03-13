package omiai

import (
	"fmt"
	"net"
	"net/url"
	"strings"
)

const DefaultServerHost = "127.0.0.1"

func NormalizeServerHost(host string) string {
	host = strings.TrimSpace(host)
	if host == "" {
		return DefaultServerHost
	}
	return host
}

func ValidateServerHost(host string) error {
	host = NormalizeServerHost(host)
	if net.ParseIP(host) == nil {
		return fmt.Errorf("server must be a single IP address")
	}
	return nil
}

func EndpointsForServerHost(host string) (apiURL, signalingURL string) {
	host = NormalizeServerHost(host)
	return "http://" + host + ":8000", "ws://" + host + ":4000/ws/sankaku/websocket"
}

func ServerHostFromEndpoint(raw string) string {
	parsed, err := url.Parse(raw)
	if err != nil {
		return NormalizeServerHost(raw)
	}
	if parsed.Hostname() == "" {
		return DefaultServerHost
	}
	return parsed.Hostname()
}
