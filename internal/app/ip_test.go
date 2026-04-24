package app

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestClientIP_PrefersCFConnectingIP(t *testing.T) {
	t.Parallel()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "127.0.0.1:55123"
	req.Header.Set("CF-Connecting-IP", "203.0.113.7")
	req.Header.Set("X-Forwarded-For", "198.51.100.2, 203.0.113.7")
	ip, err := clientIP(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := ip.String(); got != "203.0.113.7" {
		t.Errorf("ip: got %q, want %q", got, "203.0.113.7")
	}
}

func TestClientIP_FallsBackToXForwardedFor(t *testing.T) {
	t.Parallel()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "10.0.0.5:12345"
	req.Header.Set("X-Forwarded-For", "198.51.100.2, 192.0.2.1")
	ip, err := clientIP(req)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	// Leftmost entry in XFF is the original client.
	if got := ip.String(); got != "198.51.100.2" {
		t.Errorf("ip: got %q, want %q", got, "198.51.100.2")
	}
}

func TestClientIP_FallsBackToRemoteAddr(t *testing.T) {
	t.Parallel()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "192.0.2.50:44444"
	ip, err := clientIP(req)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if got := ip.String(); got != "192.0.2.50" {
		t.Errorf("ip: got %q, want %q", got, "192.0.2.50")
	}
}

func TestClientIP_HandlesIPv6RemoteAddr(t *testing.T) {
	t.Parallel()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "[2001:db8::1]:44444"
	ip, err := clientIP(req)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if got := ip.String(); got != "2001:db8::1" {
		t.Errorf("ip: got %q, want %q", got, "2001:db8::1")
	}
}

func TestClientIP_IgnoresBogusHeader(t *testing.T) {
	t.Parallel()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "192.0.2.50:44444"
	req.Header.Set("CF-Connecting-IP", "not-an-ip")
	ip, err := clientIP(req)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if got := ip.String(); got != "192.0.2.50" {
		t.Errorf("should fall back past bogus CF-Connecting-IP: got %q", got)
	}
}
