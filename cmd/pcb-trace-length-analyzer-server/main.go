// Command pcb-trace-length-analyzer-server runs the control plane on its own, so the web
// front end can be developed and the HTTP surface exercised without
// embeddedci-server.
//
// It is a development harness, not a deployment target. Production mounts the
// server package into embeddedci-server instead, behind that application's own
// authentication; here there is none, and the identity every request runs
// under is whatever -user says. That is why it binds to localhost by default.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"time"

	"github.com/embeddedci-com/pcb-autorouter/server"
)

func main() {
	addr := flag.String("addr", "127.0.0.1:8091", "address to listen on")
	user := flag.String("user", "dev", "user id every request runs as")
	org := flag.String("org", "dev-org", "organization id every request runs as")
	ttl := flag.Duration("session-ttl", server.DefaultSessionTTL, "how long an uploaded board is kept")
	maxUpload := flag.Int64("max-upload", server.DefaultMaxUploadBytes, "largest accepted upload, in bytes")
	webappDir := flag.String("webapp", "",
		"serve a built webapp bundle from this directory, e.g. webapp/dist (same origin, so no CORS needed)")
	origin := flag.String("cors-origin", "loopback",
		`allow browser requests from this origin; "loopback" allows any http://localhost:<port> or http://127.0.0.1:<port>, "" disables CORS`)
	flag.Parse()

	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))

	svc := server.New(server.Deps{
		Store:          server.NewMemoryStore(nil),
		Logger:         log,
		SessionTTL:     *ttl,
		MaxUploadBytes: *maxUpload,
	})

	mux := http.NewServeMux()
	// Stand in for the host's authentication. embeddedci-server resolves a real
	// session here; this hands every request the same made-up identity, which
	// is the one thing about this harness that must never reach production.
	asDevUser := func(h http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			ctx := server.WithUser(r.Context(), server.UserIdentity{
				UserID: *user, OrganizationID: *org, Login: *user,
			})
			h(w, r.WithContext(ctx))
		}
	}
	svc.Mount(mux, "/api", asDevUser)
	mux.HandleFunc("GET /api/health", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})

	if *webappDir != "" {
		spa, err := spaHandler(*webappDir)
		if err != nil {
			log.Error("cannot serve the webapp bundle", "dir", *webappDir, "err", err)
			fmt.Fprintf(os.Stderr, "\n  build it first:  npm --prefix webapp run build\n\n")
			os.Exit(1)
		}
		mux.Handle("/", spa)
	}

	handler := withCORS(*origin, mux)
	srv := &http.Server{
		Addr:    *addr,
		Handler: handler,
		// A large board upload over a slow link needs room; the read timeout
		// is what stops a stalled connection holding a worker forever.
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       5 * time.Minute,
		WriteTimeout:      5 * time.Minute,
		IdleTimeout:       2 * time.Minute,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	go func() {
		log.Info("pcb-trace-length-analyzer-server listening",
			"addr", *addr, "user", *user, "cors_origin", *origin)
		fmt.Fprintf(os.Stderr, "\n  health   http://%s/api/health\n", *addr)
		fmt.Fprintf(os.Stderr, "  upload   POST http://%s/api/pcb-trace-length-analyzer/sessions  (multipart: board, project, rules)\n", *addr)
		if *webappDir != "" {
			fmt.Fprintf(os.Stderr, "  app      http://%s/tools/pcb-trace-length-analyzer\n", *addr)
		}
		fmt.Fprintln(os.Stderr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Error("listen failed", "err", err)
			os.Exit(1)
		}
	}()

	<-ctx.Done()
	log.Info("shutting down")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Error("shutdown", "err", err)
	}
}

// withCORS lets a Vite dev server on another port talk to this one.
//
// "loopback" reflects back any origin on localhost, because the dev server's
// port is not knowable in advance -- it is whichever one was free. That is
// still not a wildcard: a request from anywhere but the machine's own loopback
// interface gets no CORS headers at all, and it only ever applies to this
// harness. Production is same-origin, because the tool is mounted inside
// embeddedci-server rather than served beside it.
func withCORS(origin string, next http.Handler) http.Handler {
	if origin == "" {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		allow := origin
		if origin == "loopback" {
			allow = ""
			if got := r.Header.Get("Origin"); isLoopbackOrigin(got) {
				allow = got
			}
		}
		if allow != "" {
			w.Header().Set("Access-Control-Allow-Origin", allow)
			w.Header().Set("Access-Control-Allow-Credentials", "true")
			w.Header().Set("Vary", "Origin")
		}
		if r.Method == http.MethodOptions {
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, DELETE, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
			w.Header().Set("Access-Control-Max-Age", "600")
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// isLoopbackOrigin reports whether an Origin header names this machine.
//
// The host is compared after stripping the port, and only http is accepted,
// because that is all a local Vite server speaks. Anything that does not parse
// as a URL, or whose host is not a loopback address, is refused rather than
// guessed at.
func isLoopbackOrigin(origin string) bool {
	if origin == "" {
		return false
	}
	u, err := url.Parse(origin)
	if err != nil || u.Scheme != "http" {
		return false
	}
	host := u.Hostname()
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
