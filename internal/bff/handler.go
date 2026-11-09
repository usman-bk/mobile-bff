// Package bff serves the app facing endpoints.
package bff

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/usmanvirk/mobile-bff/internal/cache"
	"github.com/usmanvirk/mobile-bff/internal/shape"
	"github.com/usmanvirk/mobile-bff/internal/upstream"
)

const (
	// DefaultLimit and MaxLimit bound a page. The maximum is not a round
	// number for its own sake: 20 cards is a screen and a half on the
	// largest phone, and a client asking for 500 is paginating badly rather
	// than needing 500.
	DefaultLimit = 20
	MaxLimit     = 50

	// UpstreamTimeout is shorter than any client timeout, so that a slow
	// upstream produces a clear 504 from here rather than a connection the
	// phone gives up on with nothing to show.
	UpstreamTimeout = 2 * time.Second

	ListTTL   = 60 * time.Second
	DetailTTL = 5 * time.Minute
)

type Handler struct {
	Catalogue upstream.Catalogue
	Cache     cache.Cache
	Log       *slog.Logger

	// EnforceBudget makes an over budget response a server error instead of
	// a log line. Off in production and on in tests: a customer whose page
	// is two kilobytes over should still see their page, and a build that
	// produces one should fail.
	EnforceBudget bool
}

func (h *Handler) Routes() *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/list", h.list)
	mux.HandleFunc("GET /v1/listings/{id}", h.detail)
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok\n"))
	})
	return mux
}

func (h *Handler) device(r *http.Request) shape.Device {
	return shape.ParseDevice(
		r.Header.Get("X-Viewport-Width"),
		r.Header.Get("X-Viewport-Scale"),
		r.Header.Get("Accept"),
	)
}

func writeErr(w http.ResponseWriter, code int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": msg})
}

// writeCached writes a response that came out of the cache or was just built.
func writeCached(w http.ResponseWriter, body []byte, hit bool) {
	w.Header().Set("Content-Type", "application/json")
	if hit {
		w.Header().Set("X-Cache", "hit")
	} else {
		w.Header().Set("X-Cache", "miss")
	}
	w.Header().Set("Content-Length", strconv.Itoa(len(body)))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(body)
}

func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query().Get("q")
	limit := DefaultLimit
	if raw := r.URL.Query().Get("limit"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 {
			writeErr(w, http.StatusBadRequest, "limit must be a positive number")
			return
		}
		// Clamped rather than refused. A client asking for 500 gets 50 and a
		// working screen, which is better than an error it has no handling
		// for, and the page token tells it there is more.
		if n > MaxLimit {
			n = MaxLimit
		}
		limit = n
	}

	dev := h.device(r)
	bucket := shape.BucketOf(dev)
	key := shape.Key(shape.ScreenList, bucket, dev, q, strconv.Itoa(limit))

	if body, hit := h.fromCache(r.Context(), key); hit {
		writeCached(w, body, true)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), UpstreamTimeout)
	defer cancel()

	listings, err := h.Catalogue.Search(ctx, q, limit)
	if err != nil {
		h.upstreamError(w, r, "search", err)
		return
	}

	// Shaped for the bucket's canonical device, not for this exact one, or
	// the entry about to be cached is only correct for one caller.
	cards := shape.Cards(listings, bucket.Canonical(dev))
	body, err := json.Marshal(cards)
	if err != nil {
		h.Log.Error("encoding list", "err", err)
		writeErr(w, http.StatusInternalServerError, "could not build the response")
		return
	}

	if err := h.checkBudget(shape.ScreenList, len(body), len(cards)); err != nil {
		if h.EnforceBudget {
			writeErr(w, http.StatusInternalServerError, err.Error())
			return
		}
		h.Log.Error("response over budget", "err", err, "screen", shape.ScreenList)
	}

	h.toCache(r.Context(), key, body, ListTTL)
	writeCached(w, body, false)
}

func (h *Handler) detail(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		writeErr(w, http.StatusBadRequest, "no id")
		return
	}

	dev := h.device(r)
	bucket := shape.BucketOf(dev)
	key := shape.Key(shape.ScreenDetail, bucket, dev, id)

	if body, hit := h.fromCache(r.Context(), key); hit {
		writeCached(w, body, true)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), UpstreamTimeout)
	defer cancel()

	l, err := h.Catalogue.Get(ctx, id)
	if err != nil {
		if errors.Is(err, upstream.ErrNotFound) {
			writeErr(w, http.StatusNotFound, "no such listing")
			return
		}
		h.upstreamError(w, r, "get", err)
		return
	}

	d := shape.ToDetail(l, bucket.Canonical(dev))
	body, err := json.Marshal(d)
	if err != nil {
		h.Log.Error("encoding detail", "err", err)
		writeErr(w, http.StatusInternalServerError, "could not build the response")
		return
	}

	if err := h.checkBudget(shape.ScreenDetail, len(body), 1); err != nil {
		if h.EnforceBudget {
			writeErr(w, http.StatusInternalServerError, err.Error())
			return
		}
		h.Log.Error("response over budget", "err", err, "screen", shape.ScreenDetail, "id", id)
	}

	h.toCache(r.Context(), key, body, DetailTTL)
	writeCached(w, body, false)
}

func (h *Handler) checkBudget(s shape.Screen, size, items int) error {
	return shape.Check(s, size, items)
}

// fromCache treats any cache error as a miss.
//
// A cache is the one dependency a request should survive losing. Redis being
// down should make the app slow, not broken, so the error is logged and the
// request carries on to the upstream. The alternative, failing the request,
// turns a cache outage into a full outage.
func (h *Handler) fromCache(ctx context.Context, key string) ([]byte, bool) {
	if h.Cache == nil {
		return nil, false
	}
	body, hit, err := h.Cache.Get(ctx, key)
	if err != nil {
		h.Log.Warn("cache read failed, serving from upstream", "err", err)
		return nil, false
	}
	return body, hit
}

func (h *Handler) toCache(ctx context.Context, key string, body []byte, ttl time.Duration) {
	if h.Cache == nil {
		return
	}
	if err := h.Cache.Set(ctx, key, body, ttl); err != nil {
		h.Log.Warn("cache write failed", "err", err)
	}
}

func (h *Handler) upstreamError(w http.ResponseWriter, r *http.Request, op string, err error) {
	// A timeout is distinguished from a failure, because they are different
	// problems and the app can retry one of them sensibly.
	if errors.Is(err, context.DeadlineExceeded) {
		h.Log.Error("upstream timed out", "op", op, "timeout", UpstreamTimeout.String())
		writeErr(w, http.StatusGatewayTimeout, "the catalogue did not answer in time")
		return
	}
	if errors.Is(err, context.Canceled) || r.Context().Err() != nil {
		// The client hung up. Nothing to write, and not worth an error log.
		h.Log.Debug("client went away", "op", op)
		return
	}
	h.Log.Error("upstream failed", "op", op, "err", err)
	writeErr(w, http.StatusBadGateway, "the catalogue is unavailable")
}
