package stress

import (
	"fmt"
	"strings"

	"github.com/kubernetes-sigs/kro/pkg/testutil/generator"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/yaml"
)

type hierarchyNode struct {
	Definition HierarchyDefinition
	Builder    string
	Component  string
	Children   []string
}

func GraphHeavy50Definitions() []HierarchyDefinition {
	nodes := graphHeavy50Nodes()
	definitions := make([]HierarchyDefinition, 0, len(nodes))
	for _, node := range nodes {
		definitions = append(definitions, node.Definition)
	}
	return definitions
}

func renderGraphHeavy50Hierarchy() ([]RenderedHierarchyRGD, error) {
	nodes := graphHeavy50Nodes()
	index := make(map[string]hierarchyNode, len(nodes))
	rendered := make([]RenderedHierarchyRGD, 0, len(nodes))

	for _, node := range nodes {
		index[node.Definition.BaseName] = node
	}

	for _, node := range nodes {
		object, err := buildGraphHeavy50RGD(node, index)
		if err != nil {
			return nil, err
		}
		document, err := yaml.Marshal(object.Object)
		if err != nil {
			return nil, fmt.Errorf("marshal %s: %w", node.Definition.ResourceGraphName, err)
		}

		rendered = append(rendered, RenderedHierarchyRGD{
			Definition: node.Definition,
			Filename:   node.Definition.ResourceGraphName + ".yaml",
			Object:     object,
			YAML:       document,
		})
	}

	return rendered, nil
}

func buildGraphHeavy50RGD(node hierarchyNode, index map[string]hierarchyNode) (*unstructured.Unstructured, error) {
	if len(node.Children) > 0 {
		return buildGraphHeavyAggregate(node, index), nil
	}

	switch node.Builder {
	case "heavy-core":
		return buildGraphHeavyPlatformCoreWithComponent(node.Definition, node.Component), nil
	case "web":
		return buildGraphHeavyWorkloadSlice(node.Definition, node.Component), nil
	case "api":
		return buildGraphHeavyWorkloadSlice(node.Definition, node.Component), nil
	case "worker":
		return buildGraphHeavyWorkloadSlice(node.Definition, node.Component), nil
	case "auth":
		return buildGraphHeavyAuthWithComponent(node.Definition, node.Component), nil
	case "rbac":
		return buildGraphHeavyRBACWithComponent(node.Definition, node.Component), nil
	case "config":
		return buildGraphHeavyConfigWithComponent(node.Definition, node.Component), nil
	case "ack-network":
		return buildGraphHeavyAckNetworkWithComponent(node.Definition, node.Component), nil
	case "ack-data":
		return buildGraphHeavyAckData(node.Definition), nil
	case "compound":
		return buildGraphHeavyCompound(node.Definition, node.Component), nil
	default:
		return nil, fmt.Errorf("unknown graph-heavy-50 builder %q", node.Builder)
	}
}

func buildGraphHeavyAggregate(node hierarchyNode, index map[string]hierarchyNode) *unstructured.Unstructured {
	statusChildren := map[string]interface{}{}
	statusFields := map[string]interface{}{
		"children": statusChildren,
	}
	opts := []generator.ResourceGraphDefinitionOption{}
	childReadyChecks := make([]string, 0, len(node.Children))

	for key, value := range graphHeavyAggregateStatusRelays(node.Definition.BaseName) {
		statusFields[key] = value
	}

	for _, childBaseName := range node.Children {
		childNode := index[childBaseName]
		childID := graphHeavyIDToken(childBaseName)
		statusChildren[childID] = fmt.Sprintf("${%s.metadata.name}", childID)
		childReadyChecks = append(childReadyChecks, fmt.Sprintf("%s.status.state == 'ACTIVE'", childID))

		spec := map[string]interface{}{
			"name":      "${schema.spec.name}",
			"namespace": "${schema.spec.namespace}",
			"owner":     "${schema.spec.owner}",
			"tier":      childNode.Component,
			"image":     "${schema.spec.image}",
			"replicas":  "${schema.spec.replicas}",
		}
		for key, value := range graphHeavyPassthroughSpecAssignments() {
			spec[key] = value
		}
		for key, value := range graphHeavyExtraSpecForBuilder(childNode.Builder, childBaseName) {
			spec[key] = value
		}
		for key, value := range graphHeavyAggregateChildSpecOverrides(node.Definition.BaseName, childBaseName) {
			spec[key] = value
		}

		opts = append(opts, generator.WithResource(
			childID,
			map[string]interface{}{
				"apiVersion": "kro.run/v1alpha1",
				"kind":       childNode.Definition.SchemaKind,
				"metadata": map[string]interface{}{
					"name":      fmt.Sprintf("${schema.spec.name}-%s", childBaseName),
					"namespace": "${schema.spec.namespace}",
				},
				"spec": spec,
			},
			graphHeavyChildInstanceReadyWhen(childID),
			nil,
		))
	}

	if len(childReadyChecks) > 0 {
		statusFields["allChildrenReady"] = fmt.Sprintf("${%s}", strings.Join(childReadyChecks, " && "))
	}

	opts = append([]generator.ResourceGraphDefinitionOption{
		generator.WithSchema(
			node.Definition.SchemaKind,
			"v1alpha1",
			graphHeavyCommonSpec(),
			statusFields,
		),
	}, opts...)

	return newGraphHeavyRGD(node.Definition, opts...)
}

func graphHeavyExtraSpecForBuilder(builder string, childBaseName string) map[string]interface{} {
	switch builder {
	case "ack-network":
		return map[string]interface{}{
			"fakeVpcAID":          fmt.Sprintf("${schema.spec.name}-%s-vpc-a", childBaseName),
			"fakeVpcBID":          fmt.Sprintf("${schema.spec.name}-%s-vpc-b", childBaseName),
			"fakeSubnetAID":       fmt.Sprintf("${schema.spec.name}-%s-subnet-a", childBaseName),
			"fakeSubnetBID":       fmt.Sprintf("${schema.spec.name}-%s-subnet-b", childBaseName),
			"fakeSecurityGroupID": fmt.Sprintf("${schema.spec.name}-%s-sg", childBaseName),
			"fakeRouteTableAID":   fmt.Sprintf("${schema.spec.name}-%s-rt-a", childBaseName),
			"fakeRouteTableBID":   fmt.Sprintf("${schema.spec.name}-%s-rt-b", childBaseName),
		}
	case "ack-data":
		return map[string]interface{}{
			"assetsBucketName":     fmt.Sprintf("${schema.spec.name}-%s-assets", childBaseName),
			"logsBucketName":       fmt.Sprintf("${schema.spec.name}-%s-logs", childBaseName),
			"auditBucketName":      fmt.Sprintf("${schema.spec.name}-%s-audit", childBaseName),
			"eventsTableName":      fmt.Sprintf("${schema.spec.name}-%s-events", childBaseName),
			"sessionsTableName":    fmt.Sprintf("${schema.spec.name}-%s-sessions", childBaseName),
			"dbSubnetGroupName":    fmt.Sprintf("${schema.spec.name}-%s-db-subnets", childBaseName),
			"dbInstanceIdentifier": fmt.Sprintf("${schema.spec.name}-%s-db", childBaseName),
			"fakeSubnetAID":        fmt.Sprintf("${schema.spec.name}-%s-subnet-a", childBaseName),
			"fakeSubnetBID":        fmt.Sprintf("${schema.spec.name}-%s-subnet-b", childBaseName),
		}
	default:
		return nil
	}
}

func graphHeavy50Nodes() []hierarchyNode {
	return []hierarchyNode{
		nodeAggregate(1, "stack", "stack", "platform", "workloads", "security", "data", "clones"),
		nodeAggregate(2, "platform", "platform", "platform-core-group", "platform-config-group", "platform-auth-group", "platform-rbac-group", "platform-ack-group"),
		nodeAggregate(3, "workloads", "workloads", "workloads-web-group", "workloads-api-group", "workloads-worker-group", "workloads-batch-group"),
		nodeAggregate(4, "security", "security", "security-secrets-group", "security-identity-group", "security-policy-group"),
		nodeAggregate(5, "data", "data", "data-network-group", "data-storage-group", "data-events-group"),
		nodeAggregate(6, "clones", "clones", "clones-web-group", "clones-api-group", "clones-worker-group", "clones-mixed-group"),
		nodeAggregate(7, "platform-core-group", "platform-core-group", "platform-core-primary", "platform-core-secondary"),
		nodeAggregate(8, "platform-config-group", "platform-config-group", "platform-config-main"),
		nodeAggregate(9, "platform-auth-group", "platform-auth-group", "platform-auth-main"),
		nodeAggregate(10, "platform-rbac-group", "platform-rbac-group", "platform-rbac-main"),
		nodeAggregate(11, "platform-ack-group", "platform-ack-group", "platform-network-main", "platform-data-main"),
		nodeAggregate(12, "workloads-web-group", "workloads-web-group", "web-main", "web-edge"),
		nodeAggregate(13, "workloads-api-group", "workloads-api-group", "api-main", "api-admin"),
		nodeAggregate(14, "workloads-worker-group", "workloads-worker-group", "worker-main"),
		nodeAggregate(15, "workloads-batch-group", "workloads-batch-group", "batch-main"),
		nodeAggregate(16, "security-secrets-group", "security-secrets-group", "security-secrets-main"),
		nodeAggregate(17, "security-identity-group", "security-identity-group", "security-identity-main"),
		nodeAggregate(18, "security-policy-group", "security-policy-group", "security-policy-main"),
		nodeAggregate(19, "data-network-group", "data-network-group", "data-network-main", "data-network-edge"),
		nodeAggregate(20, "data-storage-group", "data-storage-group", "data-storage-main", "data-storage-archive"),
		nodeAggregate(21, "data-events-group", "data-events-group", "data-events-main"),
		nodeAggregate(22, "clones-web-group", "clones-web-group", "web-clone-a"),
		nodeAggregate(23, "clones-api-group", "clones-api-group", "api-clone-a"),
		nodeAggregate(24, "clones-worker-group", "clones-worker-group", "worker-clone-a"),
		nodeAggregate(25, "clones-mixed-group", "clones-mixed-group", "mixed-clone-a"),
		nodeLeaf(26, "platform-core-primary", "heavy-core", "platform-core-primary", 20),
		nodeLeaf(27, "platform-core-secondary", "heavy-core", "platform-core-secondary", 20),
		nodeLeaf(28, "platform-config-main", "config", "platform-config-main", 6),
		nodeLeaf(29, "platform-auth-main", "auth", "platform-auth-main", 6),
		nodeLeaf(30, "platform-rbac-main", "rbac", "platform-rbac-main", 6),
		nodeLeaf(31, "platform-network-main", "ack-network", "platform-network-main", 7),
		nodeLeaf(32, "platform-data-main", "ack-data", "platform-data-main", 7),
		nodeLeaf(33, "web-main", "web", "web-main", 6),
		nodeLeaf(34, "web-edge", "heavy-core", "web-edge", 20),
		nodeLeaf(35, "api-main", "api", "api-main", 6),
		nodeLeaf(36, "api-admin", "heavy-core", "api-admin", 20),
		nodeLeaf(37, "worker-main", "worker", "worker-main", 6),
		nodeLeaf(38, "batch-main", "heavy-core", "batch-main", 20),
		nodeLeaf(39, "security-secrets-main", "compound", "security-secrets-main", 10),
		nodeLeaf(40, "security-identity-main", "compound", "security-identity-main", 10),
		nodeLeaf(41, "security-policy-main", "compound", "security-policy-main", 10),
		nodeLeaf(42, "data-network-main", "ack-network", "data-network-main", 7),
		nodeLeaf(43, "data-network-edge", "ack-data", "data-network-edge", 7),
		nodeLeaf(44, "data-storage-main", "ack-data", "data-storage-main", 7),
		nodeLeaf(45, "data-storage-archive", "ack-data", "data-storage-archive", 7),
		nodeLeaf(46, "data-events-main", "compound", "data-events-main", 10),
		nodeLeaf(47, "web-clone-a", "web", "web-clone-a", 6),
		nodeLeaf(48, "api-clone-a", "api", "api-clone-a", 6),
		nodeLeaf(49, "worker-clone-a", "worker", "worker-clone-a", 6),
		nodeLeaf(50, "mixed-clone-a", "heavy-core", "mixed-clone-a", 20),
	}
}

func nodeAggregate(order int, baseName string, component string, children ...string) hierarchyNode {
	return hierarchyNode{
		Definition: newHierarchyDefinition("graph-heavy-50", order, baseName, 0, len(children)),
		Builder:    "aggregate",
		Component:  component,
		Children:   children,
	}
}

func nodeLeaf(order int, baseName string, builder string, component string, directResources int) hierarchyNode {
	return hierarchyNode{
		Definition: newHierarchyDefinition("graph-heavy-50", order, baseName, directResources, 0),
		Builder:    builder,
		Component:  component,
	}
}

func graphHeavyIDToken(value string) string {
	parts := strings.FieldsFunc(value, func(r rune) bool {
		return r == '-' || r == '_' || r == '.'
	})
	if len(parts) == 0 {
		return value
	}

	id := parts[0]
	for _, part := range parts[1:] {
		if part == "" {
			continue
		}
		id += graphHeavyKindToken(part)
	}
	return id
}
