package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
)

func main() {
	owner := flag.String("owner", "", "Allowed GitHub repository owner (required)")
	repo := flag.String("repo", "", "Allowed GitHub repository name (required)")
	addr := flag.String("addr", ":8083", "Listen address")
	flag.Parse()

	if *owner == "" || *repo == "" {
		fmt.Fprintln(os.Stderr, "error: --owner and --repo are required")
		flag.Usage()
		os.Exit(1)
	}

	auth, err := NewGitHubAuth()
	if err != nil {
		log.Fatalf("Failed to initialize GitHub auth: %v", err)
	}

	client := NewGitHubClient(auth)
	policy := &Policy{AllowedOwner: *owner, AllowedRepo: *repo}
	server := NewServer(*owner, *repo, policy, client)

	mux := http.NewServeMux()
	mux.Handle("/mcp", server)

	httpServer := &http.Server{
		Addr:    *addr,
		Handler: mux,
	}

	// Graceful shutdown on SIGINT/SIGTERM.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	go func() {
		log.Printf("GitHub MCP starting: addr=%s owner=%s repo=%s", *addr, *owner, *repo)
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Server error: %v", err)
		}
	}()

	<-ctx.Done()
	log.Println("Shutting down...")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := httpServer.Shutdown(shutdownCtx); err != nil {
		log.Fatalf("Shutdown error: %v", err)
	}

	log.Println("Server stopped")
}
