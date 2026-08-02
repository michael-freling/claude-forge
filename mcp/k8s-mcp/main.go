package main

import (
	"context"
	"flag"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"
)

func main() {
	addr := flag.String("addr", ":8080", "Listen address")
	kubeconfig := flag.String("kubeconfig", "", "Path to kubeconfig (defaults to $KUBECONFIG, then ~/.kube/config)")
	kubeContext := flag.String("context", "", "Kubeconfig context to use (defaults to current-context)")
	gcpAuth := flag.Bool("gcp-auth", false,
		"Force Google ADC auth; auto-enabled when the kubeconfig uses gke-gcloud-auth-plugin")
	flag.Parse()

	kc := *kubeconfig
	if kc == "" {
		kc = os.Getenv("KUBECONFIG")
	}
	if kc == "" {
		if home, err := os.UserHomeDir(); err == nil {
			kc = filepath.Join(home, ".kube", "config")
		}
	}

	client, err := NewClient(kc, *kubeContext, *gcpAuth)
	if err != nil {
		log.Fatalf("Failed to initialize Kubernetes client: %v", err)
	}

	server := NewServer(&Policy{}, client)
	mux := http.NewServeMux()
	mux.Handle("/mcp", server)
	httpServer := &http.Server{Addr: *addr, Handler: mux}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	go func() {
		log.Printf("Kubernetes MCP starting: addr=%s kubeconfig=%s", *addr, kc)
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
