package stress

import (
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func TestRGDGeneratorSetsTypeMetaAndLabels(t *testing.T) {
	generated := RGDGenerator("Smoke", DefaultComplexities["low"])(0)

	if generated.GetAPIVersion() != "kro.run/v1alpha1" {
		t.Fatalf("unexpected apiVersion %q", generated.GetAPIVersion())
	}
	if generated.GetKind() != "ResourceGraphDefinition" {
		t.Fatalf("unexpected kind %q", generated.GetKind())
	}
	if generated.GetLabels()[TestLabelKey] != TestLabelValue {
		t.Fatalf("missing stress label: %#v", generated.GetLabels())
	}
	if generated.GetLabels()[PrefixLabelKey] != "smoke" {
		t.Fatalf("unexpected prefix label %#v", generated.GetLabels())
	}

	resources, found, err := unstructured.NestedSlice(generated.Object, "spec", "resources")
	if err != nil || !found {
		t.Fatalf("expected resources, found=%v err=%v", found, err)
	}
	if len(resources) != 3 {
		t.Fatalf("expected 3 low-complexity resources, got %d", len(resources))
	}
}

func TestInstanceGeneratorSetsNamespaceAndLabels(t *testing.T) {
	generated := InstanceGenerator("Smoke", 7, "default")(3)

	if generated.GetKind() != "StressTestSmoke7" {
		t.Fatalf("unexpected kind %q", generated.GetKind())
	}
	if generated.GetNamespace() != "default" {
		t.Fatalf("unexpected namespace %q", generated.GetNamespace())
	}
	if generated.GetLabels()[PrefixLabelKey] != "smoke" {
		t.Fatalf("unexpected prefix label %#v", generated.GetLabels())
	}
}

func TestInstanceKindUsesPrefixForUniqueness(t *testing.T) {
	if left, right := InstanceKind("alpha", 7), InstanceKind("beta", 7); left == right {
		t.Fatalf("expected unique kinds, got %q and %q", left, right)
	}
}

func TestDeploymentComplexityBuildsFiftyPairsAndLinksThem(t *testing.T) {
	generated := RGDGenerator("Smoke", DefaultComplexities["deployments"])(2)

	resources, found, err := unstructured.NestedSlice(generated.Object, "spec", "resources")
	if err != nil || !found {
		t.Fatalf("expected resources, found=%v err=%v", found, err)
	}
	if len(resources) != 100 {
		t.Fatalf("expected 100 deployment-complexity resources, got %d", len(resources))
	}

	configMaps := 0
	deployments := 0

	for _, resource := range resources {
		resourceMap, ok := resource.(map[string]interface{})
		if !ok {
			t.Fatalf("unexpected resource type %T", resource)
		}
		template, ok := resourceMap["template"].(map[string]interface{})
		if !ok {
			t.Fatalf("missing template in resource %#v", resourceMap)
		}

		switch template["kind"] {
		case "ConfigMap":
			configMaps++
		case "Deployment":
			deployments++

			replicas, found, err := unstructured.NestedInt64(template, "spec", "replicas")
			if err != nil || !found {
				t.Fatalf("expected deployment replicas, found=%v err=%v", found, err)
			}
			if replicas != 0 {
				t.Fatalf("expected zero replicas, got %d", replicas)
			}
		}
	}

	if configMaps != 50 {
		t.Fatalf("expected 50 configmaps, got %d", configMaps)
	}
	if deployments != 50 {
		t.Fatalf("expected 50 deployments, got %d", deployments)
	}

	deploy1 := findResourceByID(t, resources, "deploy1")
	template, ok := deploy1["template"].(map[string]interface{})
	if !ok {
		t.Fatalf("missing template in deploy1")
	}
	cmName, found, err := unstructured.NestedString(template, "spec", "template", "spec", "containers", "0", "envFrom", "0", "configMapRef", "name")
	if err == nil && found {
		// unreachable: NestedString does not support slice indexes in the path
		_ = cmName
	}

	spec, ok := template["spec"].(map[string]interface{})
	if !ok {
		t.Fatalf("missing spec in deployment template")
	}
	podTemplate, ok := spec["template"].(map[string]interface{})
	if !ok {
		t.Fatalf("missing pod template in deployment template")
	}
	podSpec, ok := podTemplate["spec"].(map[string]interface{})
	if !ok {
		t.Fatalf("missing pod spec in deployment template")
	}
	containers, ok := podSpec["containers"].([]interface{})
	if !ok || len(containers) != 1 {
		t.Fatalf("expected one container, got %#v", podSpec["containers"])
	}
	container, ok := containers[0].(map[string]interface{})
	if !ok {
		t.Fatalf("unexpected container type %T", containers[0])
	}
	envFrom, ok := container["envFrom"].([]interface{})
	if !ok || len(envFrom) != 1 {
		t.Fatalf("expected one envFrom entry, got %#v", container["envFrom"])
	}
	envFromEntry, ok := envFrom[0].(map[string]interface{})
	if !ok {
		t.Fatalf("unexpected envFrom entry type %T", envFrom[0])
	}
	configMapRef, ok := envFromEntry["configMapRef"].(map[string]interface{})
	if !ok {
		t.Fatalf("missing configMapRef in envFrom entry")
	}
	if got, want := configMapRef["name"], "${cm1.metadata.name}"; got != want {
		t.Fatalf("expected deployment to reference %q, got %v", want, got)
	}
}

func findResourceByID(t *testing.T, resources []interface{}, id string) map[string]interface{} {
	t.Helper()

	for _, resource := range resources {
		resourceMap, ok := resource.(map[string]interface{})
		if ok && resourceMap["id"] == id {
			return resourceMap
		}
	}

	t.Fatalf("resource %q not found", id)
	return nil
}
