package main

import (
	"context"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"stubr/internal/config"
	"stubr/internal/logging"
	"stubr/internal/router"
)

func main() {
	var (
		configFile string
		port       int
		host       string
		stubsDir   string
		verbose    bool
	)

	flag.StringVar(&configFile, "config", "stubr.yaml", "Path to YAML config file")
	flag.IntVar(&port, "port", 0, "Override listen port")
	flag.StringVar(&host, "host", "", "Override listen host")
	flag.StringVar(&stubsDir, "dir", "", "Override stubs directory")
	flag.BoolVar(&verbose, "verbose", false, "Enable verbose request logging")
	flag.Parse()

	cfg, err := config.Load(configFile)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error loading config: %v\n", err)
		os.Exit(1)
	}

	cfg.MergeCLI(port, host, stubsDir)

	if verbose {
		cfg.Verbose = true
	}

	r := router.New(cfg)

	addr := fmt.Sprintf("%s:%d", cfg.Host, cfg.Port)
	srv := &http.Server{
		Addr:         addr,
		Handler:      r,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 60 * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		logging.Info("stubr listening", "addr", addr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logging.Error("server error", "error", err)
			os.Exit(1)
		}
	}()

	<-quit
	logging.Info("shutting down...")

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		logging.Error("forced shutdown", "error", err)
		os.Exit(1)
	}

	logging.Info("server stopped")
}
