package main

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/discovery"
	memory "k8s.io/client-go/discovery/cached/memory"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/restmapper"
	"k8s.io/client-go/tools/clientcmd"
)

// maxLogBytes caps a pod-log read so a chatty pod cannot blow out the model's
// context.
const maxLogBytes = 256 * 1024

// Client wraps the Kubernetes API for the tool handlers. It uses a dynamic
// client (arbitrary resources) plus a REST mapper (kind → resource + scope) so
// the policy can reason about the exact resource and namespaced-ness, and a
// typed client for pod logs (a streaming subresource the dynamic client does
// not model).
type Client struct {
	dyn    dynamic.Interface
	typed  kubernetes.Interface
	disco  discovery.DiscoveryInterface
	mapper meta.RESTMapper
}

// NewClient builds a Client from a kubeconfig file and optional context. Auth
// (bearer token or embedded client certificate) comes straight from the
// kubeconfig; exec-plugin auth is not supported inside the container.
func NewClient(kubeconfigPath, kubeContext string) (*Client, error) {
	loadingRules := &clientcmd.ClientConfigLoadingRules{ExplicitPath: kubeconfigPath}
	overrides := &clientcmd.ConfigOverrides{}
	if kubeContext != "" {
		overrides.CurrentContext = kubeContext
	}
	cc := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(loadingRules, overrides)
	restConfig, err := cc.ClientConfig()
	if err != nil {
		return nil, fmt.Errorf("failed to load kubeconfig %s: %w", kubeconfigPath, err)
	}

	dyn, err := dynamic.NewForConfig(restConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create dynamic client: %w", err)
	}
	typed, err := kubernetes.NewForConfig(restConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create clientset: %w", err)
	}
	dc, err := discovery.NewDiscoveryClientForConfig(restConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create discovery client: %w", err)
	}
	mapper := restmapper.NewDeferredDiscoveryRESTMapper(memory.NewMemCacheClient(dc))

	return &Client{dyn: dyn, typed: typed, disco: dc, mapper: mapper}, nil
}

// resolved describes a resolved target: its GroupVersionResource and whether it
// is namespace-scoped.
type resolved struct {
	gvr        schema.GroupVersionResource
	namespaced bool
}

// resolve maps an apiVersion + kind to a resource + scope via the REST mapper.
func (c *Client) resolve(apiVersion, kind string) (resolved, error) {
	gv, err := schema.ParseGroupVersion(apiVersion)
	if err != nil {
		return resolved{}, fmt.Errorf("invalid api_version %q: %w", apiVersion, err)
	}
	mapping, err := c.mapper.RESTMapping(schema.GroupKind{Group: gv.Group, Kind: kind}, gv.Version)
	if err != nil {
		return resolved{}, fmt.Errorf("unknown resource %s/%s: %w", apiVersion, kind, err)
	}
	return resolved{
		gvr:        mapping.Resource,
		namespaced: mapping.Scope.Name() == meta.RESTScopeNameNamespace,
	}, nil
}

// resource returns the dynamic resource interface, namespaced when applicable.
func (c *Client) resource(r resolved, namespace string) dynamic.ResourceInterface {
	if r.namespaced {
		return c.dyn.Resource(r.gvr).Namespace(namespace)
	}
	return c.dyn.Resource(r.gvr)
}

func (c *Client) list(ctx context.Context, r resolved, namespace, labelSelector string) (*unstructured.UnstructuredList, error) {
	return c.resource(r, namespace).List(ctx, metav1.ListOptions{LabelSelector: labelSelector})
}

func (c *Client) get(ctx context.Context, r resolved, namespace, name string) (*unstructured.Unstructured, error) {
	return c.resource(r, namespace).Get(ctx, name, metav1.GetOptions{})
}

func (c *Client) delete(ctx context.Context, r resolved, namespace, name string) error {
	return c.resource(r, namespace).Delete(ctx, name, metav1.DeleteOptions{})
}

// applyResource creates the object, or updates it if it already exists. (A
// create-or-update rather than a server-side apply, so it works uniformly and
// is straightforward to test.)
func (c *Client) applyResource(ctx context.Context, r resolved, obj *unstructured.Unstructured) (*unstructured.Unstructured, error) {
	ri := c.resource(r, obj.GetNamespace())
	existing, err := ri.Get(ctx, obj.GetName(), metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		return ri.Create(ctx, obj, metav1.CreateOptions{})
	}
	if err != nil {
		return nil, err
	}
	obj.SetResourceVersion(existing.GetResourceVersion())
	return ri.Update(ctx, obj, metav1.UpdateOptions{})
}

// logs returns up to maxLogBytes of a pod's logs.
func (c *Client) logs(ctx context.Context, namespace, name, container string, tailLines int64) (string, error) {
	limit := int64(maxLogBytes)
	opts := &corev1.PodLogOptions{Container: container, LimitBytes: &limit}
	if tailLines > 0 {
		opts.TailLines = &tailLines
	}
	raw, err := c.typed.CoreV1().Pods(namespace).GetLogs(name, opts).DoRaw(ctx)
	if err != nil {
		return "", err
	}
	return string(raw), nil
}

// apiResources lists the server's preferred API resources (discovery).
func (c *Client) apiResources() ([]*metav1.APIResourceList, error) {
	lists, err := c.disco.ServerPreferredResources()
	if err != nil {
		// Partial discovery errors are common (e.g. a broken aggregated API);
		// return whatever was discovered alongside the error context.
		if len(lists) == 0 {
			return nil, fmt.Errorf("discovery failed: %w", err)
		}
	}
	return lists, nil
}
