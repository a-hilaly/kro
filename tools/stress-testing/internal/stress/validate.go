package stress

import (
	"fmt"
	"sort"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/client-go/discovery"
)

type MissingKind struct {
	APIVersion string
	Kind       string
}

func MissingRenderedKinds(discoveryClient discovery.DiscoveryInterface, rendered []RenderedHierarchyRGD) ([]MissingKind, error) {
	required := map[string]map[string]struct{}{}
	for _, manifest := range rendered {
		resources, found, err := unstructured.NestedSlice(manifest.Object.Object, "spec", "resources")
		if err != nil {
			return nil, fmt.Errorf("read resources for %s: %w", manifest.Definition.ResourceGraphName, err)
		}
		if !found {
			continue
		}
		for _, resource := range resources {
			resourceMap, ok := resource.(map[string]interface{})
			if !ok {
				continue
			}
			template, ok := resourceMap["template"].(map[string]interface{})
			if !ok {
				continue
			}
			apiVersion, _ := template["apiVersion"].(string)
			kind, _ := template["kind"].(string)
			if apiVersion == "" || kind == "" {
				continue
			}
			if apiVersion == "kro.run/v1alpha1" {
				continue
			}
			if _, ok := required[apiVersion]; !ok {
				required[apiVersion] = map[string]struct{}{}
			}
			required[apiVersion][kind] = struct{}{}
		}
	}

	var missing []MissingKind
	for apiVersion, kinds := range required {
		resourceList, err := discoveryClient.ServerResourcesForGroupVersion(apiVersion)
		if err != nil {
			for kind := range kinds {
				missing = append(missing, MissingKind{APIVersion: apiVersion, Kind: kind})
			}
			continue
		}

		available := map[string]struct{}{}
		for _, resource := range resourceList.APIResources {
			available[resource.Kind] = struct{}{}
		}
		for kind := range kinds {
			if _, ok := available[kind]; !ok {
				missing = append(missing, MissingKind{APIVersion: apiVersion, Kind: kind})
			}
		}
	}

	sort.Slice(missing, func(i, j int) bool {
		if missing[i].APIVersion == missing[j].APIVersion {
			return missing[i].Kind < missing[j].Kind
		}
		return missing[i].APIVersion < missing[j].APIVersion
	})

	return missing, nil
}
