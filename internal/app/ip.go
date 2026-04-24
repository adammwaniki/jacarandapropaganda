package app

import (
	"errors"
	"net"
	"net/http"
	"net/netip"
	"strings"
)

// clientIP returns the caller's originating IP for rate-limiting purposes.
//
// Priority order:
//  1. CF-Connecting-IP (Cloudflare, our primary deployment path).
//  2. The leftmost entry of X-Forwarded-For (any plain reverse proxy).
//  3. net.SplitHostPort on RemoteAddr (direct TCP in dev).
//
// Headers are trusted because Cloudflare/Caddy are the only ingress in
// production. If we add a non-proxied ingress path, revisit this.
func clientIP(r *http.Request) (netip.Addr, error) {
	if v := r.Header.Get("CF-Connecting-IP"); v != "" {
		if addr, err := netip.ParseAddr(strings.TrimSpace(v)); err == nil {
			return addr, nil
		}
	}
	if v := r.Header.Get("X-Forwarded-For"); v != "" {
		// Leftmost entry is the original client.
		if first := strings.TrimSpace(strings.SplitN(v, ",", 2)[0]); first != "" {
			if addr, err := netip.ParseAddr(first); err == nil {
				return addr, nil
			}
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return netip.Addr{}, errors.New("clientIP: no valid source")
	}
	addr, err := netip.ParseAddr(host)
	if err != nil {
		return netip.Addr{}, errors.New("clientIP: remote addr unparseable")
	}
	return addr, nil
}
