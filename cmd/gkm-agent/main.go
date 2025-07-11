package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/redhat-et/GKM/pkg/gkm-agent/agent"
)

func main() {
	// Create a new context for graceful shutdown
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Setup signal catching
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	log.Println("Starting gkm-agent...")

	// Start the agent functionality
	go func() {
		if err := agent.Start(ctx); err != nil {
			log.Fatalf("Failed to start agent: %v", err)
		}
	}()

	// Wait for a termination signal
	sig := <-sigChan
	log.Printf("Received signal %v, shutting down...", sig)
	cancel()
}
