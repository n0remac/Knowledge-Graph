package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"time"

	"github.com/n0remac/Knowledge-Graph/internal/config"
	"github.com/n0remac/Knowledge-Graph/internal/discordbot"
	webapp "github.com/n0remac/Knowledge-Graph/web"
)

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("configuration error: %w", err)
	}

	runtime, err := discordbot.NewRuntime(cfg)
	if err != nil {
		return fmt.Errorf("failed to initialize bot runtime: %w", err)
	}
	defer func() {
		if err := runtime.Close(); err != nil {
			log.Printf("error during shutdown cleanup: %v", err)
		}
	}()

	mux := http.NewServeMux()
	webapp.Graph(mux, runtime.Store())

	listener, err := net.Listen("tcp", cfg.GraphWebAddr)
	if err != nil {
		return fmt.Errorf("failed to bind graph viewer on %q: %w", cfg.GraphWebAddr, err)
	}
	graphServer := &http.Server{Handler: mux}
	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := graphServer.Shutdown(shutdownCtx); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Printf("graph viewer shutdown error: %v", err)
		}
	}()
	go func() {
		log.Printf("graph viewer listening on %s", graphViewerURL(listener.Addr().String()))
		if serveErr := graphServer.Serve(listener); serveErr != nil && !errors.Is(serveErr, http.ErrServerClosed) {
			log.Printf("graph viewer server error: %v", serveErr)
		}
	}()

	if err := runtime.Run(); err != nil {
		return fmt.Errorf("runtime error: %w", err)
	}
	return nil
}

func graphViewerURL(addr string) string {
	if addr == "" {
		return "http://localhost/graph"
	}
	if addr[0] == ':' {
		return "http://localhost" + addr + "/graph"
	}
	return "http://" + addr + "/graph"
}
