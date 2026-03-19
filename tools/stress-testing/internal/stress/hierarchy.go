package stress

import "fmt"

// RGDShape describes one RGD definition in a composed hierarchy.
// DirectResources counts native/CRD child resources materialized by that RGD.
// ChildInstanceResources counts child-RGD instance objects created by that RGD.
type RGDShape struct {
	Name                   string
	DirectResources        int
	ChildInstanceResources int
}

// HierarchyProfile describes the per-parent-instance footprint of a composed workload.
type HierarchyProfile struct {
	Name string
	RGDs []RGDShape
}

// HierarchyProjection is a concrete projection for a number of top-level instances.
type HierarchyProjection struct {
	Name             string
	RGDDefinitions   int
	ParentInstances  int
	LeafResources    int
	ChildInstanceCRs int
	TotalInstanceCRs int
	TotalObjects     int
}

func GraphHeavyHierarchy() HierarchyProfile {
	definitions := GraphHeavyDefinitions()
	shapes := make([]RGDShape, 0, len(definitions))
	for _, definition := range definitions {
		shapes = append(shapes, RGDShape{
			Name:                   definition.BaseName,
			DirectResources:        definition.DirectResources,
			ChildInstanceResources: definition.ChildInstanceResources,
		})
	}

	return HierarchyProfile{
		Name: "graph-heavy",
		RGDs: shapes,
	}
}

func GraphHeavy50Hierarchy() HierarchyProfile {
	definitions := GraphHeavy50Definitions()
	shapes := make([]RGDShape, 0, len(definitions))
	for _, definition := range definitions {
		shapes = append(shapes, RGDShape{
			Name:                   definition.BaseName,
			DirectResources:        definition.DirectResources,
			ChildInstanceResources: definition.ChildInstanceResources,
		})
	}

	return HierarchyProfile{
		Name: "graph-heavy-50",
		RGDs: shapes,
	}
}

func HierarchyByName(name string) (HierarchyProfile, bool) {
	switch name {
	case "graph-heavy":
		return GraphHeavyHierarchy(), true
	case "graph-heavy-50":
		return GraphHeavy50Hierarchy(), true
	default:
		return HierarchyProfile{}, false
	}
}

func (profile HierarchyProfile) DefinitionCount() int {
	return len(profile.RGDs)
}

func (profile HierarchyProfile) LeafResourcesPerParentInstance() int {
	total := 0
	for _, rgd := range profile.RGDs {
		total += rgd.DirectResources
	}
	return total
}

func (profile HierarchyProfile) ChildInstancesPerParentInstance() int {
	total := 0
	for _, rgd := range profile.RGDs {
		total += rgd.ChildInstanceResources
	}
	return total
}

func (profile HierarchyProfile) InstanceCRsPerParentInstance() int {
	// One top-level instance plus all child-instance resources spawned by the hierarchy.
	return 1 + profile.ChildInstancesPerParentInstance()
}

func (profile HierarchyProfile) ObjectsPerParentInstance(includeInstances bool) int {
	total := profile.LeafResourcesPerParentInstance()
	if includeInstances {
		total += profile.InstanceCRsPerParentInstance()
	}
	return total
}

func (profile HierarchyProfile) Project(parentInstances int) HierarchyProjection {
	if parentInstances < 0 {
		parentInstances = 0
	}

	leafResources := profile.LeafResourcesPerParentInstance() * parentInstances
	childInstanceCRs := profile.ChildInstancesPerParentInstance() * parentInstances
	totalInstanceCRs := profile.InstanceCRsPerParentInstance() * parentInstances

	return HierarchyProjection{
		Name:             profile.Name,
		RGDDefinitions:   profile.DefinitionCount(),
		ParentInstances:  parentInstances,
		LeafResources:    leafResources,
		ChildInstanceCRs: childInstanceCRs,
		TotalInstanceCRs: totalInstanceCRs,
		TotalObjects:     leafResources + totalInstanceCRs,
	}
}

func (profile HierarchyProfile) ProjectForTarget(targetTotal int, includeInstances bool) (HierarchyProjection, error) {
	if targetTotal <= 0 {
		return HierarchyProjection{}, fmt.Errorf("target total must be > 0")
	}

	perParent := profile.ObjectsPerParentInstance(includeInstances)
	if perParent <= 0 {
		return HierarchyProjection{}, fmt.Errorf("hierarchy %q has zero objects per parent instance", profile.Name)
	}

	return profile.Project(ceilDiv(targetTotal, perParent)), nil
}

func ceilDiv(total, divisor int) int {
	if divisor <= 0 {
		return 0
	}
	return (total + divisor - 1) / divisor
}
