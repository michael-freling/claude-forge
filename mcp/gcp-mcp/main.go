package main

import (
	"context"
	"flag"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
)

func main() {
	addr := flag.String("addr", ":8084", "Listen address")
	project := flag.String("project", "",
		"Default project for tools that omit a \"project\" argument. Optional: omit it to require an explicit "+
			"project per call, so any project the credential can access can be targeted.")
	quotaProject := flag.String("quota-project", "",
		"Fixed project billed for quota (x-goog-user-project). Default: bill each call's target project.")
	impersonate := flag.String("impersonate-service-account", "",
		"Service account to impersonate (falls back to CLOUDSDK_AUTH_IMPERSONATE_SERVICE_ACCOUNT)")
	allowWrites := flag.Bool("allow-writes", false, "Allow mutating (write) tools; off by default")
	allowSecretAccess := flag.Bool("allow-secret-access", false, "Allow returning Secret Manager payloads; off by default")
	flag.Parse()

	ctx := context.Background()

	ts, err := NewADCTokenSource(ctx)
	if err != nil {
		log.Fatalf("Failed to initialize Google Cloud credentials: %v", err)
	}
	if target := resolveImpersonationTarget(*impersonate); target != "" {
		log.Printf("Impersonating service account: %s", target)
		ts = newImpersonatedTokenSource(ts, target, nil)
	}

	// Do not default the quota project to --project here: when it is empty each
	// call bills the project it targets (see executeTool), which is what makes
	// cross-project use bill correctly. --quota-project only fixes a single
	// billing project for every call.
	client := NewClient(ts, *project, *quotaProject)
	policy := &Policy{AllowWrites: *allowWrites, AllowSecretAccess: *allowSecretAccess}
	server := NewServer(policy, client)

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
		log.Printf("GCP MCP starting: addr=%s project=%s writes=%v secret_access=%v",
			*addr, *project, *allowWrites, *allowSecretAccess)
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
