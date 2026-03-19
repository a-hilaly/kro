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

func TestRealComplexityBuildsTwentyResourcesWithAckAndZeroReplicaDefaults(t *testing.T) {
	generated := RGDGenerator("Real", DefaultComplexities["real"])(1)

	resources, found, err := unstructured.NestedSlice(generated.Object, "spec", "resources")
	if err != nil || !found {
		t.Fatalf("expected resources, found=%v err=%v", found, err)
	}
	if len(resources) != 20 {
		t.Fatalf("expected 20 real-complexity resources, got %d", len(resources))
	}

	spec, found, err := unstructured.NestedMap(generated.Object, "spec", "schema", "spec")
	if err != nil || !found {
		t.Fatalf("expected schema spec, found=%v err=%v", found, err)
	}
	if got, want := spec["replicas"], "integer | default=0"; got != want {
		t.Fatalf("expected replicas schema %q, got %v", want, got)
	}

	expectedKinds := map[string]int{
		"ConfigMap":           3,
		"Secret":              1,
		"ServiceAccount":      1,
		"Role":                2,
		"RoleBinding":         2,
		"Deployment":          1,
		"Service":             2,
		"NetworkPolicy":       1,
		"PodDisruptionBudget": 1,
		"Lease":               1,
		"Job":                 1,
		"CronJob":             1,
		"Ingress":             1,
		"Bucket":              1,
		"VPC":                 1,
	}

	gotKinds := map[string]int{}
	for _, resource := range resources {
		resourceMap, ok := resource.(map[string]interface{})
		if !ok {
			t.Fatalf("unexpected resource type %T", resource)
		}
		template, ok := resourceMap["template"].(map[string]interface{})
		if !ok {
			t.Fatalf("missing template in resource %#v", resourceMap)
		}
		kind, _ := template["kind"].(string)
		gotKinds[kind]++
	}
	if len(gotKinds) != len(expectedKinds) {
		t.Fatalf("unexpected kind count %#v", gotKinds)
	}
	for kind, want := range expectedKinds {
		if got := gotKinds[kind]; got != want {
			t.Fatalf("expected %d %s resources, got %d", want, kind, got)
		}
	}

	deployment := findResourceByID(t, resources, "deployment")
	deploymentTemplate, ok := deployment["template"].(map[string]interface{})
	if !ok {
		t.Fatalf("missing deployment template")
	}
	if got, want := nestedMapValue(t, deploymentTemplate, "spec", "replicas"), "${schema.spec.replicas}"; got != want {
		t.Fatalf("expected deployment replicas %q, got %v", want, got)
	}

	deploymentSpec, ok := nestedMapValue(t, deploymentTemplate, "spec", "template", "spec").(map[string]interface{})
	if !ok {
		t.Fatalf("missing deployment pod spec")
	}
	if got, want := deploymentSpec["serviceAccountName"], "${serviceAccount.metadata.name}"; got != want {
		t.Fatalf("expected deployment SA ref %q, got %v", want, got)
	}

	preflightJob := findResourceByID(t, resources, "preflightJob")
	preflightTemplate, ok := preflightJob["template"].(map[string]interface{})
	if !ok {
		t.Fatalf("missing preflight job template")
	}
	if got, want := nestedMapValue(t, preflightTemplate, "spec", "suspend"), true; got != want {
		t.Fatalf("expected suspended job, got %v", got)
	}

	maintenanceCron := findResourceByID(t, resources, "maintenanceCron")
	maintenanceTemplate, ok := maintenanceCron["template"].(map[string]interface{})
	if !ok {
		t.Fatalf("missing maintenance cron template")
	}
	if got, want := nestedMapValue(t, maintenanceTemplate, "spec", "suspend"), true; got != want {
		t.Fatalf("expected suspended cronjob, got %v", got)
	}

	ackBucket := findResourceByID(t, resources, "ackBucket")
	ackBucketTemplate, ok := ackBucket["template"].(map[string]interface{})
	if !ok {
		t.Fatalf("missing ack bucket template")
	}
	if got, want := ackBucketTemplate["apiVersion"], "s3.services.k8s.aws/v1alpha1"; got != want {
		t.Fatalf("expected bucket apiVersion %q, got %v", want, got)
	}

	ackVpc := findResourceByID(t, resources, "ackVpc")
	ackVpcTemplate, ok := ackVpc["template"].(map[string]interface{})
	if !ok {
		t.Fatalf("missing ack vpc template")
	}
	if got, want := ackVpcTemplate["apiVersion"], "ec2.services.k8s.aws/v1alpha1"; got != want {
		t.Fatalf("expected vpc apiVersion %q, got %v", want, got)
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

func nestedMapValue(t *testing.T, value map[string]interface{}, path ...string) interface{} {
	t.Helper()

	current := value
	for i, segment := range path {
		next, ok := current[segment]
		if !ok {
			t.Fatalf("missing path segment %q in %#v", segment, current)
		}
		if i == len(path)-1 {
			return next
		}
		nextMap, ok := next.(map[string]interface{})
		if !ok {
			t.Fatalf("expected map at %q, got %T", segment, next)
		}
		current = nextMap
	}

	return nil
}
