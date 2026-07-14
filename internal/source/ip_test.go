package source

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/madnh/emday/internal/config"
)

// public-ip is tested against a local httptest server, never the real internet,
// so the contract (strict validation, string value, metric name) is pinned
// hermetically.
func TestPublicIPAcceptsBareAddress(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte("203.0.113.7\n"))
	}))
	defer srv.Close()

	s := newPublicIPSource("wan", &config.Source{
		Type:        "public-ip",
		Mode:        []string{"v4"},
		EndpointsV4: []string{srv.URL},
	})
	m := collectMap(t, s)

	v, ok := m["wan.v4"]
	if !ok {
		t.Fatalf("missing wan.v4 (have: %v)", keys(m))
	}
	if v.IsNum {
		t.Errorf("wan.v4 should be a string address, got numeric %v", v.Num)
	}
	if v.Str != "203.0.113.7" {
		t.Errorf("wan.v4 = %q, want 203.0.113.7", v.Str)
	}
}

// A non-address response (HTML, an error page) must be rejected so a broken
// endpoint is never mistaken for an IP change. With only v4 configured and it
// failing, Collect returns an error and emits nothing.
func TestPublicIPRejectsNonAddress(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte("<html>rate limited</html>"))
	}))
	defer srv.Close()

	s := newPublicIPSource("wan", &config.Source{
		Type:        "public-ip",
		Mode:        []string{"v4"},
		EndpointsV4: []string{srv.URL},
	})
	samples, _, err := s.Collect(context.Background())
	if err == nil {
		t.Fatalf("expected an error when the only endpoint returns non-IP, got samples %v", samples)
	}
	if len(samples) != 0 {
		t.Errorf("expected no samples, got %v", samples)
	}
}

// (ValidateIP itself is covered by TestValidateIP in exec_test.go.)

// local-ip reads NIC addresses from the kernel (netlink). Loopback is not
// global-unicast, so it must be filtered out — a deterministic contract check.
func TestLocalIPFiltersLoopback(t *testing.T) {
	lo := loopbackName(t)
	s := newLocalIPSource("lan", &config.Source{Type: "local-ip", Interfaces: []string{lo}})
	m := collectMap(t, s)
	for k := range m {
		t.Errorf("loopback %s must emit no address, got %s", lo, k)
	}
}

// If a NIC has a global-unicast address, local-ip emits it under the expected
// metric name and it parses as an IP. Guards the netlink-backed reading + the
// <iface>_v4/_v6 naming.
func TestLocalIPEmitsParseableAddress(t *testing.T) {
	name, fam := ifaceWithGlobalUnicast(t)
	if name == "" {
		t.Skip("no non-loopback global-unicast interface on this host")
	}
	s := newLocalIPSource("lan", &config.Source{Type: "local-ip", Interfaces: []string{name}})
	m := collectMap(t, s)

	key := "lan." + name + "_" + fam
	v, ok := m[key]
	if !ok {
		t.Fatalf("expected %q (have: %v)", key, keys(m))
	}
	if net.ParseIP(v.Str) == nil {
		t.Errorf("%s = %q is not a parseable IP", key, v.Str)
	}
}

func loopbackName(t *testing.T) string {
	t.Helper()
	ifaces, err := net.Interfaces()
	if err != nil {
		t.Fatalf("net.Interfaces: %v", err)
	}
	for _, i := range ifaces {
		if i.Flags&net.FlagLoopback != 0 {
			return i.Name
		}
	}
	t.Skip("no loopback interface found")
	return ""
}

// ifaceWithGlobalUnicast returns the first interface carrying a global-unicast
// address and which family ("v4"/"v6") it is, or "" if none exists.
func ifaceWithGlobalUnicast(t *testing.T) (name, family string) {
	t.Helper()
	ifaces, err := net.Interfaces()
	if err != nil {
		t.Fatalf("net.Interfaces: %v", err)
	}
	for _, i := range ifaces {
		if i.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, err := i.Addrs()
		if err != nil {
			continue
		}
		for _, a := range addrs {
			ipnet, ok := a.(*net.IPNet)
			if !ok || !ipnet.IP.IsGlobalUnicast() {
				continue
			}
			if ipnet.IP.To4() != nil {
				return i.Name, "v4"
			}
			return i.Name, "v6"
		}
	}
	return "", ""
}
