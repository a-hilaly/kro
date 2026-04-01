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

func TestFeatureMixComplexityBuildsHundredResourcesWithCollectionsAndExternalRefs(t *testing.T) {
	generated := RGDGenerator("FeatureMix", DefaultComplexities["feature-mix"])(3)

	resources, found, err := unstructured.NestedSlice(generated.Object, "spec", "resources")
	if err != nil || !found {
		t.Fatalf("expected resources, found=%v err=%v", found, err)
	}
	if len(resources) != 100 {
		t.Fatalf("expected 100 feature-mix resources, got %d", len(resources))
	}

	collections := 0
	externalRefs := 0
	namedExternalRefs := 0
	selectorExternalRefs := 0
	readyWhen := 0
	includeWhen := 0

	for _, resource := range resources {
		resourceMap, ok := resource.(map[string]interface{})
		if !ok {
			t.Fatalf("unexpected resource type %T", resource)
		}

		if forEach, ok := resourceMap["forEach"].([]interface{}); ok && len(forEach) > 0 {
			collections++
		}
		if externalRef, ok := resourceMap["externalRef"].(map[string]interface{}); ok {
			externalRefs++
			metadata, ok := externalRef["metadata"].(map[string]interface{})
			if !ok {
				t.Fatalf("missing metadata on externalRef %#v", externalRef)
			}
			if _, ok := metadata["name"]; ok {
				namedExternalRefs++
			}
			if _, ok := metadata["selector"]; ok {
				selectorExternalRefs++
			}
		}
		if readyWhenList, ok := resourceMap["readyWhen"].([]interface{}); ok && len(readyWhenList) > 0 {
			readyWhen++
		}
		if includeWhenList, ok := resourceMap["includeWhen"].([]interface{}); ok && len(includeWhenList) > 0 {
			includeWhen++
		}
	}

	if collections != 13 {
		t.Fatalf("expected 13 collection resources, got %d", collections)
	}
	if externalRefs != 15 {
		t.Fatalf("expected 15 external refs, got %d", externalRefs)
	}
	if namedExternalRefs != 15 {
		t.Fatalf("expected 15 named external refs, got %d", namedExternalRefs)
	}
	if selectorExternalRefs != 0 {
		t.Fatalf("expected 0 selector external refs, got %d", selectorExternalRefs)
	}
	if readyWhen != 15 {
		t.Fatalf("expected 15 readyWhen resources, got %d", readyWhen)
	}
	if includeWhen == 0 {
		t.Fatalf("expected includeWhen coverage in feature-mix preset")
	}

	if got, want := nestedMapValue(t, generated.Object, "spec", "schema", "status", "externals", "rootConfigName"), "${extConfig1.metadata.name}"; got != want {
		t.Fatalf("expected root config status %q, got %v", want, got)
	}
	if got, want := nestedMapValue(t, generated.Object, "spec", "schema", "status", "resources", "ackVpcName"), "${ackVpc.metadata.name}"; got != want {
		t.Fatalf("expected ack vpc status %q, got %v", want, got)
	}
	if got, want := nestedMapValue(t, generated.Object, "spec", "schema", "status", "ack", "securityGroupCount"), "${string(ackSecurityGroups1.size())}"; got != want {
		t.Fatalf("expected ack security group count status %q, got %v", want, got)
	}
	if got, want := nestedMapValue(t, generated.Object, "spec", "schema", "status", "workload", "readyReplicas"), "${string(deploy1.status.readyReplicas)}"; got != want {
		t.Fatalf("expected ready replicas status %q, got %v", want, got)
	}
	if got, want := nestedMapValue(t, generated.Object, "spec", "schema", "status", "deployments", "deployment10Name"), "${deploy10.metadata.name}"; got != want {
		t.Fatalf("expected deployment status %q, got %v", want, got)
	}
	if got, want := nestedMapValue(t, generated.Object, "spec", "schema", "status", "collections", "matrixSecrets5Count"), "${string(matrixSecrets5.size())}"; got != want {
		t.Fatalf("expected collection status %q, got %v", want, got)
	}

	ackBucket := findResourceByID(t, resources, "ackBucket")
	if got, want := nestedMapValue(t, ackBucket, "template", "apiVersion"), "s3.services.k8s.aws/v1alpha1"; got != want {
		t.Fatalf("expected ack bucket apiVersion %q, got %v", want, got)
	}

	ackSubnets := findResourceByID(t, resources, "ackSubnets1")
	if got, want := nestedMapValue(t, ackSubnets, "template", "kind"), "Subnet"; got != want {
		t.Fatalf("expected ack subnet kind %q, got %v", want, got)
	}
	forEach, ok := ackSubnets["forEach"].([]interface{})
	if !ok || len(forEach) != 1 {
		t.Fatalf("expected ackSubnets1 to have a single forEach dimension, got %#v", ackSubnets["forEach"])
	}

	ackSecurityGroups := findResourceByID(t, resources, "ackSecurityGroups1")
	if got, want := nestedMapValue(t, ackSecurityGroups, "template", "kind"), "SecurityGroup"; got != want {
		t.Fatalf("expected ack security group kind %q, got %v", want, got)
	}
	ackForEach, ok := ackSecurityGroups["forEach"].([]interface{})
	if !ok || len(ackForEach) != 1 {
		t.Fatalf("expected ackSecurityGroups1 to have a single forEach dimension, got %#v", ackSecurityGroups["forEach"])
	}
}

func TestCircusComplexityDropsOptionalNodesButKeepsMandatoryCore(t *testing.T) {
	cfg := DefaultComplexities["circus"]
	generated := RGDGenerator("Circus", cfg)(7)
	base := featureMixRGD("Circus", 7)

	resources, found, err := unstructured.NestedSlice(generated.Object, "spec", "resources")
	if err != nil || !found {
		t.Fatalf("expected resources, found=%v err=%v", found, err)
	}

	minResources := 100 - cfg.DropMax
	maxResources := 100 - cfg.DropMin
	if len(resources) < minResources || len(resources) > maxResources {
		t.Fatalf("expected circus resource count in [%d,%d], got %d", minResources, maxResources, len(resources))
	}

	resourceIDs := map[string]bool{}
	for _, resource := range resources {
		resourceMap, ok := resource.(map[string]interface{})
		if !ok {
			t.Fatalf("unexpected resource type %T", resource)
		}
		id, _ := resourceMap["id"].(string)
		resourceIDs[id] = true
	}

	for id := range circusMandatoryIDs {
		if !resourceIDs[id] {
			t.Fatalf("expected mandatory circus id %q to remain present", id)
		}
	}

	dependencies := circusResourceDependencies(resources, resourceIDsForTest(t, generated))
	for id, refs := range dependencies {
		for ref := range refs {
			if !resourceIDs[ref] {
				t.Fatalf("resource %q still references dropped dependency %q", id, ref)
			}
		}
	}

	baseResourceIDs := resourceIDsForTest(t, base)
	droppedResourceIDs := make([]string, 0, len(baseResourceIDs)-len(resourceIDs))
	for _, id := range baseResourceIDs {
		if !resourceIDs[id] {
			droppedResourceIDs = append(droppedResourceIDs, id)
		}
	}
	if len(droppedResourceIDs) == 0 {
		t.Fatalf("expected circus to drop at least one resource")
	}

	baseStatusLeafCount := statusLeafCount(t, base)
	generatedStatus, found, err := unstructured.NestedMap(generated.Object, "spec", "schema", "status")
	if err != nil || !found {
		t.Fatalf("expected status schema, found=%v err=%v", found, err)
	}
	refs := map[string]bool{}
	circusCollectReferences(generatedStatus, droppedResourceIDs, refs)
	if len(refs) > 0 {
		t.Fatalf("expected pruned circus status schema to avoid dropped resources, still found refs: %v", refs)
	}
	if got := statusLeafCount(t, generated); got >= baseStatusLeafCount {
		t.Fatalf("expected circus status schema to shrink from base count %d, got %d", baseStatusLeafCount, got)
	}
}

func TestCircusComplexityIsDeterministicPerIndexAndSeed(t *testing.T) {
	cfg := DefaultComplexities["circus"]

	left := resourceIDsForTest(t, RGDGenerator("Circus", cfg)(11))
	right := resourceIDsForTest(t, RGDGenerator("Circus", cfg)(11))
	if !equalStringSlices(left, right) {
		t.Fatalf("expected deterministic circus output for same prefix/index/seed")
	}

	otherPrefix := resourceIDsForTest(t, RGDGenerator("AnotherCircus", cfg)(11))
	if !equalStringSlices(left, otherPrefix) {
		t.Fatalf("expected deterministic circus output across prefixes for same index/seed")
	}

	other := resourceIDsForTest(t, RGDGenerator("Circus", cfg)(12))
	if equalStringSlices(left, other) {
		t.Fatalf("expected different circus output for different indexes")
	}
}

func TestPlanSyntheticRGDsHigh(t *testing.T) {
	plan, err := PlanSyntheticRGDs("planhigh", DefaultComplexities["high"], 10)
	if err != nil {
		t.Fatalf("plan synthetic high: %v", err)
	}

	if got, want := plan.TotalRGDs, 10; got != want {
		t.Fatalf("expected %d RGDs, got %d", want, got)
	}
	if got, want := plan.TotalResources, 1000; got != want {
		t.Fatalf("expected %d total resources, got %d", want, got)
	}
	if got, want := plan.AverageResources, 100.0; got != want {
		t.Fatalf("expected average resources %.1f, got %.1f", want, got)
	}
	if got, want := plan.MinResources, 100; got != want {
		t.Fatalf("expected min resources %d, got %d", want, got)
	}
	if got, want := plan.MaxResources, 100; got != want {
		t.Fatalf("expected max resources %d, got %d", want, got)
	}
	if got, want := plan.UniqueResourceShapes, 1; got != want {
		t.Fatalf("expected unique resource shapes %d, got %d", want, got)
	}
	if got, want := plan.UniqueStatusShapes, 1; got != want {
		t.Fatalf("expected unique status shapes %d, got %d", want, got)
	}
}

func TestPlanSyntheticRGDsCircusVariesButIsStable(t *testing.T) {
	cfg := DefaultComplexities["circus"]
	plan, err := PlanSyntheticRGDs("circus-a", cfg, 25)
	if err != nil {
		t.Fatalf("plan synthetic circus: %v", err)
	}
	planOtherPrefix, err := PlanSyntheticRGDs("circus-b", cfg, 25)
	if err != nil {
		t.Fatalf("plan synthetic circus other prefix: %v", err)
	}

	if plan.TotalRGDs != 25 {
		t.Fatalf("expected 25 RGDs, got %d", plan.TotalRGDs)
	}
	if plan.MinResources < 50 || plan.MaxResources > 95 {
		t.Fatalf("expected circus resource range within [50,95], got [%d,%d]", plan.MinResources, plan.MaxResources)
	}
	if plan.UniqueResourceShapes < 2 {
		t.Fatalf("expected multiple resource shapes, got %d", plan.UniqueResourceShapes)
	}
	if plan.UniqueStatusShapes < 2 {
		t.Fatalf("expected multiple status shapes, got %d", plan.UniqueStatusShapes)
	}
	if plan.TotalResources != planOtherPrefix.TotalResources ||
		plan.AverageResources != planOtherPrefix.AverageResources ||
		plan.MinResources != planOtherPrefix.MinResources ||
		plan.MaxResources != planOtherPrefix.MaxResources ||
		plan.UniqueResourceShapes != planOtherPrefix.UniqueResourceShapes ||
		plan.UniqueStatusShapes != planOtherPrefix.UniqueStatusShapes {
		t.Fatalf("expected circus planning to be prefix-independent")
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

func resourceIDsForTest(t *testing.T, generated *unstructured.Unstructured) []string {
	t.Helper()

	resources, found, err := unstructured.NestedSlice(generated.Object, "spec", "resources")
	if err != nil || !found {
		t.Fatalf("expected resources, found=%v err=%v", found, err)
	}

	ids := make([]string, 0, len(resources))
	for _, resource := range resources {
		resourceMap, ok := resource.(map[string]interface{})
		if !ok {
			t.Fatalf("unexpected resource type %T", resource)
		}
		id, _ := resourceMap["id"].(string)
		ids = append(ids, id)
	}

	return ids
}

func statusLeafCount(t *testing.T, generated *unstructured.Unstructured) int {
	t.Helper()

	statusSchema, found, err := unstructured.NestedMap(generated.Object, "spec", "schema", "status")
	if err != nil || !found {
		t.Fatalf("expected status schema, found=%v err=%v", found, err)
	}
	return countLeafStrings(statusSchema)
}

func countLeafStrings(value interface{}) int {
	switch typed := value.(type) {
	case string:
		return 1
	case []interface{}:
		count := 0
		for _, item := range typed {
			count += countLeafStrings(item)
		}
		return count
	case map[string]interface{}:
		count := 0
		for _, item := range typed {
			count += countLeafStrings(item)
		}
		return count
	default:
		return 0
	}
}

func equalStringSlices(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if left[i] != right[i] {
			return false
		}
	}
	return true
}
