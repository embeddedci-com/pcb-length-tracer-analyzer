// Command pcb-trace-length-analyzer-engine is the analyzer as the KiCad plugin
// runs it: the same control plane embeddedci.com mounts, served over stdin and
// stdout instead of a socket.
//
// The plugin starts it as a child process, sends it requests framed the way
// package pipehttp describes, and reads the answers back. Nothing listens on a
// port, so nothing else on the machine can reach it, and there is no port to
// pick, find free, or have a firewall prompt about. When the plugin exits, the
// pipe closes and so does this.
//
// Every request runs as one local identity: the only person who can write to
// this process's stdin is the one who started it.
//
// stdout carries frames and nothing else. Logs go to stderr.
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"time"

	"github.com/embeddedci-com/pcb-autorouter/pipehttp"
	"github.com/embeddedci-com/pcb-autorouter/server"
)

// version is set at build time with -ldflags "-X main.version=...".
var version = "dev"

func main() {
	showVersion := flag.Bool("version", false, "print the version and exit")
	verbose := flag.Bool("v", false, "log every request to stderr")
	flag.Parse()
	if *showVersion {
		fmt.Println(version)
		return
	}

	level := slog.LevelWarn
	if *verbose {
		level = slog.LevelInfo
	}
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level}))

	svc := server.New(server.Deps{
		Store:  server.NewMemoryStore(nil),
		Logger: log,
		// A board open in the editor is kept for as long as the plugin runs;
		// the TTL only has to outlast a long afternoon.
		SessionTTL: 24 * time.Hour,
		// The board comes from the user's own editor, not from the internet.
		// The limit is only there to catch a corrupt stream.
		MaxUploadBytes: 256 << 20,
	})

	local := server.UserIdentity{UserID: "kicad", OrganizationID: "local", Login: "kicad"}
	mux := http.NewServeMux()
	svc.Mount(mux, "/api", func(h http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			h(w, r.WithContext(server.WithUser(r.Context(), local)))
		}
	})
	mux.HandleFunc("GET /api/health", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintf(w, `{"status":"ok","version":%q}`, version)
	})

	var handler http.Handler = mux
	if *verbose {
		handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			mux.ServeHTTP(w, r)
			log.Info("request", "method", r.Method, "path", r.URL.Path, "took", time.Since(start))
		})
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	if err := pipehttp.Serve(ctx, handler, os.Stdin, os.Stdout); err != nil {
		log.Error("engine stopped", "err", err)
		os.Exit(1)
	}
}
