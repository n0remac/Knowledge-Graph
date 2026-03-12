package main

import (
	"log"

	"github.com/n0remac/Knowledge-Graph/internal/config"
	"github.com/n0remac/Knowledge-Graph/internal/discordbot"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("configuration error: %v", err)
	}

	runtime, err := discordbot.NewRuntime(cfg)
	if err != nil {
		log.Fatalf("failed to initialize bot runtime: %v", err)
	}
	defer func() {
		if err := runtime.Close(); err != nil {
			log.Printf("error during shutdown cleanup: %v", err)
		}
	}()

	if err := runtime.Run(); err != nil {
		log.Fatalf("runtime error: %v", err)
	}
}
