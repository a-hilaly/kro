package stress

import (
	"bytes"
	"testing"
)

func TestGraphHeavyHierarchyFootprint(t *testing.T) {
	profile := GraphHeavyHierarchy()

	if got, want := profile.DefinitionCount(), 10; got != want {
		t.Fatalf("expected %d RGD definitions, got %d", want, got)
	}
	if got, want := profile.LeafResourcesPerParentInstance(), 70; got != want {
		t.Fatalf("expected %d leaf resources per parent instance, got %d", want, got)
	}
	if got, want := profile.ChildInstancesPerParentInstance(), 9; got != want {
		t.Fatalf("expected %d child instance resources per parent instance, got %d", want, got)
	}
	if got, want := profile.InstanceCRsPerParentInstance(), 10; got != want {
		t.Fatalf("expected %d total instance CRs per parent instance, got %d", want, got)
	}
	if got, want := profile.ObjectsPerParentInstance(true), 80; got != want {
		t.Fatalf("expected %d total objects per parent instance, got %d", want, got)
	}
}

func TestGraphHeavyProjectionForLeafTarget(t *testing.T) {
	profile := GraphHeavyHierarchy()

	projection, err := profile.ProjectForTarget(1_000_000, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if got, want := projection.ParentInstances, 14286; got != want {
		t.Fatalf("expected %d parent instances, got %d", want, got)
	}
	if got, want := projection.LeafResources, 1_000_020; got != want {
		t.Fatalf("expected %d leaf resources, got %d", want, got)
	}
	if got, want := projection.TotalInstanceCRs, 142_860; got != want {
		t.Fatalf("expected %d total instance CRs, got %d", want, got)
	}
	if got, want := projection.TotalObjects, 1_142_880; got != want {
		t.Fatalf("expected %d total objects, got %d", want, got)
	}
}

func TestGraphHeavyProjectionForTotalObjectTarget(t *testing.T) {
	profile := GraphHeavyHierarchy()

	projection, err := profile.ProjectForTarget(1_000_000, true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if got, want := projection.ParentInstances, 12500; got != want {
		t.Fatalf("expected %d parent instances, got %d", want, got)
	}
	if got, want := projection.LeafResources, 875_000; got != want {
		t.Fatalf("expected %d leaf resources, got %d", want, got)
	}
	if got, want := projection.TotalInstanceCRs, 125_000; got != want {
		t.Fatalf("expected %d total instance CRs, got %d", want, got)
	}
	if got, want := projection.TotalObjects, 1_000_000; got != want {
		t.Fatalf("expected %d total objects, got %d", want, got)
	}
}

func TestGraphHeavy50HierarchyFootprint(t *testing.T) {
	profile := GraphHeavy50Hierarchy()

	if got, want := profile.DefinitionCount(), 50; got != want {
		t.Fatalf("expected %d RGD definitions, got %d", want, got)
	}
	if got, want := profile.LeafResourcesPerParentInstance(), 256; got != want {
		t.Fatalf("expected %d leaf resources per parent instance, got %d", want, got)
	}
	if got, want := profile.ChildInstancesPerParentInstance(), 49; got != want {
		t.Fatalf("expected %d child instance resources per parent instance, got %d", want, got)
	}
	if got, want := profile.InstanceCRsPerParentInstance(), 50; got != want {
		t.Fatalf("expected %d total instance CRs per parent instance, got %d", want, got)
	}
	if got, want := profile.ObjectsPerParentInstance(true), 306; got != want {
		t.Fatalf("expected %d total objects per parent instance, got %d", want, got)
	}
}

func TestGraphHeavy50ProjectionForTotalObjectTarget(t *testing.T) {
	profile := GraphHeavy50Hierarchy()

	projection, err := profile.ProjectForTarget(1_000_000, true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if got, want := projection.ParentInstances, 3268; got != want {
		t.Fatalf("expected %d parent instances, got %d", want, got)
	}
	if got, want := projection.LeafResources, 836_608; got != want {
		t.Fatalf("expected %d leaf resources, got %d", want, got)
	}
	if got, want := projection.TotalInstanceCRs, 163_400; got != want {
		t.Fatalf("expected %d total instance CRs, got %d", want, got)
	}
	if got, want := projection.TotalObjects, 1_000_008; got != want {
		t.Fatalf("expected %d total objects, got %d", want, got)
	}
}

func TestGraphHeavyRenderDoesNotEmitNullTypes(t *testing.T) {
	rendered, err := RenderHierarchy("graph-heavy")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, document := range rendered {
		if bytes.Contains(document.YAML, []byte("types: null")) {
			t.Fatalf("rendered %s with null schema types", document.Definition.ResourceGraphName)
		}
	}
}

func TestGraphHeavy50RenderDoesNotEmitNullTypes(t *testing.T) {
	rendered, err := RenderHierarchy("graph-heavy-50")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, document := range rendered {
		if bytes.Contains(document.YAML, []byte("types: null")) {
			t.Fatalf("rendered %s with null schema types", document.Definition.ResourceGraphName)
		}
	}
}

func TestGraphHeavy50RenderContainsStatusChainingAndIncludeWhen(t *testing.T) {
	rendered, err := RenderHierarchy("graph-heavy-50")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var combined bytes.Buffer
	for _, document := range rendered {
		combined.Write(document.YAML)
		combined.WriteByte('\n')
	}

	output := combined.Bytes()
	wantSnippets := [][]byte{
		[]byte("${platform.status.sharedConfigName}"),
		[]byte("${platform.status.authSecretName}"),
		[]byte("${platformNetworkMain.status.subnetAName}"),
		[]byte("${webMain.status.configName}"),
		[]byte(".status.state == 'ACTIVE'"),
		[]byte("allChildrenReady:"),
		[]byte("includeWhen:"),
		[]byte("${schema.spec.includeSecondaryResources}"),
	}

	for _, snippet := range wantSnippets {
		if !bytes.Contains(output, snippet) {
			t.Fatalf("expected rendered hierarchy to contain %q", string(snippet))
		}
	}
}
