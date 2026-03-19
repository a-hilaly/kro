package stress

import (
	"fmt"
	"sort"
	"strings"

	"github.com/kubernetes-sigs/kro/pkg/testutil/generator"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/yaml"
)

type HierarchyDefinition struct {
	ProfileName            string
	Order                  int
	BaseName               string
	ResourceGraphName      string
	SchemaKind             string
	DirectResources        int
	ChildInstanceResources int
}

type RenderedHierarchyRGD struct {
	Definition HierarchyDefinition
	Filename   string
	Object     *unstructured.Unstructured
	YAML       []byte
}

func GraphHeavyDefinitions() []HierarchyDefinition {
	return []HierarchyDefinition{
		newHierarchyDefinition("graph-heavy", 1, "stack", 0, 9),
		newHierarchyDefinition("graph-heavy", 2, "platform-core", 20, 0),
		newHierarchyDefinition("graph-heavy", 3, "web", 6, 0),
		newHierarchyDefinition("graph-heavy", 4, "api", 6, 0),
		newHierarchyDefinition("graph-heavy", 5, "worker", 6, 0),
		newHierarchyDefinition("graph-heavy", 6, "auth", 6, 0),
		newHierarchyDefinition("graph-heavy", 7, "rbac", 6, 0),
		newHierarchyDefinition("graph-heavy", 8, "config", 6, 0),
		newHierarchyDefinition("graph-heavy", 9, "ack-network", 7, 0),
		newHierarchyDefinition("graph-heavy", 10, "ack-data", 7, 0),
	}
}

func RenderHierarchy(name string) ([]RenderedHierarchyRGD, error) {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "graph-heavy":
		return renderGraphHeavyHierarchy()
	case "graph-heavy-50":
		return renderGraphHeavy50Hierarchy()
	default:
		return nil, fmt.Errorf("unknown hierarchy %q", name)
	}
}

func newHierarchyDefinition(profileName string, order int, baseName string, directResources int, childInstanceResources int) HierarchyDefinition {
	profileToken := graphHeavyKindToken(profileName)
	return HierarchyDefinition{
		ProfileName:            profileName,
		Order:                  order,
		BaseName:               baseName,
		ResourceGraphName:      fmt.Sprintf("%02d-%s-%s.kro.run", order, profileName, baseName),
		SchemaKind:             fmt.Sprintf("%s%02d%s", profileToken, order, graphHeavyKindToken(baseName)),
		DirectResources:        directResources,
		ChildInstanceResources: childInstanceResources,
	}
}

func renderGraphHeavyHierarchy() ([]RenderedHierarchyRGD, error) {
	definitions := GraphHeavyDefinitions()
	index := make(map[string]HierarchyDefinition, len(definitions))
	for _, definition := range definitions {
		index[definition.BaseName] = definition
	}

	rendered := make([]RenderedHierarchyRGD, 0, len(definitions))
	for _, definition := range definitions {
		object, err := buildGraphHeavyRGD(definition, index)
		if err != nil {
			return nil, err
		}
		document, err := yaml.Marshal(object.Object)
		if err != nil {
			return nil, fmt.Errorf("marshal %s: %w", definition.ResourceGraphName, err)
		}

		rendered = append(rendered, RenderedHierarchyRGD{
			Definition: definition,
			Filename:   definition.ResourceGraphName + ".yaml",
			Object:     object,
			YAML:       document,
		})
	}

	sort.Slice(rendered, func(i, j int) bool {
		return rendered[i].Definition.Order < rendered[j].Definition.Order
	})
	return rendered, nil
}

func buildGraphHeavyRGD(definition HierarchyDefinition, index map[string]HierarchyDefinition) (*unstructured.Unstructured, error) {
	switch definition.BaseName {
	case "stack":
		return buildGraphHeavyStack(definition, index), nil
	case "platform-core":
		return buildGraphHeavyPlatformCore(definition), nil
	case "web":
		return buildGraphHeavyWorkloadSlice(definition, "web"), nil
	case "api":
		return buildGraphHeavyWorkloadSlice(definition, "api"), nil
	case "worker":
		return buildGraphHeavyWorkloadSlice(definition, "worker"), nil
	case "auth":
		return buildGraphHeavyAuth(definition), nil
	case "rbac":
		return buildGraphHeavyRBAC(definition), nil
	case "config":
		return buildGraphHeavyConfig(definition), nil
	case "ack-network":
		return buildGraphHeavyAckNetwork(definition), nil
	case "ack-data":
		return buildGraphHeavyAckData(definition), nil
	default:
		return nil, fmt.Errorf("no graph-heavy builder for %q", definition.BaseName)
	}
}

func buildGraphHeavyStack(definition HierarchyDefinition, index map[string]HierarchyDefinition) *unstructured.Unstructured {
	children := []struct {
		id       string
		baseName string
		tier     string
	}{
		{id: "platformCore", baseName: "platform-core", tier: "platform"},
		{id: "web", baseName: "web", tier: "web"},
		{id: "api", baseName: "api", tier: "api"},
		{id: "worker", baseName: "worker", tier: "worker"},
		{id: "auth", baseName: "auth", tier: "auth"},
		{id: "rbac", baseName: "rbac", tier: "rbac"},
		{id: "config", baseName: "config", tier: "config"},
		{id: "ackNetwork", baseName: "ack-network", tier: "network"},
		{id: "ackData", baseName: "ack-data", tier: "data"},
	}

	opts := []generator.ResourceGraphDefinitionOption{
		generator.WithSchema(
			definition.SchemaKind,
			"v1alpha1",
			graphHeavyCommonSpec(),
			map[string]interface{}{
				"allChildrenReady": "${platform.status.state == 'ACTIVE' && web.status.state == 'ACTIVE' && api.status.state == 'ACTIVE' && worker.status.state == 'ACTIVE' && auth.status.state == 'ACTIVE' && rbac.status.state == 'ACTIVE' && config.status.state == 'ACTIVE' && ackNetwork.status.state == 'ACTIVE' && ackData.status.state == 'ACTIVE'}",
				"children": map[string]interface{}{
					"platformCore": "${platformCore.metadata.name}",
					"web":          "${web.metadata.name}",
					"api":          "${api.metadata.name}",
					"worker":       "${worker.metadata.name}",
					"auth":         "${auth.metadata.name}",
					"rbac":         "${rbac.metadata.name}",
					"config":       "${config.metadata.name}",
					"ackNetwork":   "${ackNetwork.metadata.name}",
					"ackData":      "${ackData.metadata.name}",
				},
				"deployments": map[string]interface{}{
					"web":    "${web.status.deploymentName}",
					"api":    "${api.status.deploymentName}",
					"worker": "${worker.status.deploymentName}",
					"config": "${config.status.deploymentName}",
				},
				"serviceAccounts": map[string]interface{}{
					"web":    "${web.status.serviceAccountName}",
					"api":    "${api.status.serviceAccountName}",
					"worker": "${worker.status.serviceAccountName}",
				},
				"infra": map[string]interface{}{
					"assetsBucket":         "${platformCore.status.infra.assetsBucketName}",
					"logsBucket":           "${platformCore.status.infra.logsBucketName}",
					"sharedVpc":            "${platformCore.status.infra.sharedVpcName}",
					"sharedVpcID":          "${platformCore.status.infra.sharedVpcID}",
					"eventsTable":          "${ackData.status.eventsTableName}",
					"sessionsTable":        "${ackData.status.sessionsTableName}",
					"dbInstanceIdentifier": "${ackData.status.dbInstanceIdentifier}",
				},
			},
		),
	}

	for _, child := range children {
		childDef := index[child.baseName]
		spec := map[string]interface{}{
			"name":      "${schema.spec.name}",
			"namespace": "${schema.spec.namespace}",
			"owner":     "${schema.spec.owner}",
			"tier":      child.tier,
			"image":     "${schema.spec.image}",
			"replicas":  "${schema.spec.replicas}",
		}
		for key, value := range graphHeavyPassthroughSpecAssignments() {
			spec[key] = value
		}
		if child.baseName == "ack-network" {
			spec["fakeVpcAID"] = "${schema.spec.name}-vpc-a"
			spec["fakeVpcBID"] = "${schema.spec.name}-vpc-b"
			spec["fakeSubnetAID"] = "${schema.spec.name}-subnet-a"
			spec["fakeSubnetBID"] = "${schema.spec.name}-subnet-b"
			spec["fakeSecurityGroupID"] = "${schema.spec.name}-sg"
			spec["fakeRouteTableAID"] = "${schema.spec.name}-rt-a"
			spec["fakeRouteTableBID"] = "${schema.spec.name}-rt-b"
		}
		if child.baseName == "ack-data" {
			spec["assetsBucketName"] = "${schema.spec.name}-assets"
			spec["logsBucketName"] = "${schema.spec.name}-logs"
			spec["auditBucketName"] = "${schema.spec.name}-audit"
			spec["eventsTableName"] = "${schema.spec.name}-events"
			spec["sessionsTableName"] = "${schema.spec.name}-sessions"
			spec["dbSubnetGroupName"] = "${schema.spec.name}-db-subnets"
			spec["dbInstanceIdentifier"] = "${schema.spec.name}-db"
			spec["fakeSubnetAID"] = "${schema.spec.name}-subnet-a"
			spec["fakeSubnetBID"] = "${schema.spec.name}-subnet-b"
		}
		for key, value := range graphHeavyStackChildSpecOverrides(child.baseName) {
			spec[key] = value
		}

		opts = append(opts, generator.WithResource(
			child.id,
			map[string]interface{}{
				"apiVersion": "kro.run/v1alpha1",
				"kind":       childDef.SchemaKind,
				"metadata": map[string]interface{}{
					"name":      fmt.Sprintf("${schema.spec.name}-%s", child.baseName),
					"namespace": "${schema.spec.namespace}",
				},
				"spec": spec,
			},
			graphHeavyChildInstanceReadyWhen(child.id),
			nil,
		))
	}

	return newGraphHeavyRGD(definition, opts...)
}

func buildGraphHeavyPlatformCore(definition HierarchyDefinition) *unstructured.Unstructured {
	return buildGraphHeavyPlatformCoreWithComponent(definition, "platform")
}

func buildGraphHeavyPlatformCoreWithComponent(definition HierarchyDefinition, component string) *unstructured.Unstructured {
	opts := []generator.ResourceGraphDefinitionOption{
		generator.WithSchema(
			definition.SchemaKind,
			"v1alpha1",
			graphHeavyCommonSpec(),
			map[string]interface{}{
				"config": map[string]interface{}{
					"baseConfigName":    "${baseConfig.metadata.name}",
					"featureFlagsName":  "${featureFlags.metadata.name}",
					"runtimeConfigName": "${runtimeConfig.metadata.name}",
					"policyConfigName":  "${policyConfig.metadata.name}",
				},
				"secrets": map[string]interface{}{
					"platformSecretName": "${platformSecret.metadata.name}",
					"signingSecretName":  "${signingSecret.metadata.name}",
					"tlsSecretName":      "${tlsSecret.metadata.name}",
					"sessionSecretName":  "${sessionSecret.metadata.name}",
				},
				"serviceAccounts": map[string]interface{}{
					"platform": "${platformServiceAccount.metadata.name}",
					"web":      "${webServiceAccount.metadata.name}",
					"api":      "${apiServiceAccount.metadata.name}",
				},
				"roles": map[string]interface{}{
					"platform": "${platformRole.metadata.name}",
					"web":      "${webRole.metadata.name}",
					"api":      "${apiRole.metadata.name}",
				},
				"infra": map[string]interface{}{
					"assetsBucketName": "${assetsBucket.metadata.name}",
					"logsBucketName":   "${logsBucket.metadata.name}",
					"sharedVpcName":    "${sharedVpc.metadata.name}",
					"sharedVpcID":      "${sharedVpc.metadata.name}",
				},
			},
		),
		graphHeavyConfigMap("baseConfig", component, "base", map[string]interface{}{
			"owner":            "${schema.spec.owner}",
			"tier":             "${schema.spec.tier}",
			"component":        component,
			"sharedConfigName": "${schema.spec.sharedConfigName}",
		}),
		graphHeavyConfigMapWithInclude("featureFlags", component, "flags", map[string]interface{}{
			"feature.web":    "enabled",
			"feature.api":    "enabled",
			"feature.worker": "enabled",
		}, graphHeavyIncludeWhen("includeSecondaryResources")),
		graphHeavyConfigMap("runtimeConfig", component, "runtime", map[string]interface{}{
			"image":    "${schema.spec.image}",
			"replicas": "${string(schema.spec.replicas)}",
		}),
		graphHeavyConfigMap("policyConfig", component, "policy", map[string]interface{}{
			"rbac":      "strict",
			"ownership": "${schema.spec.owner}",
		}),
		graphHeavySecret("platformSecret", component, "platform", map[string]interface{}{
			"username": "${schema.spec.name}",
			"token":    "${schema.spec.name}-${schema.spec.owner}",
		}),
		graphHeavySecretWithInclude("signingSecret", component, "signing", map[string]interface{}{
			"issuer": "${schema.spec.name}",
			"kid":    "${schema.spec.name}-signing",
		}, graphHeavyIncludeWhen("includeSecondaryResources")),
		graphHeavySecret("tlsSecret", component, "tls", map[string]interface{}{
			"crt": "${schema.spec.name}-crt",
			"key": "${schema.spec.name}-key",
		}),
		graphHeavySecretWithInclude("sessionSecret", component, "session", map[string]interface{}{
			"session": "${schema.spec.name}-session",
		}, graphHeavyIncludeWhen("includeSecondaryResources")),
		graphHeavyServiceAccount("platformServiceAccount", component, "platform"),
		graphHeavyServiceAccount("webServiceAccount", component, "web"),
		graphHeavyServiceAccount("apiServiceAccount", component, "api"),
		graphHeavyRole("platformRole", component, "platform", []string{"configmaps", "secrets"}),
		graphHeavyRole("webRole", component, "web", []string{"configmaps"}),
		graphHeavyRole("apiRole", component, "api", []string{"configmaps", "secrets"}),
		graphHeavyRoleBindingWithInclude("platformBinding", component, "platform", "${platformRole.metadata.name}", "${platformServiceAccount.metadata.name}", graphHeavyIncludeWhen("includeBindings")),
		graphHeavyRoleBindingWithInclude("webBinding", component, "web", "${webRole.metadata.name}", "${webServiceAccount.metadata.name}", graphHeavyIncludeWhen("includeBindings")),
		graphHeavyRoleBindingWithInclude("apiBinding", component, "api", "${apiRole.metadata.name}", "${apiServiceAccount.metadata.name}", graphHeavyIncludeWhen("includeBindings")),
		graphHeavyBucket("assetsBucket", fmt.Sprintf("${schema.spec.name}-%s-assets", component)),
		graphHeavyBucketWithInclude("logsBucket", fmt.Sprintf("${schema.spec.name}-%s-logs", component), graphHeavyIncludeWhen("includeAuditResources")),
		graphHeavyVPC("sharedVpc", fmt.Sprintf("${schema.spec.name}-%s-vpc", component)),
	}

	return newGraphHeavyRGD(definition, opts...)
}

func buildGraphHeavyWorkloadSlice(definition HierarchyDefinition, component string) *unstructured.Unstructured {
	idPrefix := graphHeavyIDToken(component)
	configID := idPrefix + "Config"
	secretID := idPrefix + "Secret"
	saID := idPrefix + "ServiceAccount"
	roleID := idPrefix + "Role"
	bindingID := idPrefix + "Binding"
	deploymentID := idPrefix + "Deployment"

	opts := []generator.ResourceGraphDefinitionOption{
		generator.WithSchema(
			definition.SchemaKind,
			"v1alpha1",
			graphHeavyCommonSpec(),
			map[string]interface{}{
				"deploymentName":       fmt.Sprintf("${%s.metadata.name}", deploymentID),
				"deploymentGeneration": fmt.Sprintf("${%s.metadata.generation}", deploymentID),
				"serviceAccountName":   fmt.Sprintf("${%s.metadata.name}", saID),
				"configName":           fmt.Sprintf("${%s.metadata.name}", configID),
				"secretName":           fmt.Sprintf("${%s.metadata.name}", secretID),
				"roleName":             fmt.Sprintf("${%s.metadata.name}", roleID),
				"roleBindingName":      fmt.Sprintf("${%s.metadata.name}", bindingID),
			},
		),
		graphHeavyConfigMapWithInclude(configID, component, "config", map[string]interface{}{
			"component":            component,
			"owner":                "${schema.spec.owner}",
			"tier":                 "${schema.spec.tier}",
			"sharedConfigName":     "${schema.spec.sharedConfigName}",
			"connectionSecretName": "${schema.spec.connectionSecretName}",
			"assetsBucketName":     "${schema.spec.assetsBucketName}",
			"eventsTableName":      "${schema.spec.eventsTableName}",
			"sessionsTableName":    "${schema.spec.sessionsTableName}",
		}, graphHeavyIncludeWhen("includePrimaryResources")),
		graphHeavySecretWithInclude(secretID, component, "secret", map[string]interface{}{
			"token":          fmt.Sprintf("${schema.spec.name}-%s-token", component),
			"issuer":         "${schema.spec.owner}",
			"authSecretName": "${schema.spec.authSecretName}",
			"tokenSecretRef": "${schema.spec.tokenSecretName}",
		}, graphHeavyIncludeWhen("includePrimaryResources")),
		graphHeavyServiceAccount(saID, component, component),
		graphHeavyRole(roleID, component, component, []string{"configmaps", "secrets"}),
		graphHeavyRoleBindingWithInclude(bindingID, component, component, fmt.Sprintf("${%s.metadata.name}", roleID), fmt.Sprintf("${%s.metadata.name}", saID), graphHeavyIncludeWhen("includeBindings")),
		graphHeavyDeploymentWithInclude(deploymentID, component, fmt.Sprintf("${%s.metadata.name}", saID), fmt.Sprintf("${%s.metadata.name}", configID), fmt.Sprintf("${%s.metadata.name}", secretID), graphHeavyIncludeWhen("includePrimaryResources")),
	}

	return newGraphHeavyRGD(definition, opts...)
}

func buildGraphHeavyAuth(definition HierarchyDefinition) *unstructured.Unstructured {
	return buildGraphHeavyAuthWithComponent(definition, "auth")
}

func buildGraphHeavyAuthWithComponent(definition HierarchyDefinition, component string) *unstructured.Unstructured {
	idPrefix := graphHeavyIDToken(component)
	configID := idPrefix + "Config"
	serviceAccountID := idPrefix + "ServiceAccount"
	authSecretID := idPrefix + "Secret"
	tokenSecretID := idPrefix + "TokenSecret"
	roleID := idPrefix + "Role"
	bindingID := idPrefix + "Binding"

	opts := []generator.ResourceGraphDefinitionOption{
		generator.WithSchema(
			definition.SchemaKind,
			"v1alpha1",
			graphHeavyCommonSpec(),
			map[string]interface{}{
				"configName":         fmt.Sprintf("${%s.metadata.name}", configID),
				"serviceAccountName": fmt.Sprintf("${%s.metadata.name}", serviceAccountID),
				"authSecretName":     fmt.Sprintf("${%s.metadata.name}", authSecretID),
				"tokenSecretName":    fmt.Sprintf("${%s.metadata.name}", tokenSecretID),
				"roleName":           fmt.Sprintf("${%s.metadata.name}", roleID),
				"roleBindingName":    fmt.Sprintf("${%s.metadata.name}", bindingID),
			},
		),
		graphHeavyConfigMapWithInclude(configID, component, "config", map[string]interface{}{
			"issuer": "${schema.spec.name}",
			"owner":  "${schema.spec.owner}",
		}, graphHeavyIncludeWhen("includePrimaryResources")),
		graphHeavySecret(authSecretID, component, "auth", map[string]interface{}{
			"issuer": "${schema.spec.name}",
			"aud":    "graph-heavy",
		}),
		graphHeavySecretWithInclude(tokenSecretID, component, "token", map[string]interface{}{
			"accessToken":  "${schema.spec.name}-access",
			"refreshToken": "${schema.spec.name}-refresh",
		}, graphHeavyIncludeWhen("includeSecondaryResources")),
		graphHeavyServiceAccount(serviceAccountID, component, component),
		graphHeavyRole(roleID, component, component, []string{"secrets"}),
		graphHeavyRoleBindingWithInclude(bindingID, component, component, fmt.Sprintf("${%s.metadata.name}", roleID), fmt.Sprintf("${%s.metadata.name}", serviceAccountID), graphHeavyIncludeWhen("includeBindings")),
	}
	return newGraphHeavyRGD(definition, opts...)
}

func buildGraphHeavyRBAC(definition HierarchyDefinition) *unstructured.Unstructured {
	return buildGraphHeavyRBACWithComponent(definition, "rbac")
}

func buildGraphHeavyRBACWithComponent(definition HierarchyDefinition, component string) *unstructured.Unstructured {
	opts := []generator.ResourceGraphDefinitionOption{
		generator.WithSchema(
			definition.SchemaKind,
			"v1alpha1",
			graphHeavyCommonSpec(),
			map[string]interface{}{
				"adminRoleName":            "${adminRole.metadata.name}",
				"readerRoleName":           "${readerRole.metadata.name}",
				"adminServiceAccountName":  "${adminServiceAccount.metadata.name}",
				"readerServiceAccountName": "${readerServiceAccount.metadata.name}",
				"adminBindingName":         "${adminBinding.metadata.name}",
				"readerBindingName":        "${readerBinding.metadata.name}",
			},
		),
		graphHeavyServiceAccount("adminServiceAccount", component, "admin"),
		graphHeavyServiceAccountWithInclude("readerServiceAccount", component, "reader", graphHeavyIncludeWhen("includeSecondaryResources")),
		graphHeavyRole("adminRole", component, "admin", []string{"configmaps", "secrets", "serviceaccounts"}),
		graphHeavyRoleWithInclude("readerRole", component, "reader", []string{"configmaps"}, graphHeavyIncludeWhen("includeSecondaryResources")),
		graphHeavyRoleBinding("adminBinding", component, "admin", "${adminRole.metadata.name}", "${adminServiceAccount.metadata.name}"),
		graphHeavyRoleBindingWithInclude("readerBinding", component, "reader", "${readerRole.metadata.name}", "${readerServiceAccount.metadata.name}", graphHeavyIncludeWhen("includeBindings")),
	}
	return newGraphHeavyRGD(definition, opts...)
}

func buildGraphHeavyConfig(definition HierarchyDefinition) *unstructured.Unstructured {
	return buildGraphHeavyConfigWithComponent(definition, "config")
}

func buildGraphHeavyConfigWithComponent(definition HierarchyDefinition, component string) *unstructured.Unstructured {
	opts := []generator.ResourceGraphDefinitionOption{
		generator.WithSchema(
			definition.SchemaKind,
			"v1alpha1",
			graphHeavyCommonSpec(),
			map[string]interface{}{
				"sharedConfigName":     "${sharedConfig.metadata.name}",
				"envConfigName":        "${envConfig.metadata.name}",
				"featureConfigName":    "${featureConfig.metadata.name}",
				"connectionSecretName": "${connectionSecret.metadata.name}",
				"tlsSecretName":        "${tlsSecret.metadata.name}",
				"deploymentName":       "${configDeployment.metadata.name}",
			},
		),
		graphHeavyConfigMap("sharedConfig", component, "shared", map[string]interface{}{
			"name":  "${schema.spec.name}",
			"owner": "${schema.spec.owner}",
		}),
		graphHeavyConfigMap("envConfig", component, "env", map[string]interface{}{
			"ENVIRONMENT": "graph-heavy",
			"TIER":        "${schema.spec.tier}",
		}),
		graphHeavyConfigMapWithInclude("featureConfig", component, "features", map[string]interface{}{
			"featureA": "enabled",
			"featureB": "enabled",
		}, graphHeavyIncludeWhen("includeSecondaryResources")),
		graphHeavySecret("connectionSecret", component, "connection", map[string]interface{}{
			"url":      "postgres://${schema.spec.name}",
			"username": "${schema.spec.name}",
		}),
		graphHeavySecretWithInclude("tlsSecret", component, "tls", map[string]interface{}{
			"crt": "${schema.spec.name}-config-crt",
			"key": "${schema.spec.name}-config-key",
		}, graphHeavyIncludeWhen("includeSecondaryResources")),
		graphHeavyDeploymentWithInclude("configDeployment", component, "", "${sharedConfig.metadata.name}", "${connectionSecret.metadata.name}", graphHeavyIncludeWhen("includePrimaryResources")),
	}
	return newGraphHeavyRGD(definition, opts...)
}

func buildGraphHeavyAckNetwork(definition HierarchyDefinition) *unstructured.Unstructured {
	return buildGraphHeavyAckNetworkWithComponent(definition, "network")
}

func buildGraphHeavyAckNetworkWithComponent(definition HierarchyDefinition, component string) *unstructured.Unstructured {
	spec := graphHeavyCommonSpec()
	spec["fakeVpcAID"] = "string"
	spec["fakeVpcBID"] = "string"
	spec["fakeSubnetAID"] = "string"
	spec["fakeSubnetBID"] = "string"
	spec["fakeSecurityGroupID"] = "string"
	spec["fakeRouteTableAID"] = "string"
	spec["fakeRouteTableBID"] = "string"

	opts := []generator.ResourceGraphDefinitionOption{
		generator.WithSchema(
			definition.SchemaKind,
			"v1alpha1",
			spec,
			map[string]interface{}{
				"vpcAName":          "${appVpc.metadata.name}",
				"vpcBName":          "${sharedVpc.metadata.name}",
				"subnetAName":       "${privateSubnetA.metadata.name}",
				"subnetBName":       "${privateSubnetB.metadata.name}",
				"securityGroupName": "${appSecurityGroup.metadata.name}",
				"routeTableAName":   "${appRouteTable.metadata.name}",
				"routeTableBName":   "${sharedRouteTable.metadata.name}",
			},
		),
		graphHeavyVPC("appVpc", "${schema.spec.fakeVpcAID}"),
		graphHeavyVPCWithInclude("sharedVpc", "${schema.spec.fakeVpcBID}", graphHeavyIncludeWhen("includeSecondaryResources")),
		graphHeavySubnet("privateSubnetA", "${schema.spec.fakeSubnetAID}", "${schema.spec.fakeVpcAID}", "us-west-2a", "10.0.0.0/24"),
		graphHeavySubnet("privateSubnetB", "${schema.spec.fakeSubnetBID}", "${schema.spec.fakeVpcBID}", "us-west-2b", "10.0.1.0/24"),
		graphHeavySecurityGroup("appSecurityGroup", "${schema.spec.fakeSecurityGroupID}", "${schema.spec.fakeVpcAID}"),
		graphHeavyRouteTable("appRouteTable", "${schema.spec.fakeRouteTableAID}", "${schema.spec.fakeVpcAID}"),
		graphHeavyRouteTableWithInclude("sharedRouteTable", "${schema.spec.fakeRouteTableBID}", "${schema.spec.fakeVpcBID}", graphHeavyIncludeWhen("includeSecondaryResources")),
	}
	return newGraphHeavyRGD(definition, opts...)
}

func buildGraphHeavyCompound(definition HierarchyDefinition, component string) *unstructured.Unstructured {
	opts := []generator.ResourceGraphDefinitionOption{
		generator.WithSchema(
			definition.SchemaKind,
			"v1alpha1",
			graphHeavyCommonSpec(),
			map[string]interface{}{
				"deploymentName":              "${compoundDeployment.metadata.name}",
				"serviceAccountName":          "${compoundServiceAccount.metadata.name}",
				"secondaryServiceAccountName": "${batchServiceAccount.metadata.name}",
				"primaryConfigName":           "${primaryConfig.metadata.name}",
				"featureConfigName":           "${featureConfig.metadata.name}",
				"primarySecretName":           "${primarySecret.metadata.name}",
				"tlsSecretName":               "${tlsSecret.metadata.name}",
				"roleName":                    "${compoundRole.metadata.name}",
				"roleBindingName":             "${compoundBinding.metadata.name}",
				"auditBucketName":             "${auditBucket.metadata.name}",
			},
		),
		graphHeavyConfigMap("primaryConfig", component, "primary", map[string]interface{}{
			"component": component,
			"owner":     "${schema.spec.owner}",
		}),
		graphHeavyConfigMapWithInclude("featureConfig", component, "feature", map[string]interface{}{
			"toggleA": "enabled",
			"toggleB": "enabled",
		}, graphHeavyIncludeWhen("includeSecondaryResources")),
		graphHeavySecret("primarySecret", component, "primary", map[string]interface{}{
			"token": "${schema.spec.name}-" + component + "-token",
		}),
		graphHeavySecretWithInclude("tlsSecret", component, "tls", map[string]interface{}{
			"crt": "${schema.spec.name}-" + component + "-crt",
			"key": "${schema.spec.name}-" + component + "-key",
		}, graphHeavyIncludeWhen("includeSecondaryResources")),
		graphHeavyServiceAccount("compoundServiceAccount", component, "primary"),
		graphHeavyServiceAccountWithInclude("batchServiceAccount", component, "batch", graphHeavyIncludeWhen("includeSecondaryResources")),
		graphHeavyRole("compoundRole", component, "role", []string{"configmaps", "secrets"}),
		graphHeavyRoleBindingWithInclude("compoundBinding", component, "binding", "${compoundRole.metadata.name}", "${compoundServiceAccount.metadata.name}", graphHeavyIncludeWhen("includeBindings")),
		graphHeavyDeploymentWithInclude("compoundDeployment", component, "${compoundServiceAccount.metadata.name}", "${primaryConfig.metadata.name}", "${primarySecret.metadata.name}", graphHeavyIncludeWhen("includePrimaryResources")),
		graphHeavyBucketWithInclude("auditBucket", fmt.Sprintf("${schema.spec.name}-%s-audit", component), graphHeavyIncludeWhen("includeAuditResources")),
	}

	return newGraphHeavyRGD(definition, opts...)
}

func buildGraphHeavyAckData(definition HierarchyDefinition) *unstructured.Unstructured {
	spec := graphHeavyCommonSpec()
	spec["assetsBucketName"] = "string"
	spec["logsBucketName"] = "string"
	spec["auditBucketName"] = "string"
	spec["eventsTableName"] = "string"
	spec["sessionsTableName"] = "string"
	spec["dbSubnetGroupName"] = "string"
	spec["dbInstanceIdentifier"] = "string"
	spec["fakeSubnetAID"] = "string"
	spec["fakeSubnetBID"] = "string"

	opts := []generator.ResourceGraphDefinitionOption{
		generator.WithSchema(
			definition.SchemaKind,
			"v1alpha1",
			spec,
			map[string]interface{}{
				"assetsBucketName":     "${assetsBucket.metadata.name}",
				"logsBucketName":       "${logsBucket.metadata.name}",
				"auditBucketName":      "${auditBucket.metadata.name}",
				"eventsTableName":      "${eventsTable.metadata.name}",
				"sessionsTableName":    "${sessionsTable.metadata.name}",
				"dbSubnetGroupName":    "${dbSubnetGroup.metadata.name}",
				"dbInstanceIdentifier": "${appDatabase.metadata.name}",
			},
		),
		graphHeavyBucket("assetsBucket", "${schema.spec.assetsBucketName}"),
		graphHeavyBucket("logsBucket", "${schema.spec.logsBucketName}"),
		graphHeavyBucketWithInclude("auditBucket", "${schema.spec.auditBucketName}", graphHeavyIncludeWhen("includeAuditResources")),
		graphHeavyDynamoTable("eventsTable", "${schema.spec.eventsTableName}"),
		graphHeavyDynamoTableWithInclude("sessionsTable", "${schema.spec.sessionsTableName}", graphHeavyIncludeWhen("includeSecondaryResources")),
		graphHeavyDBSubnetGroup("dbSubnetGroup", "${schema.spec.dbSubnetGroupName}", "${schema.spec.fakeSubnetAID}", "${schema.spec.fakeSubnetBID}"),
		graphHeavyDBInstance("appDatabase", "${schema.spec.dbInstanceIdentifier}", "${dbSubnetGroup.metadata.name}"),
	}
	return newGraphHeavyRGD(definition, opts...)
}

func newGraphHeavyRGD(definition HierarchyDefinition, opts ...generator.ResourceGraphDefinitionOption) *unstructured.Unstructured {
	rgd := generator.NewResourceGraphDefinition(definition.ResourceGraphName, opts...)
	if rgd.Spec.Schema != nil && len(rgd.Spec.Schema.Types.Raw) == 0 && rgd.Spec.Schema.Types.Object == nil {
		rgd.Spec.Schema.Types = runtime.RawExtension{
			Object: &unstructured.Unstructured{Object: map[string]interface{}{}},
			Raw:    []byte("{}"),
		}
	}
	rgd.SetLabels(map[string]string{
		TestLabelKey:                     TestLabelValue,
		"stress.kro.run/hierarchy":       definition.ProfileName,
		"stress.kro.run/hierarchy-index": fmt.Sprintf("%02d", definition.Order),
	})
	return mustToUnstructured(rgd, "kro.run/v1alpha1", "ResourceGraphDefinition")
}

func graphHeavyStackChildSpecOverrides(baseName string) map[string]interface{} {
	switch baseName {
	case "web":
		return map[string]interface{}{
			"sharedConfigName":         "${platformCore.status.config.runtimeConfigName}",
			"sharedServiceAccountName": "${platformCore.status.serviceAccounts.web}",
			"sharedRoleName":           "${rbac.status.readerRoleName}",
			"assetsBucketName":         "${platformCore.status.infra.assetsBucketName}",
			"authSecretName":           "${auth.status.authSecretName}",
			"tokenSecretName":          "${auth.status.tokenSecretName}",
		}
	case "api":
		return map[string]interface{}{
			"sharedConfigName":         "${platformCore.status.config.runtimeConfigName}",
			"sharedServiceAccountName": "${platformCore.status.serviceAccounts.api}",
			"sharedRoleName":           "${rbac.status.adminRoleName}",
			"assetsBucketName":         "${platformCore.status.infra.assetsBucketName}",
			"eventsTableName":          "${ackData.status.eventsTableName}",
			"sessionsTableName":        "${ackData.status.sessionsTableName}",
			"authSecretName":           "${auth.status.authSecretName}",
			"tokenSecretName":          "${auth.status.tokenSecretName}",
			"connectionSecretName":     "${config.status.connectionSecretName}",
		}
	case "worker":
		return map[string]interface{}{
			"sharedConfigName":         "${platformCore.status.config.runtimeConfigName}",
			"sharedServiceAccountName": "${platformCore.status.serviceAccounts.platform}",
			"sharedRoleName":           "${rbac.status.adminRoleName}",
			"assetsBucketName":         "${platformCore.status.infra.assetsBucketName}",
			"eventsTableName":          "${ackData.status.eventsTableName}",
			"sessionsTableName":        "${ackData.status.sessionsTableName}",
			"connectionSecretName":     "${config.status.connectionSecretName}",
		}
	case "ack-data":
		return map[string]interface{}{
			"fakeSubnetAID": "${ackNetwork.status.subnetAName}",
			"fakeSubnetBID": "${ackNetwork.status.subnetBName}",
		}
	default:
		return nil
	}
}

func graphHeavyAggregateStatusRelays(baseName string) map[string]interface{} {
	switch baseName {
	case "platform-core-group":
		return map[string]interface{}{
			"allChildrenReady":               "${platformCorePrimary.status.state == 'ACTIVE' && platformCoreSecondary.status.state == 'ACTIVE'}",
			"sharedConfigName":              "${platformCorePrimary.status.config.runtimeConfigName}",
			"assetsBucketName":              "${platformCorePrimary.status.infra.assetsBucketName}",
			"webSharedServiceAccountName":   "${platformCorePrimary.status.serviceAccounts.web}",
			"apiSharedServiceAccountName":   "${platformCorePrimary.status.serviceAccounts.api}",
			"workerSharedServiceAccountName": "${platformCorePrimary.status.serviceAccounts.platform}",
		}
	case "platform-auth-group":
		return map[string]interface{}{
			"allChildrenReady": "${platformAuthMain.status.state == 'ACTIVE'}",
			"authSecretName":  "${platformAuthMain.status.authSecretName}",
			"tokenSecretName": "${platformAuthMain.status.tokenSecretName}",
		}
	case "platform-config-group":
		return map[string]interface{}{
			"allChildrenReady":       "${platformConfigMain.status.state == 'ACTIVE'}",
			"connectionSecretName": "${platformConfigMain.status.connectionSecretName}",
		}
	case "platform-rbac-group":
		return map[string]interface{}{
			"allChildrenReady":    "${platformRbacMain.status.state == 'ACTIVE'}",
			"webSharedRoleName":    "${platformRbacMain.status.readerRoleName}",
			"apiSharedRoleName":    "${platformRbacMain.status.adminRoleName}",
			"workerSharedRoleName": "${platformRbacMain.status.adminRoleName}",
		}
	case "platform-ack-group":
		return map[string]interface{}{
			"allChildrenReady": "${platformNetworkMain.status.state == 'ACTIVE' && platformDataMain.status.state == 'ACTIVE'}",
			"eventsTableName":   "${platformDataMain.status.eventsTableName}",
			"sessionsTableName": "${platformDataMain.status.sessionsTableName}",
		}
	case "platform":
		return map[string]interface{}{
			"allChildrenReady":               "${platformCoreGroup.status.state == 'ACTIVE' && platformConfigGroup.status.state == 'ACTIVE' && platformAuthGroup.status.state == 'ACTIVE' && platformRbacGroup.status.state == 'ACTIVE' && platformAckGroup.status.state == 'ACTIVE'}",
			"sharedConfigName":              "${platformCoreGroup.status.sharedConfigName}",
			"assetsBucketName":              "${platformCoreGroup.status.assetsBucketName}",
			"connectionSecretName":          "${platformConfigGroup.status.connectionSecretName}",
			"authSecretName":                "${platformAuthGroup.status.authSecretName}",
			"tokenSecretName":               "${platformAuthGroup.status.tokenSecretName}",
			"eventsTableName":               "${platformAckGroup.status.eventsTableName}",
			"sessionsTableName":             "${platformAckGroup.status.sessionsTableName}",
			"webSharedServiceAccountName":   "${platformCoreGroup.status.webSharedServiceAccountName}",
			"apiSharedServiceAccountName":   "${platformCoreGroup.status.apiSharedServiceAccountName}",
			"workerSharedServiceAccountName": "${platformCoreGroup.status.workerSharedServiceAccountName}",
			"webSharedRoleName":             "${platformRbacGroup.status.webSharedRoleName}",
			"apiSharedRoleName":             "${platformRbacGroup.status.apiSharedRoleName}",
			"workerSharedRoleName":          "${platformRbacGroup.status.workerSharedRoleName}",
		}
	default:
		return nil
	}
}

func graphHeavyChildInstanceReadyWhen(id string) []string {
	return []string{fmt.Sprintf("${%s.status.state == 'ACTIVE'}", id)}
}

func graphHeavyAggregateChildSpecOverrides(parentBaseName string, childBaseName string) map[string]interface{} {
	switch parentBaseName {
	case "stack":
		if childBaseName == "workloads" || childBaseName == "clones" {
			return map[string]interface{}{
				"sharedConfigName":              "${platform.status.sharedConfigName}",
				"connectionSecretName":          "${platform.status.connectionSecretName}",
				"authSecretName":                "${platform.status.authSecretName}",
				"tokenSecretName":               "${platform.status.tokenSecretName}",
				"assetsBucketName":              "${platform.status.assetsBucketName}",
				"eventsTableName":               "${platform.status.eventsTableName}",
				"sessionsTableName":             "${platform.status.sessionsTableName}",
				"webSharedServiceAccountName":   "${platform.status.webSharedServiceAccountName}",
				"apiSharedServiceAccountName":   "${platform.status.apiSharedServiceAccountName}",
				"workerSharedServiceAccountName": "${platform.status.workerSharedServiceAccountName}",
				"webSharedRoleName":             "${platform.status.webSharedRoleName}",
				"apiSharedRoleName":             "${platform.status.apiSharedRoleName}",
				"workerSharedRoleName":          "${platform.status.workerSharedRoleName}",
			}
		}
	case "workloads", "clones":
		switch childBaseName {
		case "workloads-web-group", "clones-web-group":
			return map[string]interface{}{
				"sharedConfigName":         "${schema.spec.sharedConfigName}",
				"assetsBucketName":         "${schema.spec.assetsBucketName}",
				"authSecretName":           "${schema.spec.authSecretName}",
				"tokenSecretName":          "${schema.spec.tokenSecretName}",
				"sharedServiceAccountName": "${schema.spec.webSharedServiceAccountName}",
				"sharedRoleName":           "${schema.spec.webSharedRoleName}",
			}
		case "workloads-api-group", "clones-api-group":
			return map[string]interface{}{
				"sharedConfigName":         "${schema.spec.sharedConfigName}",
				"connectionSecretName":     "${schema.spec.connectionSecretName}",
				"authSecretName":           "${schema.spec.authSecretName}",
				"tokenSecretName":          "${schema.spec.tokenSecretName}",
				"assetsBucketName":         "${schema.spec.assetsBucketName}",
				"eventsTableName":          "${schema.spec.eventsTableName}",
				"sessionsTableName":        "${schema.spec.sessionsTableName}",
				"sharedServiceAccountName": "${schema.spec.apiSharedServiceAccountName}",
				"sharedRoleName":           "${schema.spec.apiSharedRoleName}",
			}
		case "workloads-worker-group", "workloads-batch-group", "clones-worker-group", "clones-mixed-group":
			return map[string]interface{}{
				"sharedConfigName":         "${schema.spec.sharedConfigName}",
				"connectionSecretName":     "${schema.spec.connectionSecretName}",
				"assetsBucketName":         "${schema.spec.assetsBucketName}",
				"eventsTableName":          "${schema.spec.eventsTableName}",
				"sessionsTableName":        "${schema.spec.sessionsTableName}",
				"sharedServiceAccountName": "${schema.spec.workerSharedServiceAccountName}",
				"sharedRoleName":           "${schema.spec.workerSharedRoleName}",
			}
		}
	case "platform-ack-group":
		if childBaseName == "platform-data-main" {
			return map[string]interface{}{
				"fakeSubnetAID": "${platformNetworkMain.status.subnetAName}",
				"fakeSubnetBID": "${platformNetworkMain.status.subnetBName}",
			}
		}
	case "workloads-web-group":
		if childBaseName == "web-edge" {
			return map[string]interface{}{
				"sharedConfigName":         "${webMain.status.configName}",
				"sharedServiceAccountName": "${webMain.status.serviceAccountName}",
				"sharedRoleName":           "${webMain.status.roleName}",
				"assetsBucketName":         "${schema.spec.assetsBucketName}",
				"authSecretName":           "${schema.spec.authSecretName}",
				"tokenSecretName":          "${schema.spec.tokenSecretName}",
			}
		}
	case "workloads-api-group":
		if childBaseName == "api-admin" {
			return map[string]interface{}{
				"sharedConfigName":         "${apiMain.status.configName}",
				"sharedServiceAccountName": "${apiMain.status.serviceAccountName}",
				"sharedRoleName":           "${apiMain.status.roleName}",
				"connectionSecretName":     "${schema.spec.connectionSecretName}",
				"assetsBucketName":         "${schema.spec.assetsBucketName}",
				"eventsTableName":          "${schema.spec.eventsTableName}",
				"sessionsTableName":        "${schema.spec.sessionsTableName}",
				"authSecretName":           "${schema.spec.authSecretName}",
				"tokenSecretName":          "${schema.spec.tokenSecretName}",
			}
		}
	case "data-network-group":
		if childBaseName == "data-network-edge" {
			return map[string]interface{}{
				"fakeSubnetAID": "${dataNetworkMain.status.subnetAName}",
				"fakeSubnetBID": "${dataNetworkMain.status.subnetBName}",
			}
		}
	}
	return nil
}

func graphHeavyCommonSpec() map[string]interface{} {
	return map[string]interface{}{
		"name":                         "string",
		"namespace":                    "string | default=default",
		"owner":                        "string | default=stress",
		"tier":                         "string | default=platform",
		"image":                        "string | default=registry.k8s.io/pause:3.10",
		"replicas":                     "integer | default=0",
		"sharedConfigName":             "string | default=\"\"",
		"connectionSecretName":         "string | default=\"\"",
		"authSecretName":               "string | default=\"\"",
		"tokenSecretName":              "string | default=\"\"",
		"assetsBucketName":             "string | default=\"\"",
		"eventsTableName":              "string | default=\"\"",
		"sessionsTableName":            "string | default=\"\"",
		"sharedServiceAccountName":     "string | default=\"\"",
		"sharedRoleName":               "string | default=\"\"",
		"webSharedServiceAccountName":  "string | default=\"\"",
		"apiSharedServiceAccountName":  "string | default=\"\"",
		"workerSharedServiceAccountName": "string | default=\"\"",
		"webSharedRoleName":            "string | default=\"\"",
		"apiSharedRoleName":            "string | default=\"\"",
		"workerSharedRoleName":         "string | default=\"\"",
		"includePrimaryResources":      "boolean | default=true",
		"includeSecondaryResources":    "boolean | default=true",
		"includeBindings":              "boolean | default=true",
		"includeAuditResources":        "boolean | default=true",
		"includeDataResources":         "boolean | default=true",
		"includeCloneResources":        "boolean | default=true",
	}
}

func graphHeavyPassthroughSpecAssignments() map[string]interface{} {
	return map[string]interface{}{
		"sharedConfigName":              "${schema.spec.sharedConfigName}",
		"connectionSecretName":          "${schema.spec.connectionSecretName}",
		"authSecretName":                "${schema.spec.authSecretName}",
		"tokenSecretName":               "${schema.spec.tokenSecretName}",
		"assetsBucketName":              "${schema.spec.assetsBucketName}",
		"eventsTableName":               "${schema.spec.eventsTableName}",
		"sessionsTableName":             "${schema.spec.sessionsTableName}",
		"sharedServiceAccountName":      "${schema.spec.sharedServiceAccountName}",
		"sharedRoleName":                "${schema.spec.sharedRoleName}",
		"webSharedServiceAccountName":   "${schema.spec.webSharedServiceAccountName}",
		"apiSharedServiceAccountName":   "${schema.spec.apiSharedServiceAccountName}",
		"workerSharedServiceAccountName": "${schema.spec.workerSharedServiceAccountName}",
		"webSharedRoleName":             "${schema.spec.webSharedRoleName}",
		"apiSharedRoleName":             "${schema.spec.apiSharedRoleName}",
		"workerSharedRoleName":          "${schema.spec.workerSharedRoleName}",
		"includePrimaryResources":       "${schema.spec.includePrimaryResources}",
		"includeSecondaryResources":     "${schema.spec.includeSecondaryResources}",
		"includeBindings":               "${schema.spec.includeBindings}",
		"includeAuditResources":         "${schema.spec.includeAuditResources}",
		"includeDataResources":          "${schema.spec.includeDataResources}",
		"includeCloneResources":         "${schema.spec.includeCloneResources}",
	}
}

func graphHeavyIncludeWhen(flag string) []string {
	return []string{fmt.Sprintf("${schema.spec.%s}", flag)}
}

func graphHeavyConfigMap(id string, component string, suffix string, data map[string]interface{}) generator.ResourceGraphDefinitionOption {
	return graphHeavyConfigMapWithInclude(id, component, suffix, data, nil)
}

func graphHeavyConfigMapWithInclude(id string, component string, suffix string, data map[string]interface{}, includeWhen []string) generator.ResourceGraphDefinitionOption {
	return generator.WithResource(
		id,
		map[string]interface{}{
			"apiVersion": "v1",
			"kind":       "ConfigMap",
			"metadata": map[string]interface{}{
				"name":      fmt.Sprintf("${schema.spec.name}-%s-%s", component, suffix),
				"namespace": "${schema.spec.namespace}",
				"labels": map[string]interface{}{
					"graph-heavy.kro.run/component": component,
				},
			},
			"data": data,
		},
		nil,
		includeWhen,
	)
}

func graphHeavySecret(id string, component string, suffix string, data map[string]interface{}) generator.ResourceGraphDefinitionOption {
	return graphHeavySecretWithInclude(id, component, suffix, data, nil)
}

func graphHeavySecretWithInclude(id string, component string, suffix string, data map[string]interface{}, includeWhen []string) generator.ResourceGraphDefinitionOption {
	return generator.WithResource(
		id,
		map[string]interface{}{
			"apiVersion": "v1",
			"kind":       "Secret",
			"metadata": map[string]interface{}{
				"name":      fmt.Sprintf("${schema.spec.name}-%s-%s", component, suffix),
				"namespace": "${schema.spec.namespace}",
			},
			"type":       "Opaque",
			"stringData": data,
		},
		nil,
		includeWhen,
	)
}

func graphHeavyServiceAccount(id string, component string, suffix string) generator.ResourceGraphDefinitionOption {
	return graphHeavyServiceAccountWithInclude(id, component, suffix, nil)
}

func graphHeavyServiceAccountWithInclude(id string, component string, suffix string, includeWhen []string) generator.ResourceGraphDefinitionOption {
	return generator.WithResource(
		id,
		map[string]interface{}{
			"apiVersion": "v1",
			"kind":       "ServiceAccount",
			"metadata": map[string]interface{}{
				"name":      fmt.Sprintf("${schema.spec.name}-%s-%s", component, suffix),
				"namespace": "${schema.spec.namespace}",
			},
		},
		nil,
		includeWhen,
	)
}

func graphHeavyRole(id string, component string, suffix string, resources []string) generator.ResourceGraphDefinitionOption {
	return graphHeavyRoleWithInclude(id, component, suffix, resources, nil)
}

func graphHeavyRoleWithInclude(id string, component string, suffix string, resources []string, includeWhen []string) generator.ResourceGraphDefinitionOption {
	resourceList := make([]interface{}, 0, len(resources))
	for _, resource := range resources {
		resourceList = append(resourceList, resource)
	}

	return generator.WithResource(
		id,
		map[string]interface{}{
			"apiVersion": "rbac.authorization.k8s.io/v1",
			"kind":       "Role",
			"metadata": map[string]interface{}{
				"name":      fmt.Sprintf("${schema.spec.name}-%s-%s", component, suffix),
				"namespace": "${schema.spec.namespace}",
			},
			"rules": []interface{}{
				map[string]interface{}{
					"apiGroups": []interface{}{""},
					"resources": resourceList,
					"verbs":     []interface{}{"get", "list", "watch"},
				},
			},
		},
		nil,
		includeWhen,
	)
}

func graphHeavyRoleBinding(id string, component string, suffix string, roleName string, serviceAccountName string) generator.ResourceGraphDefinitionOption {
	return graphHeavyRoleBindingWithInclude(id, component, suffix, roleName, serviceAccountName, nil)
}

func graphHeavyRoleBindingWithInclude(id string, component string, suffix string, roleName string, serviceAccountName string, includeWhen []string) generator.ResourceGraphDefinitionOption {
	return generator.WithResource(
		id,
		map[string]interface{}{
			"apiVersion": "rbac.authorization.k8s.io/v1",
			"kind":       "RoleBinding",
			"metadata": map[string]interface{}{
				"name":      fmt.Sprintf("${schema.spec.name}-%s-%s", component, suffix),
				"namespace": "${schema.spec.namespace}",
			},
			"roleRef": map[string]interface{}{
				"apiGroup": "rbac.authorization.k8s.io",
				"kind":     "Role",
				"name":     fmt.Sprintf("${schema.spec.sharedRoleName != '' ? schema.spec.sharedRoleName : %s}", trimCELWrapper(roleName)),
			},
			"subjects": []interface{}{
				map[string]interface{}{
					"kind":      "ServiceAccount",
					"name":      fmt.Sprintf("${schema.spec.sharedServiceAccountName != '' ? schema.spec.sharedServiceAccountName : %s}", trimCELWrapper(serviceAccountName)),
					"namespace": "${schema.spec.namespace}",
				},
			},
		},
		nil,
		includeWhen,
	)
}

func graphHeavyDeployment(id string, component string, serviceAccountName string, configName string, secretName string) generator.ResourceGraphDefinitionOption {
	return graphHeavyDeploymentWithInclude(id, component, serviceAccountName, configName, secretName, nil)
}

func graphHeavyDeploymentWithInclude(id string, component string, serviceAccountName string, configName string, secretName string, includeWhen []string) generator.ResourceGraphDefinitionOption {
	spec := map[string]interface{}{
		"replicas": "${schema.spec.replicas}",
		"selector": map[string]interface{}{
			"matchLabels": map[string]interface{}{
				"graph-heavy.kro.run/component": component,
				"graph-heavy.kro.run/name":      "${schema.spec.name}",
			},
		},
		"template": map[string]interface{}{
			"metadata": map[string]interface{}{
				"labels": map[string]interface{}{
					"graph-heavy.kro.run/component": component,
					"graph-heavy.kro.run/name":      "${schema.spec.name}",
				},
			},
			"spec": map[string]interface{}{
				"containers": []interface{}{
					map[string]interface{}{
						"name":  component,
						"image": "${schema.spec.image}",
						"env": []interface{}{
							map[string]interface{}{"name": "SHARED_CONFIG_NAME", "value": "${schema.spec.sharedConfigName}"},
							map[string]interface{}{"name": "AUTH_SECRET_NAME", "value": "${schema.spec.authSecretName}"},
							map[string]interface{}{"name": "TOKEN_SECRET_NAME", "value": "${schema.spec.tokenSecretName}"},
							map[string]interface{}{"name": "CONNECTION_SECRET_NAME", "value": "${schema.spec.connectionSecretName}"},
							map[string]interface{}{"name": "ASSETS_BUCKET_NAME", "value": "${schema.spec.assetsBucketName}"},
							map[string]interface{}{"name": "EVENTS_TABLE_NAME", "value": "${schema.spec.eventsTableName}"},
							map[string]interface{}{"name": "SESSIONS_TABLE_NAME", "value": "${schema.spec.sessionsTableName}"},
						},
						"envFrom": []interface{}{
							map[string]interface{}{
								"configMapRef": map[string]interface{}{"name": configName},
							},
							map[string]interface{}{
								"secretRef": map[string]interface{}{"name": secretName},
							},
						},
					},
				},
			},
		},
	}
	if serviceAccountName != "" {
		templateSpec := spec["template"].(map[string]interface{})["spec"].(map[string]interface{})
		templateSpec["serviceAccountName"] = fmt.Sprintf("${schema.spec.sharedServiceAccountName != '' ? schema.spec.sharedServiceAccountName : %s}", trimCELWrapper(serviceAccountName))
	}

	return generator.WithResource(
		id,
		map[string]interface{}{
			"apiVersion": "apps/v1",
			"kind":       "Deployment",
			"metadata": map[string]interface{}{
				"name":      fmt.Sprintf("${schema.spec.name}-%s", component),
				"namespace": "${schema.spec.namespace}",
			},
			"spec": spec,
		},
		[]string{fmt.Sprintf("${%s.spec.replicas == 0 || %s.status.readyReplicas == %s.spec.replicas}", id, id, id)},
		includeWhen,
	)
}

func graphHeavyBucket(id string, name string) generator.ResourceGraphDefinitionOption {
	return graphHeavyBucketWithInclude(id, name, nil)
}

func graphHeavyBucketWithInclude(id string, name string, includeWhen []string) generator.ResourceGraphDefinitionOption {
	return generator.WithResource(
		id,
		map[string]interface{}{
			"apiVersion": "s3.services.k8s.aws/v1alpha1",
			"kind":       "Bucket",
			"metadata": map[string]interface{}{
				"name": name,
			},
			"spec": map[string]interface{}{
				"name": name,
			},
		},
		[]string{fmt.Sprintf("${%s.metadata.generation > 0}", id)},
		includeWhen,
	)
}

func graphHeavyVPC(id string, name string) generator.ResourceGraphDefinitionOption {
	return graphHeavyVPCWithInclude(id, name, nil)
}

func graphHeavyVPCWithInclude(id string, name string, includeWhen []string) generator.ResourceGraphDefinitionOption {
	return generator.WithResource(
		id,
		map[string]interface{}{
			"apiVersion": "ec2.services.k8s.aws/v1alpha1",
			"kind":       "VPC",
			"metadata": map[string]interface{}{
				"name": name,
			},
			"spec": map[string]interface{}{
				"cidrBlocks":       []interface{}{"10.0.0.0/16"},
				"enableDNSSupport": true,
			},
		},
		[]string{fmt.Sprintf("${%s.metadata.generation > 0}", id)},
		includeWhen,
	)
}

func graphHeavySubnet(id string, name string, vpcID string, availabilityZone string, cidrBlock string) generator.ResourceGraphDefinitionOption {
	return generator.WithResource(
		id,
		map[string]interface{}{
			"apiVersion": "ec2.services.k8s.aws/v1alpha1",
			"kind":       "Subnet",
			"metadata": map[string]interface{}{
				"name": name,
			},
			"spec": map[string]interface{}{
				"availabilityZone": availabilityZone,
				"cidrBlock":        cidrBlock,
				"vpcID":            vpcID,
			},
		},
		[]string{fmt.Sprintf("${%s.metadata.generation > 0}", id)},
		nil,
	)
}

func graphHeavySecurityGroup(id string, name string, vpcID string) generator.ResourceGraphDefinitionOption {
	return generator.WithResource(
		id,
		map[string]interface{}{
			"apiVersion": "ec2.services.k8s.aws/v1alpha1",
			"kind":       "SecurityGroup",
			"metadata": map[string]interface{}{
				"name": name,
			},
			"spec": map[string]interface{}{
				"description": "graph-heavy synthetic security group",
				"name":        name,
				"vpcID":       vpcID,
			},
		},
		[]string{fmt.Sprintf("${%s.metadata.generation > 0}", id)},
		nil,
	)
}

func graphHeavyRouteTable(id string, name string, vpcID string) generator.ResourceGraphDefinitionOption {
	return graphHeavyRouteTableWithInclude(id, name, vpcID, nil)
}

func graphHeavyRouteTableWithInclude(id string, name string, vpcID string, includeWhen []string) generator.ResourceGraphDefinitionOption {
	return generator.WithResource(
		id,
		map[string]interface{}{
			"apiVersion": "ec2.services.k8s.aws/v1alpha1",
			"kind":       "RouteTable",
			"metadata": map[string]interface{}{
				"name": name,
			},
			"spec": map[string]interface{}{
				"vpcID": vpcID,
			},
		},
		[]string{fmt.Sprintf("${%s.metadata.generation > 0}", id)},
		includeWhen,
	)
}

func graphHeavyDynamoTable(id string, name string) generator.ResourceGraphDefinitionOption {
	return graphHeavyDynamoTableWithInclude(id, name, nil)
}

func graphHeavyDynamoTableWithInclude(id string, name string, includeWhen []string) generator.ResourceGraphDefinitionOption {
	return generator.WithResource(
		id,
		map[string]interface{}{
			"apiVersion": "dynamodb.services.k8s.aws/v1alpha1",
			"kind":       "Table",
			"metadata": map[string]interface{}{
				"name": name,
			},
			"spec": map[string]interface{}{
				"attributeDefinitions": []interface{}{
					map[string]interface{}{"attributeName": "id", "attributeType": "S"},
				},
				"billingMode": "PAY_PER_REQUEST",
				"keySchema": []interface{}{
					map[string]interface{}{"attributeName": "id", "keyType": "HASH"},
				},
				"tableName": name,
			},
		},
		[]string{fmt.Sprintf("${%s.metadata.generation > 0}", id)},
		includeWhen,
	)
}

func graphHeavyDBSubnetGroup(id string, name string, subnetA string, subnetB string) generator.ResourceGraphDefinitionOption {
	return generator.WithResource(
		id,
		map[string]interface{}{
			"apiVersion": "rds.services.k8s.aws/v1alpha1",
			"kind":       "DBSubnetGroup",
			"metadata": map[string]interface{}{
				"name": name,
			},
			"spec": map[string]interface{}{
				"description": "graph-heavy synthetic db subnet group",
				"name":        name,
				"subnetIDs":   []interface{}{subnetA, subnetB},
			},
		},
		[]string{fmt.Sprintf("${%s.metadata.generation > 0}", id)},
		nil,
	)
}

func graphHeavyDBInstance(id string, identifier string, subnetGroupName string) generator.ResourceGraphDefinitionOption {
	return generator.WithResource(
		id,
		map[string]interface{}{
			"apiVersion": "rds.services.k8s.aws/v1alpha1",
			"kind":       "DBInstance",
			"metadata": map[string]interface{}{
				"name": identifier,
			},
			"spec": map[string]interface{}{
				"allocatedStorage":     20,
				"dbInstanceClass":      "db.t4g.micro",
				"dbInstanceIdentifier": identifier,
				"dbSubnetGroupName":    subnetGroupName,
				"engine":               "postgres",
				"masterUsername":       "graphheavy",
			},
		},
		[]string{fmt.Sprintf("${%s.metadata.generation > 0}", id)},
		nil,
	)
}

func graphHeavyKindToken(value string) string {
	parts := strings.FieldsFunc(value, func(r rune) bool {
		return r == '-' || r == '_' || r == '.'
	})
	var builder strings.Builder
	for _, part := range parts {
		if part == "" {
			continue
		}
		builder.WriteString(strings.ToUpper(part[:1]))
		if len(part) > 1 {
			builder.WriteString(part[1:])
		}
	}
	return builder.String()
}

func trimCELWrapper(value string) string {
	if strings.HasPrefix(value, "${") && strings.HasSuffix(value, "}") {
		return strings.TrimSuffix(strings.TrimPrefix(value, "${"), "}")
	}
	return fmt.Sprintf("%q", value)
}

func HierarchySummary(rendered []RenderedHierarchyRGD) string {
	var builder strings.Builder
	totalLeaf := 0
	totalChildren := 0

	profileName := "unknown"
	if len(rendered) > 0 {
		profileName = rendered[0].Definition.ProfileName
	}
	builder.WriteString(fmt.Sprintf("Hierarchy: %s\n", profileName))
	builder.WriteString("Definitions:\n")
	for _, manifest := range rendered {
		totalLeaf += manifest.Definition.DirectResources
		totalChildren += manifest.Definition.ChildInstanceResources
		builder.WriteString(fmt.Sprintf(
			"- %02d %s | kind=%s | leaf-resources=%d | child-instance-resources=%d | file=%s\n",
			manifest.Definition.Order,
			manifest.Definition.ResourceGraphName,
			manifest.Definition.SchemaKind,
			manifest.Definition.DirectResources,
			manifest.Definition.ChildInstanceResources,
			manifest.Filename,
		))
	}

	builder.WriteString(fmt.Sprintf("Leaf resources per parent instance: %d\n", totalLeaf))
	builder.WriteString(fmt.Sprintf("Child instance resources per parent instance: %d\n", totalChildren))
	builder.WriteString(fmt.Sprintf("Total instance CRs per parent instance: %d\n", totalChildren+1))
	builder.WriteString(fmt.Sprintf("Total objects including instances per parent instance: %d\n", totalLeaf+totalChildren+1))
	return builder.String()
}
