package main

import (
	"context"
	"flag"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

// contextList collects a repeatable --context flag. It also splits on commas, so
// --context=a,b and --context=a --context=b are equivalent.
type contextList []string

func (c *contextList) String() string { return strings.Join(*c, ",") }

func (c *contextList) Set(v string) error {
	for _, part := range strings.Split(v, ",") {
		if part = strings.TrimSpace(part); part != "" {
			*c = append(*c, part)
		}
	}
	return nil
}

func main() {
	var kubeContexts contextList
	addr := flag.String("addr", ":8080", "Listen address")
	kubeconfig := flag.String("kubeconfig", "", "Path to kubeconfig (defaults to $KUBECONFIG, then ~/.kube/config)")
	flag.Var(&kubeContexts, "context",
		"Kubeconfig context to serve; repeatable or comma-separated. Defaults to every context in the kubeconfig")
	gcpAuth := flag.Bool("gcp-auth", false,
		"Force Google ADC auth for every context; auto-enabled per context that uses gke-gcloud-auth-plugin")
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

	clients, err := NewClientSet(kc, kubeContexts, *gcpAuth)
	if err != nil {
		log.Fatalf("Failed to load kubeconfig: %v", err)
	}
	// Log the served set at startup: individual clients are built on first use,
	// so this is the only place the operator sees which clusters are on offer
	// (and at which endpoint) without making a call.
	for _, info := range clients.Contexts() {
		suffix := ""
		if info.Default {
			suffix = " (default)"
		}
		log.Printf("Serving context %q -> %s%s", info.Name, info.Server, suffix)
	}

	server := NewServer(&Policy{}, clients)
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
