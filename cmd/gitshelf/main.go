// Command gitshelf is a read-only, in-place Git repository browser.
//
// It serves existing bare/mirror repositories directly from disk (zero copy)
// with GitHub-style file previews, commit history and optional issues/PRs/
// releases metadata. Single binary, config-driven, binds 127.0.0.1 by default.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"runtime/debug"
	"syscall"
	"time"

	"github.com/Loping151/gitshelf/internal/config"
	"github.com/Loping151/gitshelf/internal/web"
)

func main() {
	var (
		cfgPath     = flag.String("config", "", "path to config file (TOML)")
		bind        = flag.String("bind", "", "override server.bind, e.g. 127.0.0.1:8888")
		showVersion = flag.Bool("version", false, "print version and exit")
	)
	flag.Parse()

	if *showVersion {
		fmt.Println("gitshelf", versionString())
		return
	}

	cfg, err := config.Load(*cfgPath)
	if err != nil {
		log.Fatalf("config: %v", err)
	}
	if *bind != "" {
		cfg.Server.Bind = *bind
	}

	srv, err := web.New(cfg)
	if err != nil {
		log.Fatalf("startup: %v", err)
	}

	log.Printf("gitshelf %s — discovered %d repositories", versionString(), len(srv.Repos()))
	if cfg.Auth.Enabled {
		log.Printf("auth: enabled (first-run setup at /setup if not yet configured)")
	}

	httpSrv := &http.Server{
		Addr:              cfg.Server.Bind,
		Handler:           srv.Handler(),
		ReadHeaderTimeout: 15 * time.Second,
	}

	go func() {
		log.Printf("listening on http://%s%s/", cfg.Server.Bind, cfg.Server.BasePath)
		if err := httpSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("serve: %v", err)
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	<-stop
	log.Println("shutting down…")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = httpSrv.Shutdown(ctx)
}

// version is set at build time via -ldflags "-X main.version=v0.1.0".
var version string

func versionString() string {
	if version != "" {
		return version
	}
	if info, ok := debug.ReadBuildInfo(); ok && info.Main.Version != "" && info.Main.Version != "(devel)" {
		return info.Main.Version
	}
	return "dev"
}
