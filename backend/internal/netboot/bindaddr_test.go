package netboot

import (
	"net"
	"strings"
	"testing"
)

// loopbackInterface finds the system loopback interface with an IPv4 address.
func loopbackInterface(t *testing.T) (string, string) {
	t.Helper()
	ifaces, err := net.Interfaces()
	if err != nil {
		t.Fatalf("list interfaces: %v", err)
	}
	for _, iface := range ifaces {
		if iface.Flags&net.FlagLoopback == 0 {
			continue
		}
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, a := range addrs {
			if ipNet, ok := a.(*net.IPNet); ok && ipNet.IP.To4() != nil {
				return iface.Name, ipNet.IP.String()
			}
		}
	}
	t.Skip("no loopback interface with IPv4 found")
	return "", ""
}

func TestBindAddrNoInterfacePassesThrough(t *testing.T) {
	for _, addr := range []string{":6969", "0.0.0.0:6969", "10.0.0.1:69"} {
		got, err := BindAddr("", addr)
		if err != nil {
			t.Fatalf("BindAddr(%q): %v", addr, err)
		}
		if got != addr {
			t.Errorf("BindAddr(%q) = %q, want unchanged", addr, got)
		}
	}
}

func TestBindAddrResolvesInterfaceIP(t *testing.T) {
	name, ip := loopbackInterface(t)
	got, err := BindAddr(name, ":6969")
	if err != nil {
		t.Fatalf("BindAddr(%q): %v", name, err)
	}
	want := net.JoinHostPort(ip, "6969")
	if got != want {
		t.Errorf("BindAddr = %q, want %q", got, want)
	}
}

func TestBindAddrOverridesExistingHost(t *testing.T) {
	name, ip := loopbackInterface(t)
	got, err := BindAddr(name, "0.0.0.0:8082")
	if err != nil {
		t.Fatal(err)
	}
	if got != net.JoinHostPort(ip, "8082") {
		t.Errorf("BindAddr = %q, want interface IP with port 8082", got)
	}
}

func TestBindAddrUnknownInterface(t *testing.T) {
	_, err := BindAddr("does-not-exist0", ":69")
	if err == nil || !strings.Contains(err.Error(), "does-not-exist0") {
		t.Fatalf("err = %v, want unknown-interface error naming it", err)
	}
}

func TestBindAddrBadAddr(t *testing.T) {
	name, _ := loopbackInterface(t)
	if _, err := BindAddr(name, "no-port"); err == nil {
		t.Fatal("expected error for address without port")
	}
}
