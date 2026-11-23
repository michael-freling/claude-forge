package spec

import (
	"errors"
	"fmt"
	"net"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// PolicyConfig extends the generated policy (openshell.policy in the
// claude-forge config). Entries are host or host:port (port 443 by default);
// a wildcard (* for one label, ** for one or more) may only be the entire
// first label of a host with at least three labels, e.g. **.example.org.
type PolicyConfig struct {
	// Allow lists hosts any traffic may reach, uninspected, like the
	// built-in groups (rule custom_allow).
	Allow []string `yaml:"allow"`
	// ReadOnly lists hosts that only GET, HEAD and OPTIONS requests may reach,
	// like the built-in read_only group (rule custom_read_only).
	ReadOnly []string `yaml:"read_only"`
	// Disable lists built-in groups (GroupNames) to drop.
	Disable []string `yaml:"disable"`
}

// IsEmpty reports whether the config changes nothing.
func (c PolicyConfig) IsEmpty() bool {
	return len(c.Allow) == 0 && len(c.ReadOnly) == 0 && len(c.Disable) == 0
}

// Validate checks every entry: its syntax, that it is not a destination
// OpenShell always blocks, and that it does not overlap a provider-owned
// endpoint. Errors name the offending key (allow, read_only or disable).
func (c PolicyConfig) Validate() error {
	for _, list := range []struct {
		key     string
		entries []string
	}{{"allow", c.Allow}, {"read_only", c.ReadOnly}} {
		for _, entry := range list.entries {
			ep, err := parseEntry(entry)
			if err != nil {
				return fmt.Errorf("%s: %w", list.key, err)
			}
			if err := checkProviderOverlap(ep); err != nil {
				return fmt.Errorf("%s: %q: %w", list.key, entry, err)
			}
		}
	}
	names := GroupNames()
	for _, name := range c.Disable {
		if !slices.Contains(names, name) {
			return fmt.Errorf("disable: unknown group %q: use one of %s", name, strings.Join(names, ", "))
		}
	}
	return nil
}

// dnsLabel matches one label of a host name. Underscores are allowed: some
// service hosts use them.
var dnsLabel = regexp.MustCompile(`^[a-z0-9_]([a-z0-9_-]{0,61}[a-z0-9_])?$`)

// parseEntry parses a host or host:port entry into an uninspected endpoint.
// The host is lowercased.
func parseEntry(entry string) (Endpoint, error) {
	host, port := entry, defaultPort
	if i := strings.LastIndex(entry, ":"); i >= 0 {
		host = entry[:i]
		p, err := strconv.Atoi(entry[i+1:])
		if err != nil || p < 1 || p > 65535 {
			return Endpoint{}, fmt.Errorf("invalid entry %q: the port must be a number from 1 to 65535", entry)
		}
		port = p
	}
	host = strings.ToLower(host)
	if err := validateHost(host); err != nil {
		return Endpoint{}, fmt.Errorf("invalid entry %q: %w", entry, err)
	}
	return Endpoint{Host: host, Port: port}, nil
}

func validateHost(host string) error {
	if host == "" || len(host) > 253 {
		return errors.New("use host or host:port")
	}
	labels := strings.Split(host, ".")
	wildcard := false
	for i, label := range labels {
		if label == "*" || label == "**" {
			if i != 0 {
				return errors.New("a wildcard (* or **) may only be the entire first label")
			}
			wildcard = true
			continue
		}
		if strings.Contains(label, "*") {
			return errors.New("a wildcard (* or **) may only be the entire first label")
		}
		if !dnsLabel.MatchString(label) {
			return fmt.Errorf("invalid host label %q", label)
		}
	}
	if wildcard && len(labels) < 3 {
		return errors.New("a wildcard host needs at least three labels, e.g. **.example.org")
	}
	if blocked(host) {
		return errors.New("OpenShell always blocks loopback, link-local and metadata destinations")
	}
	return nil
}

// blocked reports destinations OpenShell never lets a sandbox reach.
func blocked(host string) bool {
	if host == "localhost" || strings.HasSuffix(host, ".localhost") || host == "metadata.google.internal" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && (ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsUnspecified())
}

// providerEndpoint is an endpoint a provider profile owns.
type providerEndpoint struct {
	Endpoint
	profile string
}

// providerEndpoints returns the endpoints the provider profiles inspect to
// inject their credentials.
func providerEndpoints() []providerEndpoint {
	var eps []providerEndpoint
	for _, p := range []*Profile{ClaudeProfile(), GitHubProfile(nil)} {
		for _, ep := range p.Endpoints {
			eps = append(eps, providerEndpoint{Endpoint: ep, profile: p.DisplayName})
		}
	}
	return eps
}

// checkProviderOverlap rejects an endpoint that would match a provider-owned
// one.
func checkProviderOverlap(ep Endpoint) error {
	for _, owned := range providerEndpoints() {
		if ep.Port == owned.Port && hostsOverlap(ep.Host, owned.Host) {
			return fmt.Errorf("overlaps %s:%d, which the %s provider inspects to inject its credential "+
				"(OpenShell rejects overlapping endpoints that disagree on inspection)", owned.Host, owned.Port, owned.profile)
		}
	}
	return nil
}

// hostsOverlap reports whether some host matches both patterns, where a
// first label of * matches one label and ** one or more.
func hostsOverlap(a, b string) bool {
	wildA, suffixA := splitWildcard(a)
	wildB, suffixB := splitWildcard(b)
	if wildA == "" && wildB == "" {
		return a == b
	}
	// Make A the pattern with the shorter (or equal) fixed suffix.
	if len(suffixA) > len(suffixB) {
		wildA, suffixA, wildB, suffixB = wildB, suffixB, wildA, suffixA
	}
	if wildA == "" {
		// An exact host never matches a pattern with a longer suffix, and an
		// equal suffix means a is b's bare suffix, which b's wildcard excludes.
		return false
	}
	if !slices.Equal(suffixA, suffixB[len(suffixB)-len(suffixA):]) {
		return false
	}
	extra := len(suffixB) - len(suffixA)
	if wildB == "" {
		// b is an exact host: A's wildcard must cover its extra labels, and
		// never matches A's bare suffix.
		return extra == 1 || (extra > 1 && wildA == "**")
	}
	// B's hosts are x.suffixB: A matches one of them when its wildcard can
	// cover the extra labels (none extra: any wildcard covers one label).
	return extra == 0 || wildA == "**"
}

// splitWildcard splits a host into its wildcard first label ("" for none)
// and the labels after it.
func splitWildcard(host string) (string, []string) {
	labels := strings.Split(host, ".")
	if labels[0] == "*" || labels[0] == "**" {
		return labels[0], labels[1:]
	}
	return "", labels
}

// ProviderOverlaps lists the endpoints of a full policy (openshell.policy_file)
// that overlap a provider-owned endpoint, as warnings: OpenShell rejects the
// sandbox when an overlapping endpoint disagrees with the provider on
// inspection.
func ProviderOverlaps(policyYAML []byte) ([]string, error) {
	var p Policy
	if err := yaml.Unmarshal(policyYAML, &p); err != nil {
		return nil, fmt.Errorf("failed to parse policy: %w", err)
	}
	var warnings []string
	for _, key := range sortedKeys(p.NetworkPolicies) {
		for _, ep := range p.NetworkPolicies[key].Endpoints {
			if ep.Port == 0 {
				ep.Port = defaultPort
			}
			if err := checkProviderOverlap(Endpoint{Host: strings.ToLower(ep.Host), Port: ep.Port}); err != nil {
				warnings = append(warnings, fmt.Sprintf("network_policies.%s: %s:%d %v", key, ep.Host, ep.Port, err))
			}
		}
	}
	return warnings, nil
}

func sortedKeys(m map[string]NetworkRule) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return keys
}

// parseEntries parses entries with parseEntry.
func parseEntries(entries []string) ([]Endpoint, error) {
	eps := make([]Endpoint, 0, len(entries))
	for _, entry := range entries {
		ep, err := parseEntry(entry)
		if err != nil {
			return nil, err
		}
		eps = append(eps, ep)
	}
	return eps, nil
}
