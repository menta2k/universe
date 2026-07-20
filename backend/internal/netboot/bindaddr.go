package netboot

import (
	"fmt"
	"net"
)

// BindAddr restricts a listen address to a specific network interface by
// replacing its host with the interface's first IPv4 address. An empty iface
// returns addr unchanged (listen on all interfaces). Any host already present
// in addr is overridden — the interface option is authoritative.
//
// Used by the machine-facing TFTP and boot-HTTP listeners; the DHCP server
// binds to its interface directly at the socket level (dhcp_interface).
func BindAddr(iface, addr string) (string, error) {
	if iface == "" {
		return addr, nil
	}
	_, port, err := net.SplitHostPort(addr)
	if err != nil {
		return "", fmt.Errorf("parse listen addr %q: %w", addr, err)
	}
	nif, err := net.InterfaceByName(iface)
	if err != nil {
		return "", fmt.Errorf("interface %q: %w", iface, err)
	}
	addrs, err := nif.Addrs()
	if err != nil {
		return "", fmt.Errorf("addresses of interface %q: %w", iface, err)
	}
	for _, a := range addrs {
		if ipNet, ok := a.(*net.IPNet); ok && ipNet.IP.To4() != nil {
			return net.JoinHostPort(ipNet.IP.String(), port), nil
		}
	}
	return "", fmt.Errorf("interface %q has no IPv4 address", iface)
}
