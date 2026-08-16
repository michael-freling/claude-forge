package main

import (
	"fmt"
	"slices"
	"strings"
	"sync"

	"k8s.io/client-go/tools/clientcmd"
)

// ContextInfo describes one kubeconfig context the server can target. It is the
// payload of the list_contexts tool, and it carries the cluster endpoint on
// purpose: "which cluster am I actually talking to" and "why is that one
// unreachable" are the two questions this server gets asked, and neither is
// answerable from a context name alone.
type ContextInfo struct {
	Name    string `json:"name"`
	Cluster string `json:"cluster,omitempty"`
	Server  string `json:"server,omitempty"`
	Default bool   `json:"default,omitempty"`
}

// clientFactory builds a Client for a single context. It is a field on ClientSet
// so tests can substitute a fake without a reachable cluster.
type clientFactory func(kubeconfigPath, kubeContext string, gcpAuth bool) (*Client, error)

// ClientSet serves one Client per kubeconfig context. Every context in the
// kubeconfig is served unless an explicit allowlist narrows the set.
//
// Clients are built lazily on first use and then cached, which matters for more
// than latency: a kubeconfig routinely mixes reachable and unreachable clusters
// (a laptop that is off-VPN, a local cluster that is not running), and neither
// the server's startup nor the working contexts may depend on the broken ones.
// A build failure is reported to the caller and deliberately not cached, so a
// context that recovers starts working without restarting the server.
type ClientSet struct {
	kubeconfigPath string
	gcpAuth        bool
	defaultContext string
	infos          []ContextInfo
	newClient      clientFactory

	mu      sync.Mutex
	clients map[string]*Client
}

// NewClientSet enumerates the kubeconfig's contexts and prepares to serve them.
// When allow is non-empty it narrows the served set to those names; every entry
// must exist in the kubeconfig, so a typo fails loudly at startup rather than
// silently serving fewer clusters than intended.
func NewClientSet(kubeconfigPath string, allow []string, gcpAuth bool) (*ClientSet, error) {
	cfg, err := clientcmd.LoadFromFile(kubeconfigPath)
	if err != nil {
		return nil, fmt.Errorf("failed to load kubeconfig %s: %w", kubeconfigPath, err)
	}

	all := make([]string, 0, len(cfg.Contexts))
	for name := range cfg.Contexts {
		all = append(all, name)
	}
	slices.Sort(all)

	names := all
	if len(allow) > 0 {
		var missing []string
		for _, want := range allow {
			if _, ok := cfg.Contexts[want]; !ok {
				missing = append(missing, want)
			}
		}
		if len(missing) > 0 {
			return nil, fmt.Errorf("kubeconfig %s has no context named %s (available: %s)",
				kubeconfigPath, strings.Join(missing, ", "), strings.Join(all, ", "))
		}
		names = slices.DeleteFunc(slices.Clone(all), func(n string) bool {
			return !slices.Contains(allow, n)
		})
	}
	if len(names) == 0 {
		return nil, fmt.Errorf("kubeconfig %s defines no contexts", kubeconfigPath)
	}

	// The default is the kubeconfig's current-context whenever it is served. If
	// an allowlist excluded it, a single served context is unambiguous enough to
	// stand in; with several, callers must name one rather than have the server
	// guess which cluster they meant.
	def := cfg.CurrentContext
	if !slices.Contains(names, def) {
		def = ""
		if len(names) == 1 {
			def = names[0]
		}
	}

	infos := make([]ContextInfo, 0, len(names))
	for _, name := range names {
		info := ContextInfo{Name: name, Default: name == def}
		if kctx := cfg.Contexts[name]; kctx != nil {
			info.Cluster = kctx.Cluster
			if cluster := cfg.Clusters[kctx.Cluster]; cluster != nil {
				info.Server = cluster.Server
			}
		}
		infos = append(infos, info)
	}

	return &ClientSet{
		kubeconfigPath: kubeconfigPath,
		gcpAuth:        gcpAuth,
		defaultContext: def,
		infos:          infos,
		newClient:      NewClient,
		clients:        map[string]*Client{},
	}, nil
}

// Contexts returns the served contexts, sorted by name.
func (cs *ClientSet) Contexts() []ContextInfo {
	return slices.Clone(cs.infos)
}

// Names returns the served context names, sorted.
func (cs *ClientSet) Names() []string {
	names := make([]string, 0, len(cs.infos))
	for _, info := range cs.infos {
		names = append(names, info.Name)
	}
	return names
}

// DefaultContext returns the context used when a call names none. It is empty
// when the set is ambiguous, in which case Get requires an explicit name.
func (cs *ClientSet) DefaultContext() string { return cs.defaultContext }

// Get returns the Client for a context, building it on first use. An empty name
// selects the default context.
func (cs *ClientSet) Get(name string) (*Client, error) {
	if name == "" {
		if cs.defaultContext == "" {
			return nil, fmt.Errorf("no default context; pass \"context\" explicitly (available: %s)",
				strings.Join(cs.Names(), ", "))
		}
		name = cs.defaultContext
	}
	if !slices.Contains(cs.Names(), name) {
		return nil, fmt.Errorf("unknown context %q (available: %s)", name, strings.Join(cs.Names(), ", "))
	}

	cs.mu.Lock()
	cached, ok := cs.clients[name]
	cs.mu.Unlock()
	if ok {
		return cached, nil
	}

	// Built outside the lock: constructing a client can block on credential
	// minting or a dead endpoint, and one bad context must not stall calls to the
	// others. A concurrent duplicate build is harmless — one wins the map and
	// both callers get that one.
	built, err := cs.newClient(cs.kubeconfigPath, name, cs.gcpAuth)
	if err != nil {
		return nil, fmt.Errorf("context %q: %w", name, err)
	}

	cs.mu.Lock()
	defer cs.mu.Unlock()
	if existing, ok := cs.clients[name]; ok {
		return existing, nil
	}
	cs.clients[name] = built
	return built, nil
}
