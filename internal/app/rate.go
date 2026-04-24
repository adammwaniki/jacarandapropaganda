package app

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/netip"

	"github.com/google/uuid"

	"github.com/adammwaniki/jacarandapropaganda/internal/rate"
)

// RateLimiter is the subset of *rate.Limiter the app layer needs. A stub
// satisfies it in unit tests; production passes the real limiter.
type RateLimiter interface {
	CheckAndRecordTreeCreate(ctx context.Context, device uuid.UUID, ip netip.Addr) error
	CheckAndRecordObservationCreate(ctx context.Context, device uuid.UUID) error
}

// enforceRateLimit runs the limiter check if one is configured. It returns
//   - (false, nil): caller should stop, response already written with 429.
//   - (true, nil):  caller should continue.
//   - (false, err): internal error; caller should write 500.
//
// Centralizing the 429/500 decision here keeps the handlers tidy.
func enforceRateLimit(w http.ResponseWriter, r *http.Request, run func() error) (ok bool) {
	if err := run(); err != nil {
		var limErr rate.LimitedError
		if errors.As(err, &limErr) {
			write429(w, r, limErr)
			return false
		}
		slog.ErrorContext(r.Context(), "rate limiter internal error", "err", err)
		writeError(w, http.StatusInternalServerError, "rate check failed")
		return false
	}
	return true
}

func write429(w http.ResponseWriter, r *http.Request, err rate.LimitedError) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Retry-After", "86400") // 24h; rolling window is per-event but a crude hint helps automated clients
	w.WriteHeader(http.StatusTooManyRequests)
	var buf bytes.Buffer
	data := map[string]any{
		"Kind":  string(err.Kind),
		"Scope": string(err.Scope),
		"Limit": err.Limit,
	}
	if rerr := renderFragment(&buf, "rate_limited.html", data); rerr != nil {
		slog.ErrorContext(r.Context(), "render rate_limited fragment", "err", rerr)
		return
	}
	_, _ = w.Write(buf.Bytes())
}
