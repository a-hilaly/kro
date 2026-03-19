package stress

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	apimachineryyaml "k8s.io/apimachinery/pkg/util/yaml"
)

var CRDGVR = schema.GroupVersionResource{
	Group:    "apiextensions.k8s.io",
	Version:  "v1",
	Resource: "customresourcedefinitions",
}

func FetchCRDsFromURLs(ctx context.Context, urls []string) ([]*unstructured.Unstructured, error) {
	client := &http.Client{}
	var crds []*unstructured.Unstructured

	for _, url := range urls {
		docs, err := fetchYAMLDocuments(ctx, client, url)
		if err != nil {
			return nil, err
		}
		crds = append(crds, docs...)
	}

	if len(crds) == 0 {
		return nil, fmt.Errorf("no CustomResourceDefinition documents found")
	}

	return crds, nil
}

func fetchYAMLDocuments(ctx context.Context, client *http.Client, url string) ([]*unstructured.Unstructured, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("build request for %s: %w", url, err)
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch %s: %w", url, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetch %s: unexpected status %s", url, resp.Status)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", url, err)
	}

	decoder := apimachineryyaml.NewYAMLOrJSONDecoder(bytes.NewReader(body), 4096)
	var crds []*unstructured.Unstructured
	for {
		var raw map[string]interface{}
		if err := decoder.Decode(&raw); err != nil {
			if err == io.EOF {
				break
			}
			return nil, fmt.Errorf("decode %s: %w", url, err)
		}
		if len(raw) == 0 {
			continue
		}

		obj := &unstructured.Unstructured{Object: raw}
		if obj.GetKind() != "CustomResourceDefinition" || obj.GetAPIVersion() != "apiextensions.k8s.io/v1" {
			continue
		}
		crds = append(crds, obj)
	}

	return crds, nil
}
