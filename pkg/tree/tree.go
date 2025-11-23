package tree

import (
	"context"
	"fmt"
	"strings"

	"github.com/vinishsoman/crossplane-diagnose/pkg/report"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
)

// Builder handles tree construction
type Builder struct {
	client dynamic.Interface
}

// NewBuilder creates a new Builder
func NewBuilder(client dynamic.Interface) *Builder {
	return &Builder{client: client}
}

// BuildTree constructs a tree for a given Composite Resource
func (b *Builder) BuildTree(ctx context.Context, gvr schema.GroupVersionResource, name string) (*report.ResourceStatus, error) {
	xr, err := b.client.Resource(gvr).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to get XR %s: %v", name, err)
	}

	// 2. Build Tree Recursively
	return b.buildNodeRecursive(ctx, xr), nil
}

func (b *Builder) buildNodeRecursive(ctx context.Context, obj *unstructured.Unstructured) *report.ResourceStatus {
	node := &report.ResourceStatus{
		Kind:   obj.GetKind(),
		Name:   obj.GetName(),
		Synced: "Unknown",
		Ready:  "Unknown",
	}

	// Extract Status
	conditions, found, _ := unstructured.NestedSlice(obj.Object, "status", "conditions")
	if found {
		for _, c := range conditions {
			cond, ok := c.(map[string]interface{})
			if !ok {
				continue
			}
			cType, _ := cond["type"].(string)
			cStatus, _ := cond["status"].(string)
			cReason, _ := cond["reason"].(string)
			cMessage, _ := cond["message"].(string)

			if cType == "Synced" {
				node.Synced = cStatus
			}
			if cType == "Ready" {
				node.Ready = cStatus
			}

			node.Conditions = append(node.Conditions, fmt.Sprintf("%s=%s (%s): %s", cType, cStatus, cReason, cMessage))
		}
	}

	// Determine overall status
	if node.Ready == "True" && node.Synced == "True" {
		node.Status = "Available"
	} else {
		node.Status = "Unhealthy"
	}

	// Fetch Events
	events, err := b.fetchEvents(ctx, obj.GetKind(), obj.GetName(), obj.GetNamespace())
	if err == nil {
		node.Events = events
	}

	// Find Children (Managed Resources) via spec.resourceRefs
	refs, found, err := unstructured.NestedSlice(obj.Object, "spec", "resourceRefs")
	if err == nil && found {
		for _, ref := range refs {
			refMap, ok := ref.(map[string]interface{})
			if !ok {
				continue
			}

			// Extract reference details
			apiVersion, _ := refMap["apiVersion"].(string)
			kind, _ := refMap["kind"].(string)
			refName, _ := refMap["name"].(string)

			if apiVersion == "" || kind == "" || refName == "" {
				continue
			}

			// Parse GroupVersion
			gv, err := schema.ParseGroupVersion(apiVersion)
			if err != nil {
				continue
			}

			// Naive pluralization
			resource := strings.ToLower(kind) + "s"

			childGVR := schema.GroupVersionResource{
				Group:    gv.Group,
				Version:  gv.Version,
				Resource: resource,
			}

			childObj, err := b.client.Resource(childGVR).Get(ctx, refName, metav1.GetOptions{})
			if err != nil {
				node.Children = append(node.Children, report.ResourceStatus{
					Kind:   kind,
					Name:   refName,
					Status: fmt.Sprintf("Error fetching: %v", err),
				})
				continue
			}

			// Recursively build child node
			childNode := b.buildNodeRecursive(ctx, childObj)
			node.Children = append(node.Children, *childNode)
		}
	}

	return node
}

func (b *Builder) fetchEvents(ctx context.Context, kind, name, namespace string) ([]string, error) {
	// Events are namespaced if the object is namespaced.
	// If namespace is empty, it might be cluster scoped, but events for cluster scoped objects are usually in default or specific namespace?
	// Actually events are always namespaced. For cluster scoped resources, events are often in 'default'.
	// But let's try the object's namespace if present, or 'default' if not?
	// Safest is to list all events with field selector.

	gvr := schema.GroupVersionResource{Group: "", Version: "v1", Resource: "events"}

	// If namespace is provided, list in that namespace.
	// If not, list in all namespaces (requires ClusterRole).
	var client dynamic.ResourceInterface
	if namespace != "" {
		client = b.client.Resource(gvr).Namespace(namespace)
	} else {
		client = b.client.Resource(gvr) // All namespaces
	}

	opts := metav1.ListOptions{
		FieldSelector: fmt.Sprintf("involvedObject.kind=%s,involvedObject.name=%s", kind, name),
	}

	list, err := client.List(ctx, opts)
	if err != nil {
		return nil, err
	}

	var events []string
	for _, item := range list.Items {
		reason, _, _ := unstructured.NestedString(item.Object, "reason")
		message, _, _ := unstructured.NestedString(item.Object, "message")
		typeStr, _, _ := unstructured.NestedString(item.Object, "type")
		events = append(events, fmt.Sprintf("[%s] %s: %s", typeStr, reason, message))
	}
	return events, nil
}
