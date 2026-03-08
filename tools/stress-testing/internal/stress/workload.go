package stress

import (
	"fmt"
	"strings"
	"unicode"

	"github.com/kubernetes-sigs/kro/pkg/testutil/generator"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

const (
	TestLabelKey   = "stress.kro.run/test"
	PrefixLabelKey = "stress.kro.run/prefix"
	TestLabelValue = "true"
)

var RGDGVR = schema.GroupVersionResource{
	Group:    "kro.run",
	Version:  "v1alpha1",
	Resource: "resourcegraphdefinitions",
}

type Complexity struct {
	ConfigMaps      int
	ServiceAccounts int
	Roles           int
	RoleBindings    int
	Deployments     int
	Replicas        int64
}

var DefaultComplexities = map[string]Complexity{
	"low":              {ConfigMaps: 3},
	"medium":           {ConfigMaps: 5, ServiceAccounts: 5, Roles: 5, RoleBindings: 5},
	"high":             {ConfigMaps: 25, ServiceAccounts: 25, Roles: 25, RoleBindings: 25},
	"deployments":      {ConfigMaps: 50, Deployments: 50, Replicas: 0},
	"deployment-heavy": {ConfigMaps: 50, Deployments: 50, Replicas: 0},
}

func RGDName(prefix string, index int) string {
	return fmt.Sprintf("%s%d.kro.run", sanitizePrefix(prefix), index)
}

func InstanceKind(prefix string, rgdIndex int) string {
	return fmt.Sprintf("StressTest%s%d", kindToken(prefix), rgdIndex)
}

func InstanceGVR(prefix string, rgdIndex int) schema.GroupVersionResource {
	kind := InstanceKind(prefix, rgdIndex)
	return schema.GroupVersionResource{
		Group:    "kro.run",
		Version:  "v1alpha1",
		Resource: strings.ToLower(kind) + "s",
	}
}

func LabelSelector(prefix string) string {
	if prefix == "" {
		return fmt.Sprintf("%s=%s", TestLabelKey, TestLabelValue)
	}

	return fmt.Sprintf("%s=%s,%s=%s", TestLabelKey, TestLabelValue, PrefixLabelKey, sanitizePrefix(prefix))
}

func RGDGenerator(prefix string, cfg Complexity) func(index int) *unstructured.Unstructured {
	prefix = sanitizePrefix(prefix)

	return func(index int) *unstructured.Unstructured {
		name := RGDName(prefix, index)
		kind := InstanceKind(prefix, index)

		opts := []generator.ResourceGraphDefinitionOption{
			generator.WithSchema(
				kind,
				"v1alpha1",
				map[string]interface{}{
					"name":      "string",
					"namespace": "string | default=default",
				},
				map[string]interface{}{},
			),
		}

		for i := 0; i < cfg.ConfigMaps; i++ {
			opts = append(opts, generator.WithResource(
				fmt.Sprintf("cm%d", i+1),
				map[string]interface{}{
					"apiVersion": "v1",
					"kind":       "ConfigMap",
					"metadata": map[string]interface{}{
						"name":      fmt.Sprintf("${schema.spec.name}-cm-%d", i+1),
						"namespace": "${schema.spec.namespace}",
					},
					"data": map[string]interface{}{
						"index": fmt.Sprintf("%d", i+1),
						"value": "stress",
					},
				},
				nil,
				nil,
			))
		}

		for i := 0; i < cfg.ServiceAccounts; i++ {
			opts = append(opts, generator.WithResource(
				fmt.Sprintf("sa%d", i+1),
				map[string]interface{}{
					"apiVersion": "v1",
					"kind":       "ServiceAccount",
					"metadata": map[string]interface{}{
						"name":      fmt.Sprintf("${schema.spec.name}-sa-%d", i+1),
						"namespace": "${schema.spec.namespace}",
					},
				},
				nil,
				nil,
			))
		}

		for i := 0; i < cfg.Roles; i++ {
			opts = append(opts, generator.WithResource(
				fmt.Sprintf("role%d", i+1),
				map[string]interface{}{
					"apiVersion": "rbac.authorization.k8s.io/v1",
					"kind":       "Role",
					"metadata": map[string]interface{}{
						"name":      fmt.Sprintf("${schema.spec.name}-role-%d", i+1),
						"namespace": "${schema.spec.namespace}",
					},
					"rules": []interface{}{
						map[string]interface{}{
							"apiGroups": []interface{}{""},
							"resources": []interface{}{"configmaps"},
							"verbs":     []interface{}{"get", "list"},
						},
					},
				},
				nil,
				nil,
			))
		}

		for i := 0; i < cfg.RoleBindings; i++ {
			roleIndex := i%max(cfg.Roles, 1) + 1
			serviceAccountIndex := i%max(cfg.ServiceAccounts, 1) + 1

			opts = append(opts, generator.WithResource(
				fmt.Sprintf("rb%d", i+1),
				map[string]interface{}{
					"apiVersion": "rbac.authorization.k8s.io/v1",
					"kind":       "RoleBinding",
					"metadata": map[string]interface{}{
						"name":      fmt.Sprintf("${schema.spec.name}-rb-%d", i+1),
						"namespace": "${schema.spec.namespace}",
					},
					"roleRef": map[string]interface{}{
						"apiGroup": "rbac.authorization.k8s.io",
						"kind":     "Role",
						"name":     fmt.Sprintf("${role%d.metadata.name}", roleIndex),
					},
					"subjects": []interface{}{
						map[string]interface{}{
							"kind":      "ServiceAccount",
							"name":      fmt.Sprintf("${sa%d.metadata.name}", serviceAccountIndex),
							"namespace": "${schema.spec.namespace}",
						},
					},
				},
				nil,
				nil,
			))
		}

		for i := 0; i < cfg.Deployments; i++ {
			configMapIndex := i%max(cfg.ConfigMaps, 1) + 1
			opts = append(opts, generator.WithResource(
				fmt.Sprintf("deploy%d", i+1),
				map[string]interface{}{
					"apiVersion": "apps/v1",
					"kind":       "Deployment",
					"metadata": map[string]interface{}{
						"name":      fmt.Sprintf("${schema.spec.name}-deploy-%d", i+1),
						"namespace": "${schema.spec.namespace}",
						"labels": map[string]interface{}{
							"app.kubernetes.io/name":     "${schema.spec.name}",
							"app.kubernetes.io/instance": fmt.Sprintf("${schema.spec.name}-deploy-%d", i+1),
						},
					},
					"spec": map[string]interface{}{
						"replicas": cfg.Replicas,
						"selector": map[string]interface{}{
							"matchLabels": map[string]interface{}{
								"app.kubernetes.io/instance": fmt.Sprintf("${schema.spec.name}-deploy-%d", i+1),
							},
						},
						"template": map[string]interface{}{
							"metadata": map[string]interface{}{
								"labels": map[string]interface{}{
									"app.kubernetes.io/instance": fmt.Sprintf("${schema.spec.name}-deploy-%d", i+1),
								},
							},
							"spec": map[string]interface{}{
								"containers": []interface{}{
									map[string]interface{}{
										"name":  "app",
										"image": "registry.k8s.io/pause:3.10",
										"envFrom": []interface{}{
											map[string]interface{}{
												"configMapRef": map[string]interface{}{
													"name": fmt.Sprintf("${cm%d.metadata.name}", configMapIndex),
												},
											},
										},
									},
								},
							},
						},
					},
				},
				nil,
				nil,
			))
		}

		rgd := generator.NewResourceGraphDefinition(name, opts...)
		rgd.SetLabels(map[string]string{
			TestLabelKey:   TestLabelValue,
			PrefixLabelKey: prefix,
		})

		return mustToUnstructured(rgd, "kro.run/v1alpha1", "ResourceGraphDefinition")
	}
}

func InstanceGenerator(prefix string, rgdIndex int, namespace string) func(index int) *unstructured.Unstructured {
	prefix = sanitizePrefix(prefix)

	return func(index int) *unstructured.Unstructured {
		return &unstructured.Unstructured{
			Object: map[string]interface{}{
				"apiVersion": "kro.run/v1alpha1",
				"kind":       InstanceKind(prefix, rgdIndex),
				"metadata": map[string]interface{}{
					"name":      fmt.Sprintf("%s-instance-%d", prefix, index),
					"namespace": namespace,
					"labels": map[string]interface{}{
						TestLabelKey:   TestLabelValue,
						PrefixLabelKey: prefix,
					},
				},
				"spec": map[string]interface{}{
					"name":      fmt.Sprintf("%s-instance-%d", prefix, index),
					"namespace": namespace,
				},
			},
		}
	}
}

func mustToUnstructured(obj metav1.Object, apiVersion string, kind string) *unstructured.Unstructured {
	raw, err := runtime.DefaultUnstructuredConverter.ToUnstructured(obj)
	if err != nil {
		panic(err)
	}

	unstructuredObject := &unstructured.Unstructured{Object: raw}
	unstructuredObject.SetAPIVersion(apiVersion)
	unstructuredObject.SetKind(kind)
	return unstructuredObject
}

func sanitizePrefix(prefix string) string {
	prefix = strings.TrimSpace(strings.ToLower(prefix))
	if prefix == "" {
		return "krostress"
	}
	return prefix
}

func kindToken(prefix string) string {
	prefix = sanitizePrefix(prefix)

	parts := strings.FieldsFunc(prefix, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
	if len(parts) == 0 {
		return "Krostress"
	}

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

	if builder.Len() == 0 {
		return "Krostress"
	}
	return builder.String()
}

func max(left, right int) int {
	if left > right {
		return left
	}
	return right
}
