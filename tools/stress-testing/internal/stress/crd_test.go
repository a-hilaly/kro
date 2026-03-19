package stress

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestFetchCRDsFromURLs(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`
apiVersion: v1
kind: ConfigMap
metadata:
  name: ignored
---
apiVersion: apiextensions.k8s.io/v1
kind: CustomResourceDefinition
metadata:
  name: widgets.example.com
spec:
  group: example.com
  names:
    kind: Widget
    plural: widgets
  scope: Namespaced
  versions:
  - name: v1alpha1
    served: true
    storage: true
    schema:
      openAPIV3Schema:
        type: object
`))
	}))
	defer server.Close()

	crds, err := FetchCRDsFromURLs(context.Background(), []string{server.URL})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(crds) != 1 {
		t.Fatalf("expected 1 CRD, got %d", len(crds))
	}
	if got, want := crds[0].GetName(), "widgets.example.com"; got != want {
		t.Fatalf("expected CRD name %q, got %q", want, got)
	}
}

func TestFetchCRDsFromURLsErrorsWhenNoCRDsFound(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`
apiVersion: v1
kind: ConfigMap
metadata:
  name: ignored
`))
	}))
	defer server.Close()

	if _, err := FetchCRDsFromURLs(context.Background(), []string{server.URL}); err == nil {
		t.Fatal("expected error when no CRDs are present")
	}
}
