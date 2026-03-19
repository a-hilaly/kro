package stress

import (
	"context"
	"fmt"
	"strings"
	"time"

	krometadata "github.com/kubernetes-sigs/kro/pkg/metadata"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/discovery"
)

func HierarchyRootDefinition(name string) (HierarchyDefinition, error) {
	rendered, err := RenderHierarchy(name)
	if err != nil {
		return HierarchyDefinition{}, err
	}
	if len(rendered) == 0 {
		return HierarchyDefinition{}, fmt.Errorf("hierarchy %q rendered no RGDs", name)
	}
	return rendered[0].Definition, nil
}

func HierarchyRootGVR(name string) (schema.GroupVersionResource, HierarchyDefinition, error) {
	root, err := HierarchyRootDefinition(name)
	if err != nil {
		return schema.GroupVersionResource{}, HierarchyDefinition{}, err
	}

	return schema.GroupVersionResource{
		Group:    "kro.run",
		Version:  "v1alpha1",
		Resource: strings.ToLower(root.SchemaKind) + "s",
	}, root, nil
}

func HierarchyInstanceGenerator(hierarchy string, prefix string, namespace string) (schema.GroupVersionResource, HierarchyDefinition, func(index int) *unstructured.Unstructured, error) {
	targetGVR, root, err := HierarchyRootGVR(hierarchy)
	if err != nil {
		return schema.GroupVersionResource{}, HierarchyDefinition{}, nil, err
	}

	prefix = sanitizePrefix(prefix)
	return targetGVR, root, func(index int) *unstructured.Unstructured {
		name := fmt.Sprintf("%s-%d", prefix, index)
		return &unstructured.Unstructured{
			Object: map[string]interface{}{
				"apiVersion": "kro.run/v1alpha1",
				"kind":       root.SchemaKind,
				"metadata": map[string]interface{}{
					"name":      name,
					"namespace": namespace,
					"labels": map[string]interface{}{
						TestLabelKey:                             TestLabelValue,
						PrefixLabelKey:                           prefix,
						krometadata.ResourceGraphDefinitionNameLabel:      root.ResourceGraphName,
						krometadata.ResourceGraphDefinitionNamespaceLabel: metav1.NamespaceDefault,
					},
				},
				"spec": map[string]interface{}{
					"name":      name,
					"namespace": namespace,
				},
			},
		}
	}, nil
}

func WaitForInstanceResourceByName(ctx context.Context, discoveryClient discovery.DiscoveryInterface, resourceName string, pollInterval time.Duration) error {
	if pollInterval <= 0 {
		pollInterval = 2 * time.Second
	}

	return wait.PollUntilContextCancel(ctx, pollInterval, true, func(ctx context.Context) (bool, error) {
		resourceList, err := discoveryClient.ServerResourcesForGroupVersion("kro.run/v1alpha1")
		if err != nil {
			return false, nil
		}

		for _, resource := range resourceList.APIResources {
			if resource.Name == resourceName {
				return true, nil
			}
		}

		return false, nil
	})
}
