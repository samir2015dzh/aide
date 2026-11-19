package netif

import (
	"net"
	"testing"
)

func TestUsable(t *testing.T) {
	for name, want := range map[string]bool{
		"en0": true, "utun5": true, "pdp_ip0": true, "eth0": true,
		"awdl0": false, "bridge100": false, "docker0": false, "ipsec0": false, "pdp_ip1": false,
	} {
		if got := UsableInterface(name); got != want {
			t.Errorf("UsableInterface(%q) = %v, want %v", name, got, want)
		}
	}
	for ip, want := range map[string]bool{
		"192.168.1.127": true, "100.64.0.2": true, "2408:8207:185c:e470::1": true,
		"198.18.0.1": false, "fe80::1": false, "fdfe:5bbd:6105::1": false,
	} {
		if got := UsableInterfaceIP(net.ParseIP(ip)); got != want {
			t.Errorf("UsableInterfaceIP(%s) = %v, want %v", ip, got, want)
		}
	}
}
