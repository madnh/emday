package source

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/madnh/emday/internal/config"
	"github.com/madnh/emday/internal/model"
)

// Default endpoints are used only when the user configures none. They are
// plain-text services returning exactly one address.
var (
	defaultEndpointsV4 = []string{
		"https://api.ipify.org",
		"https://ipv4.icanhazip.com",
		"https://checkip.amazonaws.com",
	}
	defaultEndpointsV6 = []string{
		"https://api6.ipify.org",
		"https://ipv6.icanhazip.com",
	}
)

const maxIPResponse = 256 // an IP address response has no business being bigger

// --- public-ip: asks configured HTTP endpoints "what is my IP" ---

type publicIPSource struct {
	name      string
	modes     []string
	endpoints map[string][]string     // "v4"/"v6" -> URLs, tried in order
	clients   map[string]*http.Client // family-pinned HTTP clients
}

func newPublicIPSource(name string, cfg *config.Source) *publicIPSource {
	modes := cfg.Mode
	if len(modes) == 0 {
		modes = []string{"v4"}
	}
	ep := map[string][]string{
		"v4": cfg.EndpointsV4,
		"v6": cfg.EndpointsV6,
	}
	if len(ep["v4"]) == 0 {
		ep["v4"] = defaultEndpointsV4
	}
	if len(ep["v6"]) == 0 {
		ep["v6"] = defaultEndpointsV6
	}
	// Pin the transport to the address family: asking a dual-stack endpoint
	// "what is my IP" over the wrong family would report the wrong address.
	clients := map[string]*http.Client{}
	for family, network := range map[string]string{"v4": "tcp4", "v6": "tcp6"} {
		network := network
		dialer := &net.Dialer{Timeout: 10 * time.Second}
		clients[family] = &http.Client{
			Timeout: 15 * time.Second,
			Transport: &http.Transport{
				DialContext: func(ctx context.Context, _, addr string) (net.Conn, error) {
					return dialer.DialContext(ctx, network, addr)
				},
			},
		}
	}
	return &publicIPSource{name: name, modes: modes, endpoints: ep, clients: clients}
}

func (s *publicIPSource) Name() string { return s.name }

func (s *publicIPSource) Collect(ctx context.Context) ([]model.Sample, []model.Event, error) {
	now := time.Now()
	var samples []model.Sample
	var errs []string

	for _, family := range s.modes {
		addr, err := s.fetch(ctx, family)
		if err != nil {
			errs = append(errs, fmt.Sprintf("%s: %v", family, err))
			continue
		}
		samples = append(samples, model.Sample{
			Metric: s.name + "." + family,
			Value:  model.StrValue(addr),
			Time:   now,
		})
	}

	if len(samples) == 0 && len(errs) > 0 {
		return nil, nil, fmt.Errorf("%s", strings.Join(errs, "; "))
	}
	return samples, nil, nil
}

// fetch tries each endpoint in order and returns the first response that is
// exactly one valid address of the requested family.
func (s *publicIPSource) fetch(ctx context.Context, family string) (string, error) {
	var lastErr error
	for _, endpoint := range s.endpoints[family] {
		addr, err := s.fetchOne(ctx, family, endpoint)
		if err != nil {
			lastErr = fmt.Errorf("%s: %w", endpoint, err)
			continue
		}
		return addr, nil
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("no endpoints configured for %s", family)
	}
	return "", lastErr
}

func (s *publicIPSource) fetchOne(ctx context.Context, family, endpoint string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "emday")
	resp, err := s.clients[family].Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxIPResponse+1))
	if err != nil {
		return "", err
	}
	if len(body) > maxIPResponse {
		return "", fmt.Errorf("response too large for an IP address")
	}
	return ValidateIP(strings.TrimSpace(string(body)), family)
}

// ValidateIP accepts only a bare, valid address of the requested family —
// HTML pages, error strings, or the wrong family are rejected so a broken
// endpoint can never be mistaken for an IP change.
func ValidateIP(raw, family string) (string, error) {
	if raw == "" || strings.ContainsAny(raw, " \t\r\n") {
		return "", fmt.Errorf("response is not a bare IP address: %.60q", raw)
	}
	ip := net.ParseIP(raw)
	if ip == nil {
		return "", fmt.Errorf("response is not a valid IP address: %.60q", raw)
	}
	isV4 := ip.To4() != nil
	switch family {
	case "v4":
		if !isV4 {
			return "", fmt.Errorf("expected IPv4, got %q", raw)
		}
	case "v6":
		if isV4 {
			return "", fmt.Errorf("expected IPv6, got %q", raw)
		}
	}
	return ip.String(), nil
}

// --- local-ip: reads interface addresses from the kernel, no network calls ---

type localIPSource struct {
	name       string
	interfaces []string
}

func newLocalIPSource(name string, cfg *config.Source) *localIPSource {
	return &localIPSource{name: name, interfaces: cfg.Interfaces}
}

func (s *localIPSource) Name() string { return s.name }

func (s *localIPSource) Collect(ctx context.Context) ([]model.Sample, []model.Event, error) {
	now := time.Now()
	var samples []model.Sample
	var errs []string

	for _, iface := range s.interfaces {
		v4, v6, err := localAddrs(iface)
		if err != nil {
			errs = append(errs, fmt.Sprintf("%s: %v", iface, err))
			continue
		}
		if v4 != "" {
			samples = append(samples, model.Sample{Metric: s.name + "." + iface + "_v4", Value: model.StrValue(v4), Time: now})
		}
		if v6 != "" {
			samples = append(samples, model.Sample{Metric: s.name + "." + iface + "_v6", Value: model.StrValue(v6), Time: now})
		}
	}

	if len(samples) == 0 && len(errs) > 0 {
		return nil, nil, fmt.Errorf("%s", strings.Join(errs, "; "))
	}
	return samples, nil, nil
}

// localAddrs returns the first global unicast v4 and v6 address of a NIC.
func localAddrs(name string) (v4, v6 string, err error) {
	iface, err := net.InterfaceByName(name)
	if err != nil {
		return "", "", err
	}
	addrs, err := iface.Addrs()
	if err != nil {
		return "", "", err
	}
	for _, a := range addrs {
		ipnet, ok := a.(*net.IPNet)
		if !ok || !ipnet.IP.IsGlobalUnicast() {
			continue
		}
		if ip4 := ipnet.IP.To4(); ip4 != nil {
			if v4 == "" {
				v4 = ip4.String()
			}
		} else if v6 == "" {
			v6 = ipnet.IP.String()
		}
	}
	return v4, v6, nil
}
