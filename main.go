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
	"github.com/n0remac/Knowledge-Graph/internal/testsuite"
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

	testSuiteService, err := testsuite.NewService(testsuite.ServiceConfig{
		BaseDir:             cfg.TestSuiteBaseDir,
		OllamaBaseURL:       cfg.OllamaBaseURL,
		RequestTimeout:      cfg.TestSuiteTimeout,
		DefaultChatModel:    cfg.OllamaChatModel,
		DefaultExtractModel: cfg.OllamaExtractModel,
		DefaultPersona:      cfg.Persona,
		Telemetry:           cfg.Telemetry,
	})
	if err != nil {
		return fmt.Errorf("failed to initialize test suite service: %w", err)
	}

	mux := http.NewServeMux()
	webapp.Conversation(mux, runtime.ConversationStore(), cfg.Telemetry.BaseDir)
	webapp.TestSuite(mux, testSuiteService)

	listener, err := net.Listen("tcp", cfg.WebAddr)
	if err != nil {
		return fmt.Errorf("failed to bind conversation viewer on %q: %w", cfg.WebAddr, err)
	}
	webServer := &http.Server{Handler: mux}
	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := webServer.Shutdown(shutdownCtx); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Printf("conversation viewer shutdown error: %v", err)
		}
	}()
	go func() {
		log.Printf("conversation viewer listening on %s", conversationViewerURL(listener.Addr().String()))
		if serveErr := webServer.Serve(listener); serveErr != nil && !errors.Is(serveErr, http.ErrServerClosed) {
			log.Printf("conversation viewer server error: %v", serveErr)
		}
	}()

	if err := runtime.Run(); err != nil {
		return fmt.Errorf("runtime error: %w", err)
	}
	return nil
}

func conversationViewerURL(addr string) string {
	if addr == "" {
		return "http://localhost/conversation"
	}
	if addr[0] == ':' {
		return "http://localhost" + addr + "/conversation"
	}
	return "http://" + addr + "/conversation"
}
