// Package netif says which of this computer's network interfaces and
// addresses can reach a peer: those the direct door offers (internal/machine)
// and those a device compares to notice it moved (internal/owner).
package netif

import (
	"net"
	"strings"
)

// UsableInterface reports whether an interface can carry peer traffic.
// Apple peer-to-peer/internal radios, hypervisor/container bridges, carrier
// IPsec tunnels (ipsec*), and secondary iOS cellular bearers (pdp_ip1..N,
// used for IMS/carrier control) have no peer route.
func UsableInterface(name string) bool {
	for _, prefix := range []string{
		"awdl", "llw", "anpi", "anri", "ap", "nan", "gif", "stf", "ipsec",
		"bridge", "vnic", "vmnet", "vboxnet", "vmenet", "virbr", "docker",
	} {
		if strings.HasPrefix(name, prefix) {
			return false
		}
	}
	if strings.HasPrefix(name, "pdp_ip") && name != "pdp_ip0" {
		return false
	}
	return true
}

// UsableInterfaceIP reports whether an interface IP can reach a peer.
// It excludes link-local addresses, RFC 2544 benchmark / Surge Fake-IP
// space (198.18.0.0/15), and IPv6 Unique Local Addresses (fc00::/7).
func UsableInterfaceIP(ip net.IP) bool {
	if len(ip) == 0 || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() {
		return false
	}
	if v4 := ip.To4(); v4 != nil {
		if v4[0] == 198 && (v4[1] == 18 || v4[1] == 19) {
			return false
		}
		return true
	}
	return !ip.IsPrivate()
}
