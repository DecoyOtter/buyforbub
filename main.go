// Command buyforbub serves the pre-baby shopping checklist.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/DecoyOtter/buyforbub/internal/store"
	"github.com/DecoyOtter/buyforbub/internal/web"
)

func main() {
	// The image is distroless, so it has no shell or curl for Docker's
	// HEALTHCHECK to use — the binary checks itself instead.
	if len(os.Args) > 1 && os.Args[1] == "-healthcheck" {
		if err := healthcheck("http://127.0.0.1:" + env("PORT", "8080") + "/healthz"); err != nil {
			fmt.Fprintln(os.Stderr, "unhealthy:", err)
			os.Exit(1)
		}
		return
	}

	log := slog.New(slog.NewTextHandler(os.Stdout, nil))
	if err := run(log); err != nil {
		log.Error("fatal", "error", err)
		os.Exit(1)
	}
}

func healthcheck(url string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%s returned %s", url, resp.Status)
	}
	return nil
}

func run(log *slog.Logger) error {
	var (
		addr   = ":" + env("PORT", "8080")
		dbPath = env("DB_PATH", "/data/buyforbub.db")
		title  = env("APP_TITLE", "Buy for Bub")
	)

	st, err := store.Open(dbPath)
	if err != nil {
		return err
	}
	defer st.Close()
	log.Info("database ready", "path", dbPath)

	seed, err := loadSeed()
	if err != nil {
		return err
	}
	n, err := st.SeedIfEmpty(context.Background(), seed)
	if err != nil {
		return err
	}
	if n > 0 {
		log.Info("seeded default checklist", "items", n)
	}

	srv, err := web.New(st, title, log)
	if err != nil {
		return err
	}

	httpSrv := &http.Server{
		Addr:              addr,
		Handler:           srv,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	// Shut down cleanly so SQLite is closed properly on `docker stop`.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	errc := make(chan error, 1)
	go func() {
		log.Info("listening", "addr", addr)
		if err := httpSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errc <- err
		}
	}()

	select {
	case err := <-errc:
		return err
	case <-ctx.Done():
		log.Info("shutting down")
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return httpSrv.Shutdown(shutdownCtx)
}

// env reads a variable, falling back when it is unset or blank.
func env(key, fallback string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return fallback
}
