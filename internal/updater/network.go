package updater

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const metadataLimit = 512 << 10
const downloadLimit int64 = 2 << 30

func (u *Updater) client(metadata bool) *http.Client {
	var client http.Client
	if u.cfg.HTTPClient != nil {
		client = *u.cfg.HTTPClient
	}
	if metadata {
		client.Timeout = 5 * time.Second
	} else {
		client.Timeout = 0
	}
	prior := client.CheckRedirect
	client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if err := u.validateURL(req.Context(), req.URL); err != nil {
			return err
		}
		if prior != nil {
			return prior(req, via)
		}
		if len(via) >= 10 {
			return fmt.Errorf("too many redirects")
		}
		return nil
	}
	return &client
}

func (u *Updater) validateURL(ctx context.Context, target *url.URL) error {
	if target == nil || target.Scheme != "https" || target.Hostname() == "" || target.User != nil {
		return coded("source_unreachable", fmt.Errorf("https URL required"))
	}
	host := strings.ToLower(strings.TrimSuffix(target.Hostname(), "."))
	if u.hostAllowed(host) {
		return nil
	}
	addresses, err := net.DefaultResolver.LookupIPAddr(ctx, host)
	if err != nil {
		return coded("source_unreachable", err)
	}
	if len(addresses) == 0 {
		return coded("source_unreachable", fmt.Errorf("host has no address"))
	}
	for _, address := range addresses {
		ip := address.IP
		if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsUnspecified() || ip.IsMulticast() {
			return coded("source_unreachable", fmt.Errorf("host resolves to a rejected address"))
		}
	}
	return nil
}

func (u *Updater) hostAllowed(host string) bool {
	for _, allowed := range u.cfg.AllowedHosts {
		allowed = strings.ToLower(strings.TrimSpace(strings.TrimSuffix(allowed, ".")))
		if strings.HasPrefix(allowed, "*.") {
			suffix := strings.TrimPrefix(allowed, "*")
			if strings.HasSuffix(host, suffix) && host != strings.TrimPrefix(suffix, ".") {
				return true
			}
		} else if host == allowed {
			return true
		}
	}
	return false
}

func (u *Updater) request(ctx context.Context, method, rawURL string, headers http.Header, metadata bool) (*http.Response, error) {
	target, err := url.Parse(rawURL)
	if err != nil {
		return nil, coded("source_unreachable", err)
	}
	if err := u.validateURL(ctx, target); err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, method, target.String(), nil)
	if err != nil {
		return nil, coded("source_unreachable", err)
	}
	for key, values := range headers {
		for _, value := range values {
			req.Header.Add(key, value)
		}
	}
	req.Header.Set("Accept", "application/vnd.github+json, application/json")
	req.Header.Set("User-Agent", "nex-updater")
	response, err := u.client(metadata).Do(req)
	if err != nil {
		return nil, coded("source_unreachable", err)
	}
	return response, nil
}

func readLimited(body io.Reader, limit int64) ([]byte, error) {
	reader := io.LimitReader(body, limit+1)
	data, err := io.ReadAll(reader)
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("response exceeds limit")
	}
	return data, nil
}
