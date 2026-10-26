package anchor

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"time"
)

var (
	ErrBadScheme        = errors.New("anchor: only http/https on port 80/443 is allowed")
	ErrCredentials      = errors.New("anchor: url must not contain credentials")
	ErrBlockedHost      = errors.New("anchor: address is not allowed")
	ErrTooManyRedirects = errors.New("anchor: too many redirects")
	ErrTooLarge         = errors.New("anchor: response body exceeds the size cap")
	ErrUpstream         = errors.New("anchor: upstream did not return success")
)

const (
	fetchTimeout = 5 * time.Second
	maxBodyBytes = 1 << 20
	maxRedirects = 3
)

// Fetcher returns the body at a URL; Checker takes one so tests can stub the network.
type Fetcher interface {
	GetBytes(ctx context.Context, rawURL string) ([]byte, error)
}

// Resolver resolves a hostname; *net.Resolver satisfies it and tests supply a fake.
type Resolver interface {
	LookupIPAddr(ctx context.Context, host string) ([]net.IPAddr, error)
}

// Fetch is the guarded Fetcher: every hop is resolved, address-checked, then dialed on the address that passed.
type Fetch struct{ resolver Resolver }

func NewFetch(r Resolver) *Fetch { return &Fetch{resolver: r} }

func (f *Fetch) GetBytes(ctx context.Context, rawURL string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, fetchTimeout)
	defer cancel()
	current := rawURL
	for hops := 0; ; hops++ {
		resp, err := f.doOnce(ctx, current)
		if err != nil {
			return nil, err
		}
		switch resp.StatusCode {
		case http.StatusMovedPermanently, http.StatusFound, http.StatusSeeOther, http.StatusTemporaryRedirect, http.StatusPermanentRedirect:
			loc := resp.Header.Get("Location")
			resp.Body.Close()
			if loc == "" {
				return nil, fmt.Errorf("%w: redirect with no Location", ErrUpstream)
			}
			if hops >= maxRedirects {
				return nil, ErrTooManyRedirects
			}
			base, err := url.Parse(current)
			if err != nil {
				return nil, fmt.Errorf("%w: %v", ErrUpstream, err)
			}
			next, err := base.Parse(loc)
			if err != nil {
				return nil, fmt.Errorf("%w: %v", ErrUpstream, err)
			}
			current = next.String()
			continue
		}
		data, err := io.ReadAll(io.LimitReader(resp.Body, maxBodyBytes+1))
		resp.Body.Close()
		if err != nil {
			return nil, fmt.Errorf("%w: %v", ErrUpstream, err)
		}
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			return nil, fmt.Errorf("%w: status %d", ErrUpstream, resp.StatusCode)
		}
		if len(data) > maxBodyBytes {
			return nil, ErrTooLarge
		}
		return data, nil
	}
}

func (f *Fetch) doOnce(ctx context.Context, rawURL string) (*http.Response, error) {
	u, err := url.Parse(rawURL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" {
		return nil, ErrBadScheme
	}
	if u.User != nil {
		return nil, ErrCredentials
	}
	host := u.Hostname()
	pinned, err := f.resolveAndGuard(ctx, host)
	if err != nil {
		return nil, err
	}
	port := u.Port()
	if port == "" {
		port = map[string]string{"http": "80", "https": "443"}[u.Scheme]
	}
	if port != "80" && port != "443" {
		return nil, ErrBadScheme
	}
	pinnedAddr := net.JoinHostPort(pinned.String(), port)
	dialer := &net.Dialer{}
	client := &http.Client{
		Transport: &http.Transport{
			// The dial ignores the request's own address so nothing can re-resolve between the check and the connection.
			DialContext: func(ctx context.Context, network, _ string) (net.Conn, error) {
				return dialer.DialContext(ctx, network, pinnedAddr)
			},
			TLSClientConfig: &tls.Config{ServerName: host},
		},
		// GetBytes follows redirects itself so each hop gets a fresh guard check.
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, ErrBadScheme
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrUpstream, err)
	}
	return resp, nil
}

// resolveAndGuard returns the one address to dial; any blocked address among the answers rejects the host, and a failed lookup fails closed.
func (f *Fetch) resolveAndGuard(ctx context.Context, host string) (net.IP, error) {
	if ip := net.ParseIP(host); ip != nil {
		if isBlockedIP(ip) {
			return nil, ErrBlockedHost
		}
		return ip, nil
	}
	addrs, err := f.resolver.LookupIPAddr(ctx, host)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrBlockedHost, err)
	}
	if len(addrs) == 0 {
		return nil, fmt.Errorf("%w: no addresses", ErrBlockedHost)
	}
	for _, a := range addrs {
		if isBlockedIP(a.IP) {
			return nil, ErrBlockedHost
		}
	}
	return addrs[0].IP, nil
}

// blockedCIDRs covers ranges the net.IP Is* methods miss.
var blockedCIDRs = mustParseCIDRs([]string{
	"100.64.0.0/10", "0.0.0.0/8", "192.0.0.0/24", "192.0.2.0/24", "198.18.0.0/15", "198.51.100.0/24",
	"203.0.113.0/24", "224.0.0.0/4", "240.0.0.0/4", "255.255.255.255/32",
	"::/128", "::1/128", "64:ff9b::/96", "64:ff9b:1::/48", "100::/64", "2001::/32", "2001:db8::/32",
	"fc00::/7", "fe80::/10", "ff00::/8", "2002::/16",
})

func mustParseCIDRs(cidrs []string) []*net.IPNet {
	nets := make([]*net.IPNet, len(cidrs))
	for i, c := range cidrs {
		_, n, err := net.ParseCIDR(c)
		if err != nil {
			panic("anchor: invalid CIDR " + c)
		}
		nets[i] = n
	}
	return nets
}

var nat64Prefix = [12]byte{0x00, 0x64, 0xff, 0x9b}

// isBlockedIP recurses into an IPv4 address embedded in the low 32 bits, because net.IP does not normalize ::a.b.c.d or NAT64 forms.
func isBlockedIP(ip net.IP) bool {
	if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsMulticast() || ip.IsUnspecified() {
		return true
	}
	for _, n := range blockedCIDRs {
		if n.Contains(ip) {
			return true
		}
	}
	if ip.To4() == nil {
		if ip16 := ip.To16(); ip16 != nil {
			high := [12]byte(ip16[:12])
			if high == ([12]byte{}) || high == nat64Prefix {
				return isBlockedIP(net.IPv4(ip16[12], ip16[13], ip16[14], ip16[15]))
			}
		}
	}
	return false
}
