// Command bffd serves the app facing endpoints.
package main

import (
	"context"
	"errors"
	"flag"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/usmanvirk/mobile-bff/internal/bff"
	"github.com/usmanvirk/mobile-bff/internal/cache"
	"github.com/usmanvirk/mobile-bff/internal/upstream"
)

func main() {
	var (
		addr     = flag.String("addr", ":8080", "listen address")
		redisURL = flag.String("redis-url", os.Getenv("REDIS_URL"), "Redis DSN, empty for an in process cache")
		fakeSize = flag.Int("fake-catalogue", 200, "number of generated listings to serve, for running without the real catalogue")
		budget   = flag.Bool("enforce-budget", false, "refuse a response that is over its size budget instead of logging it")
	)
	flag.Parse()

	log := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	var c cache.Cache
	if *redisURL == "" {
		log.Warn("no redis-url, using an in process cache; two replicas will not share it")
		c = cache.NewMemory()
	} else {
		r, err := cache.NewRedis(*redisURL)
		if err != nil {
			log.Error("redis", "err", err)
			os.Exit(1)
		}
		pingCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
		err = r.Ping(pingCtx)
		cancel()
		if err != nil {
			// Not fatal. A cache that is down should make the service slow
			// and not stop it from starting, and the same reasoning applies
			// at boot as it does per request.
			log.Warn("redis did not answer, starting anyway and serving from upstream", "err", err)
		}
		c = r
		defer r.Close()
	}

	// The real deployment points this at the catalogue service. The
	// generated one is here so the binary runs and can be looked at.
	cat := upstream.NewFake(*fakeSize)

	h := &bff.Handler{Catalogue: cat, Cache: c, Log: log, EnforceBudget: *budget}

	srv := &http.Server{
		Addr:              *addr,
		Handler:           h.Routes(),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		// Longer than the upstream timeout, so a slow upstream produces a
		// 504 from the handler rather than the server cutting the
		// connection with nothing written.
		WriteTimeout: bff.UpstreamTimeout + 5*time.Second,
		IdleTimeout:  60 * time.Second,
	}

	go func() {
		log.Info("listening", "addr", *addr, "listings", *fakeSize)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Error("serve", "err", err)
			stop()
		}
	}()

	<-ctx.Done()
	log.Info("draining")
	shutCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutCtx); err != nil {
		log.Error("shutdown", "err", err)
	}
}
