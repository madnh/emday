package source

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/madnh/emday/internal/model"
)

// selfSigned builds a certificate for one host with an explicit validity
// window, so tests can produce expired and mismatched certificates without
// waiting or reaching the network.
func selfSigned(t *testing.T, host string, notBefore, notAfter time.Time) (*x509.Certificate, []byte) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generating key: %v", err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: host},
		Issuer:       pkix.Name{CommonName: "emday test CA"},
		NotBefore:    notBefore,
		NotAfter:     notAfter,
		DNSNames:     []string{host},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("creating certificate: %v", err)
	}
	leaf, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatalf("parsing certificate: %v", err)
	}
	return leaf, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
}

func samplesByMetric(samples []model.Sample) map[string]model.Value {
	out := map[string]model.Value{}
	for _, s := range samples {
		out[s.Metric] = s.Value
	}
	return out
}

// The whole point of the source: a certificate that does NOT verify still
// yields its expiry and its issuer, so "expired" and "untrusted" stay
// separate answers instead of one failure hiding the other.
func TestCertCollectReportsExpiryOfAnUntrustedCertificate(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {}))
	defer srv.Close()

	addr := srv.Listener.Addr().String()
	s := &certSource{name: "cert", endpoints: map[string]string{"web": addr}, timeout: 5 * time.Second}
	samples, events, err := s.Collect(context.Background())
	if err != nil || events != nil {
		t.Fatalf("Collect() = _, %v, %v; want no events and no error", events, err)
	}

	got := samplesByMetric(samples)
	if len(got) != 3 {
		t.Fatalf("got %d metrics %v, want 3", len(got), got)
	}
	// httptest signs its own certificate, so it is a valid certificate from an
	// authority the system does not trust.
	if status := got["cert.web.status"].Str; status != statusUntrusted {
		t.Errorf("cert.web.status = %q, want %q", status, statusUntrusted)
	}
	days, ok := got["cert.web.days_left"], got["cert.web.days_left"].IsNum
	if !ok || days.Num <= 0 {
		t.Errorf("cert.web.days_left = %v, want a positive number", days)
	}
	if issuer := got["cert.web.issuer"].Str; issuer == "" {
		t.Error("cert.web.issuer is empty, want the issuing CA's name")
	}
}

// A probe that fails must still emit `status`, because a metric that
// disappears stops being evaluated and the alert goes silent.
func TestCertCollectEmitsStatusWhenTheProbeFails(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	addr := ln.Addr().String()
	ln.Close() // nothing is listening on this port any more

	s := &certSource{name: "cert", endpoints: map[string]string{"dead": addr}, timeout: 5 * time.Second}
	samples, _, err := s.Collect(context.Background())
	if err != nil {
		t.Fatalf("Collect() error = %v, want nil (a failed probe is a status, not an error)", err)
	}
	got := samplesByMetric(samples)
	if len(got) != 1 {
		t.Fatalf("got %d metrics %v, want only cert.dead.status", len(got), got)
	}
	status := got["cert.dead.status"].Str
	if runtime.GOOS == "windows" {
		// Windows reports refusal as WSAECONNREFUSED, which does not match
		// syscall.ECONNREFUSED, so it lands in the generic bucket.
		if status != statusRefused && status != statusConnectError {
			t.Errorf("cert.dead.status = %q, want %q or %q", status, statusRefused, statusConnectError)
		}
		return
	}
	if status != statusRefused {
		t.Errorf("cert.dead.status = %q, want %q", status, statusRefused)
	}
}

func TestVerifyStatus(t *testing.T) {
	now := time.Date(2026, 8, 11, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		name      string
		host      string
		certHost  string
		notBefore time.Time
		notAfter  time.Time
		want      string
	}{
		{
			name:      "expired",
			host:      "example.test",
			certHost:  "example.test",
			notBefore: now.AddDate(-1, 0, 0),
			notAfter:  now.Add(-time.Hour),
			want:      statusExpired,
		},
		{
			name:      "hostname not covered",
			host:      "other.test",
			certHost:  "example.test",
			notBefore: now.Add(-time.Hour),
			notAfter:  now.AddDate(0, 0, 90),
			want:      statusHostnameMismatch,
		},
		{
			name:      "valid but self-signed",
			host:      "example.test",
			certHost:  "example.test",
			notBefore: now.Add(-time.Hour),
			notAfter:  now.AddDate(0, 0, 90),
			want:      statusUntrusted,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			leaf, _ := selfSigned(t, tc.certHost, tc.notBefore, tc.notAfter)
			if got := verifyStatus(leaf, nil, tc.host, now); got != tc.want {
				t.Errorf("verifyStatus() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestReadCertFile(t *testing.T) {
	now := time.Date(2026, 8, 11, 12, 0, 0, 0, time.UTC)
	dir := t.TempDir()

	_, validPEM := selfSigned(t, "example.test", now.Add(-time.Hour), now.AddDate(0, 0, 30))
	_, expiredPEM := selfSigned(t, "example.test", now.AddDate(-1, 0, 0), now.Add(-time.Hour))

	write := func(name string, data []byte) string {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, data, 0o600); err != nil {
			t.Fatalf("writing %s: %v", name, err)
		}
		return path
	}

	tests := []struct {
		name       string
		path       string
		wantStatus string
		wantCert   bool
	}{
		{"valid", write("valid.pem", validPEM), statusOK, true},
		{"expired", write("expired.pem", expiredPEM), statusExpired, true},
		{"not a certificate", write("junk.pem", []byte("hello")), statusUnreadable, false},
		{"missing", filepath.Join(dir, "absent.pem"), statusUnreadable, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			leaf, status := readCertFile(tc.path, now)
			if status != tc.wantStatus {
				t.Errorf("status = %q, want %q", status, tc.wantStatus)
			}
			if (leaf != nil) != tc.wantCert {
				t.Errorf("certificate returned = %v, want %v", leaf != nil, tc.wantCert)
			}
		})
	}
}

func TestClassifyDialError(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want string
	}{
		{"name resolution", &net.DNSError{Err: "no such host", Name: "nope.invalid", IsNotFound: true}, statusDNSFailure},
		{"deadline", context.DeadlineExceeded, statusTimeout},
		{"timeout", &net.DNSError{Err: "i/o timeout", IsTimeout: true}, statusTimeout},
		{"other", net.UnknownNetworkError("tcp9"), statusConnectError},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := classifyDialError(tc.err); got != tc.want {
				t.Errorf("classifyDialError(%v) = %q, want %q", tc.err, got, tc.want)
			}
		})
	}
}

// days_left floors, so a threshold of "<= 14" cannot be crossed a day late.
func TestDaysLeftFloors(t *testing.T) {
	now := time.Date(2026, 8, 11, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		name     string
		notAfter time.Time
		want     float64
	}{
		{"fourteen days and change", now.AddDate(0, 0, 14).Add(3 * time.Hour), 14},
		{"just under a day", now.Add(23 * time.Hour), 0},
		{"expired an hour ago", now.Add(-time.Hour), -1},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			leaf := &x509.Certificate{NotAfter: tc.notAfter}
			if got := daysLeft(leaf, now); got != tc.want {
				t.Errorf("daysLeft() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestSplitTarget(t *testing.T) {
	tests := []struct {
		target   string
		wantHost string
		wantAddr string
	}{
		{"example.test", "example.test", "example.test:443"},
		{"example.test:8443", "example.test", "example.test:8443"},
		{"127.0.0.1:443", "127.0.0.1", "127.0.0.1:443"},
	}
	for _, tc := range tests {
		t.Run(tc.target, func(t *testing.T) {
			host, addr := splitTarget(tc.target)
			if host != tc.wantHost || addr != tc.wantAddr {
				t.Errorf("splitTarget(%q) = %q, %q; want %q, %q", tc.target, host, addr, tc.wantHost, tc.wantAddr)
			}
		})
	}
}

func TestIssuerNameFallsBackToOrganization(t *testing.T) {
	leaf := &x509.Certificate{Issuer: pkix.Name{Organization: []string{"Let's Encrypt"}}}
	if got := issuerName(leaf); got != "Let's Encrypt" {
		t.Errorf("issuerName() = %q, want %q", got, "Let's Encrypt")
	}
}

// A port that speaks something other than TLS must be distinguishable from a
// port that refuses and from one that times out: they need different fixes.
func TestCertProbeReportsHandshakeErrorOnANonTLSPort(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			// Answer the ClientHello with plain text, never a ServerHello.
			conn.Write([]byte("HTTP/1.1 400 Bad Request\r\n\r\n"))
			conn.Close()
		}
	}()

	s := &certSource{name: "cert", endpoints: map[string]string{"plain": ln.Addr().String()}, timeout: 5 * time.Second}
	samples, _, err := s.Collect(context.Background())
	if err != nil {
		t.Fatalf("Collect() error = %v, want nil", err)
	}
	got := samplesByMetric(samples)
	if status := got["cert.plain.status"].Str; status != statusHandshakeError {
		t.Errorf("cert.plain.status = %q, want %q", status, statusHandshakeError)
	}
	if _, ok := got["cert.plain.days_left"]; ok {
		t.Error("days_left was emitted for a probe that obtained no certificate")
	}
}
