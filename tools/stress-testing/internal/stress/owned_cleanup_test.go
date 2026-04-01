package stress

import (
	"testing"

	krometadata "github.com/kubernetes-sigs/kro/pkg/metadata"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func TestHierarchyInstanceGeneratorTargetsRootKind(t *testing.T) {
	gvr, root, generator, err := HierarchyInstanceGenerator("graph-heavy-50", "Graph-Heavy-50-Stage1", "default")
	if err != nil {
		t.Fatalf("HierarchyInstanceGenerator returned error: %v", err)
	}

	if root.ResourceGraphName != "01-graph-heavy-50-stack.kro.run" {
		t.Fatalf("unexpected root RGD %q", root.ResourceGraphName)
	}
	if root.SchemaKind != "GraphHeavy5001Stack" {
		t.Fatalf("unexpected root kind %q", root.SchemaKind)
	}
	if gvr.Resource != "graphheavy5001stacks" {
		t.Fatalf("unexpected root resource %q", gvr.Resource)
	}

	obj := generator(7)
	if obj.GetKind() != "GraphHeavy5001Stack" {
		t.Fatalf("unexpected generated kind %q", obj.GetKind())
	}
	if obj.GetName() != "graph-heavy-50-stage1-7" {
		t.Fatalf("unexpected generated name %q", obj.GetName())
	}
	if obj.GetLabels()[PrefixLabelKey] != "graph-heavy-50-stage1" {
		t.Fatalf("unexpected prefix label %#v", obj.GetLabels())
	}
}

func TestFilterOwnedCleanupMatchesUsesInstanceLabelPrefix(t *testing.T) {
	items := []unstructured.Unstructured{
		{
			Object: map[string]interface{}{
				"metadata": map[string]interface{}{
					"name": "graph-heavy-50-stage1-0-web-main-config",
					"labels": map[string]interface{}{
						krometadata.InstanceLabel: "graph-heavy-50-stage1-0-web-main",
					},
				},
			},
		},
		{
			Object: map[string]interface{}{
				"metadata": map[string]interface{}{
					"name": "other-run-0-web-main-config",
					"labels": map[string]interface{}{
						krometadata.InstanceLabel: "other-run-0-web-main",
					},
				},
			},
		},
	}

	filtered := filterOwnedCleanupMatches(items, "graph-heavy-50-stage1")
	if len(filtered) != 1 {
		t.Fatalf("expected 1 filtered item, got %d", len(filtered))
	}
	if filtered[0].GetName() != "graph-heavy-50-stage1-0-web-main-config" {
		t.Fatalf("unexpected filtered item %q", filtered[0].GetName())
	}
}

func TestFilterOwnedCleanupMatchesDoesNotOvermatchSimilarPrefixes(t *testing.T) {
	items := []unstructured.Unstructured{
		{
			Object: map[string]interface{}{
				"metadata": map[string]interface{}{
					"name": "graph-heavy-50-1-platform",
					"labels": map[string]interface{}{
						krometadata.InstanceLabel: "graph-heavy-50-1",
					},
				},
			},
		},
		{
			Object: map[string]interface{}{
				"metadata": map[string]interface{}{
					"name": "graph-heavy-50-10-platform",
					"labels": map[string]interface{}{
						krometadata.InstanceLabel: "graph-heavy-50-10",
					},
				},
			},
		},
	}

	filtered := filterOwnedCleanupMatches(items, "graph-heavy-50-1")
	if len(filtered) != 1 {
		t.Fatalf("expected 1 filtered item, got %d", len(filtered))
	}
	if filtered[0].GetName() != "graph-heavy-50-1-platform" {
		t.Fatalf("unexpected filtered item %q", filtered[0].GetName())
	}
}
