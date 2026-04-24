//go:build integration

package app_test

import (
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"testing"
)

func cookieJar() (http.CookieJar, error) {
	return cookiejar.New(nil)
}

func parseURL(t *testing.T, s string) *url.URL {
	t.Helper()
	u, err := url.Parse(s)
	if err != nil {
		t.Fatalf("parse url %q: %v", s, err)
	}
	return u
}
