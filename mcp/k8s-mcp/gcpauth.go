package main

import (
	"context"
	"fmt"
	"strings"

	"golang.org/x/oauth2/google"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/transport"
)

// cloudPlatformScope is the OAuth scope every Google Cloud API accepts; the GKE
// API server accepts any Google OAuth2 access token minted for it as a Bearer
// token.
const cloudPlatformScope = "https://www.googleapis.com/auth/cloud-platform"

// gkeAuthPluginCommand is the exec plugin GKE kubeconfigs authenticate with.
// The binary is not present in the container, but it merely prints a Google
// OAuth2 access token — which we can mint in-process from ADC instead.
const gkeAuthPluginCommand = "gke-gcloud-auth-plugin"

// findDefaultCredentials resolves Application Default Credentials. A variable
// so tests can inject fake credentials without real ADC on disk.
var findDefaultCredentials = google.FindDefaultCredentials

// usesGKEAuthPlugin reports whether the auth-info of the selected (or current)
// context in the kubeconfig authenticates via the gke-gcloud-auth-plugin exec
// plugin. Any load or lookup failure returns false; NewClient surfaces those
// errors on its own.
func usesGKEAuthPlugin(kubeconfigPath, kubeContext string) bool {
	cfg, err := clientcmd.LoadFromFile(kubeconfigPath)
	if err != nil {
		return false
	}
	name := kubeContext
	if name == "" {
		name = cfg.CurrentContext
	}
	kctx, ok := cfg.Contexts[name]
	if !ok {
		return false
	}
	auth, ok := cfg.AuthInfos[kctx.AuthInfo]
	if !ok || auth.Exec == nil {
		return false
	}
	return strings.Contains(auth.Exec.Command, gkeAuthPluginCommand)
}

// applyGCPAuth replaces whatever auth the kubeconfig declared with Google
// Application Default Credentials: requests carry a Bearer access token minted
// in-process, and because ADC holds a refresh token the token source
// auto-refreshes — no gke-gcloud-auth-plugin binary needed, and no 1-hour
// expiry. The cluster server URL and CA data still come from the kubeconfig.
func applyGCPAuth(ctx context.Context, restConfig *rest.Config) error {
	creds, err := findDefaultCredentials(ctx, cloudPlatformScope)
	if err != nil {
		return fmt.Errorf("could not load Application Default Credentials "+
			"(run `gcloud auth application-default login` on the host and mount ~/.config/gcloud): %w", err)
	}
	// Strip whatever auth the kubeconfig declared — ADC replaces it.
	restConfig.ExecProvider = nil
	restConfig.AuthProvider = nil
	restConfig.BearerToken = ""
	restConfig.BearerTokenFile = ""
	restConfig.WrapTransport = transport.TokenSourceWrapTransport(creds.TokenSource)
	return nil
}
