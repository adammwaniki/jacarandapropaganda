package app

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/google/uuid"
)

// DeviceCookieName is the cookie the middleware uses to carry the device
// identity across requests. The value is a UUIDv4 string.
const DeviceCookieName = "jp_device"

// DeviceCookieMaxAge is a decade. The cookie is not a secret — it is the
// device's only identity — so rotating it on a short clock would just mean
// users become "new" for no product reason. Clearing cookies still works.
const DeviceCookieMaxAge = 10 * 365 * 24 * 60 * 60

type ctxKey int

const deviceCtxKey ctxKey = 1

// DeviceUpserter is the subset of the store used by the middleware. A
// package-local interface keeps the app package free of a direct pgx
// dependency and makes the middleware straightforward to test.
type DeviceUpserter interface {
	Upsert(ctx context.Context, id uuid.UUID) error
}

// WithDevice returns middleware that ensures every request carries a
// validated device ID, stored in the request context and in a long-lived
// cookie. On first visit (or when the cookie is missing/malformed/not v4)
// it issues a new UUIDv4. Every request upserts the row in `devices`.
func WithDevice(store DeviceUpserter) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			id, fromCookie := parseDeviceCookie(r)
			if !fromCookie {
				id = uuid.New() // v4
				setDeviceCookie(w, r, id)
			}

			if err := store.Upsert(r.Context(), id); err != nil {
				// A device upsert failure is worth logging but must not
				// fail the request — a broken device table should not
				// prevent someone from viewing the map.
				slog.WarnContext(r.Context(), "device upsert failed",
					"err", err, "device_id", id)
			}

			ctx := context.WithValue(r.Context(), deviceCtxKey, id)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// DeviceFromContext returns the device ID stamped by the middleware. The
// second return is false if the middleware did not run (e.g. /health).
func DeviceFromContext(ctx context.Context) (uuid.UUID, bool) {
	id, ok := ctx.Value(deviceCtxKey).(uuid.UUID)
	return id, ok
}

// parseDeviceCookie returns (id, true) if the request carries a well-formed
// UUIDv4 cookie. Anything else (missing, malformed, other version) returns
// (_, false) so the caller issues a fresh id.
func parseDeviceCookie(r *http.Request) (uuid.UUID, bool) {
	c, err := r.Cookie(DeviceCookieName)
	if err != nil {
		return uuid.Nil, false
	}
	id, err := uuid.Parse(c.Value)
	if err != nil {
		return uuid.Nil, false
	}
	if id.Version() != 4 {
		return uuid.Nil, false
	}
	return id, true
}

func setDeviceCookie(w http.ResponseWriter, r *http.Request, id uuid.UUID) {
	http.SetCookie(w, &http.Cookie{
		Name:     DeviceCookieName,
		Value:    id.String(),
		Path:     "/",
		MaxAge:   DeviceCookieMaxAge,
		HttpOnly: true,
		Secure:   isHTTPS(r),
		SameSite: http.SameSiteLaxMode,
	})
}

// isHTTPS returns true when the request reached us over TLS, either
// directly or via a trusted proxy (Cloudflare/Caddy) setting
// X-Forwarded-Proto. Secure=true on plain HTTP would make browsers drop
// the cookie in local dev, so we only set it when we're really on HTTPS.
func isHTTPS(r *http.Request) bool {
	if r.TLS != nil {
		return true
	}
	return r.Header.Get("X-Forwarded-Proto") == "https"
}
