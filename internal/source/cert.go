package source

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"math"
	"net"
	"os"
	"strings"
	"syscall"
	"time"

	"github.com/madnh/emday/internal/config"
	"github.com/madnh/emday/internal/model"
)

// Certificate status values. They are metric values users write conditions
// against, so they are part of the config contract: never reword one without
// updating `emday docs source-cert`.
const (
	statusOK               = "ok"
	statusExpired          = "expired"
	statusHostnameMismatch = "hostname-mismatch"
	statusUntrusted        = "untrusted"
	statusDNSFailure       = "dns-failure"
	statusRefused          = "refused"
	statusTimeout          = "timeout"
	statusHandshakeError   = "handshake-error"
	statusConnectError     = "connect-error"
	statusUnreadable       = "unreadable"
)

const defaultCertPort = "443"

// certSource reports how long each watched certificate remains valid, whether
// its chain verifies, and who issued it.
type certSource struct {
	name      string
	endpoints map[string]string // alias -> host[:port], dialled over TLS
	files     map[string]string // alias -> PEM path, read from disk
	timeout   time.Duration
}

func newCertSource(name string, cfg *config.Source) *certSource {
	return &certSource{
		name:      name,
		endpoints: cfg.Endpoints,
		files:     cfg.Files,
		timeout:   cfg.Timeout.Duration,
	}
}

func (s *certSource) Name() string { return s.name }

func (s *certSource) Collect(ctx context.Context) ([]model.Sample, []model.Event, error) {
	now := time.Now()
	var samples []model.Sample

	emit := func(alias string, leaf *x509.Certificate, status string) {
		add := func(suffix string, v model.Value) {
			samples = append(samples, model.Sample{
				Metric: s.name + "." + alias + "." + suffix,
				Value:  v,
				Time:   now,
			})
		}
		// status is emitted on every collect, reachable or not: a metric that
		// disappears stops being evaluated, and then "about to expire" and
		// "host unreachable" would be equally silent.
		add("status", model.StrValue(status))
		if leaf == nil {
			return
		}
		add("days_left", model.NumValue(daysLeft(leaf, now)))
		add("issuer", model.StrValue(issuerName(leaf)))
	}

	for alias, target := range s.endpoints {
		leaf, status := s.probe(ctx, target, now)
		emit(alias, leaf, status)
	}
	for alias, path := range s.files {
		leaf, status := readCertFile(path, now)
		emit(alias, leaf, status)
	}

	return samples, nil, nil
}

// probe dials the endpoint and returns the leaf certificate together with a
// status naming what is wrong with it, if anything.
func (s *certSource) probe(ctx context.Context, target string, now time.Time) (*x509.Certificate, string) {
	ctx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()

	host, addr := splitTarget(target)

	conn, err := (&net.Dialer{}).DialContext(ctx, "tcp", addr)
	if err != nil {
		return nil, classifyDialError(err)
	}
	defer conn.Close()

	// InsecureSkipVerify is deliberate and is what makes this source more
	// useful than `openssl s_client | grep notAfter`: the handshake always
	// yields the chain, so expiry and trust become two independent metrics
	// instead of one failure that hides the other. Verification is done
	// below, by hand, against the system roots.
	tlsConn := tls.Client(conn, &tls.Config{ServerName: host, InsecureSkipVerify: true}) //nolint:gosec // verified manually below
	if err := tlsConn.HandshakeContext(ctx); err != nil {
		if isTimeout(err) {
			return nil, statusTimeout
		}
		return nil, statusHandshakeError
	}
	chain := tlsConn.ConnectionState().PeerCertificates
	if len(chain) == 0 {
		return nil, statusHandshakeError
	}
	leaf := chain[0]

	return leaf, verifyStatus(leaf, chain[1:], host, now)
}

// verifyStatus checks the leaf against the system roots and the intermediates
// the server sent, and names the first thing that fails.
func verifyStatus(leaf *x509.Certificate, intermediates []*x509.Certificate, host string, now time.Time) string {
	if now.After(leaf.NotAfter) {
		return statusExpired
	}
	// Checked here rather than through VerifyOptions.DNSName: with nil Roots,
	// macOS and Windows hand the whole verification to the platform verifier,
	// which reports an untrusted chain without ever reaching the hostname. The
	// same certificate would then read `untrusted` on macOS and
	// `hostname-mismatch` on Linux.
	if err := leaf.VerifyHostname(host); err != nil {
		return statusHostnameMismatch
	}
	inter := x509.NewCertPool()
	for _, c := range intermediates {
		inter.AddCert(c)
	}
	// Roots nil means "system roots"; on a host without a usable trust store
	// Verify reports UnknownAuthority, which is the honest answer.
	_, err := leaf.Verify(x509.VerifyOptions{
		Intermediates: inter,
		CurrentTime:   now,
	})
	if err == nil {
		return statusOK
	}

	var invalid x509.CertificateInvalidError
	// An expired intermediate reaches here with a leaf that is still valid:
	// days_left stays positive while status says expired, which is accurate —
	// days_left always describes the leaf.
	if errors.As(err, &invalid) && invalid.Reason == x509.Expired {
		return statusExpired
	}
	return statusUntrusted
}

// readCertFile reads a PEM file and returns its first certificate. Chain and
// hostname cannot be judged from a file, so status is only ok or expired.
func readCertFile(path string, now time.Time) (*x509.Certificate, string) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, statusUnreadable
	}
	for block, rest := pem.Decode(raw); block != nil; block, rest = pem.Decode(rest) {
		if block.Type != "CERTIFICATE" {
			continue
		}
		leaf, err := x509.ParseCertificate(block.Bytes)
		if err != nil {
			return nil, statusUnreadable
		}
		if now.After(leaf.NotAfter) {
			return leaf, statusExpired
		}
		return leaf, statusOK
	}
	return nil, statusUnreadable
}

// daysLeft floors, so "14" means at least fourteen full days remain and a
// certificate that expired an hour ago reads -1, never 0.
func daysLeft(leaf *x509.Certificate, now time.Time) float64 {
	return math.Floor(leaf.NotAfter.Sub(now).Hours() / 24)
}

// issuerName prefers the issuer CN, the value an operator recognises; some
// CAs leave it empty and carry only the organisation.
func issuerName(leaf *x509.Certificate) string {
	if cn := leaf.Issuer.CommonName; cn != "" {
		return cn
	}
	if org := leaf.Issuer.Organization; len(org) > 0 {
		return strings.Join(org, " ")
	}
	return leaf.Issuer.String()
}

// splitTarget returns the SNI host and the dial address, defaulting the port.
func splitTarget(target string) (host, addr string) {
	if h, _, err := net.SplitHostPort(target); err == nil {
		return h, target
	}
	return target, net.JoinHostPort(target, defaultCertPort)
}

func classifyDialError(err error) string {
	if isTimeout(err) {
		return statusTimeout
	}
	var dnsErr *net.DNSError
	if errors.As(err, &dnsErr) {
		return statusDNSFailure
	}
	if errors.Is(err, syscall.ECONNREFUSED) {
		return statusRefused
	}
	// Some platforms report refusal only in the message.
	if strings.Contains(err.Error(), "connection refused") {
		return statusRefused
	}
	return statusConnectError
}

func isTimeout(err error) bool {
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	var netErr net.Error
	return errors.As(err, &netErr) && netErr.Timeout()
}
