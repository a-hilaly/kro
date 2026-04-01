package stress

import (
	"fmt"
	"hash/fnv"
	"math/rand"
	"sort"
	"strings"
	"unicode"

	krov1alpha1 "github.com/kubernetes-sigs/kro/api/v1alpha1"
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
	Preset          string
	ConfigMaps      int
	ServiceAccounts int
	Roles           int
	RoleBindings    int
	Deployments     int
	Replicas        int64
	DropMin         int
	DropMax         int
	Seed            int64
}

var DefaultComplexities = map[string]Complexity{
	"low":              {Preset: "low", ConfigMaps: 3},
	"medium":           {Preset: "medium", ConfigMaps: 5, ServiceAccounts: 5, Roles: 5, RoleBindings: 5},
	"high":             {Preset: "high", ConfigMaps: 25, ServiceAccounts: 25, Roles: 25, RoleBindings: 25},
	"deployments":      {Preset: "deployments", ConfigMaps: 50, Deployments: 50, Replicas: 0},
	"deployment-heavy": {Preset: "deployment-heavy", ConfigMaps: 50, Deployments: 50, Replicas: 0},
	"circus":           {Preset: "circus", Replicas: 0, DropMin: 5, DropMax: 50, Seed: 1},
	"feature-mix":      {Preset: "feature-mix", Replicas: 0},
	"mixed":            {Preset: "feature-mix", Replicas: 0},
	"real":             {Preset: "real", Replicas: 0},
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
		if cfg.Preset == "real" {
			return realRGD(prefix, index)
		}
		if cfg.Preset == "circus" {
			return circusRGD(prefix, index, cfg)
		}
		if cfg.Preset == "feature-mix" {
			return featureMixRGD(prefix, index)
		}

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

func realRGD(prefix string, index int) *unstructured.Unstructured {
	name := RGDName(prefix, index)
	kind := InstanceKind(prefix, index)

	opts := []generator.ResourceGraphDefinitionOption{
		generator.WithSchema(
			kind,
			"v1alpha1",
			map[string]interface{}{
				"name":        "string",
				"namespace":   "string | default=default",
				"image":       "string | default=registry.k8s.io/pause:3.10",
				"replicas":    "integer | default=0",
				"port":        "integer | default=8080",
				"metricsPort": "integer | default=9090",
				"features": map[string]interface{}{
					"ingress": "boolean | default=true",
					"batch":   "boolean | default=true",
					"metrics": "boolean | default=true",
					"ack":     "boolean | default=true",
				},
				"config": map[string]interface{}{
					"owner": "string | default=stress",
					"tier":  "string | default=backend",
				},
			},
			map[string]interface{}{
				"deploymentName":     "${deployment.metadata.name}",
				"serviceName":        "${service.metadata.name}",
				"metricsServiceName": "${metricsService.metadata.name}",
				"serviceAccountName": "${serviceAccount.metadata.name}",
				"leaseName":          "${lease.metadata.name}",
				"ingressHost":        "${ingress.spec.rules[0].host}",
				"batch": map[string]interface{}{
					"jobName":     "${preflightJob.metadata.name}",
					"cronJobName": "${maintenanceCron.metadata.name}",
				},
				"ack": map[string]interface{}{
					"bucketName":       "${ackBucket.metadata.name}",
					"bucketGeneration": "${ackBucket.metadata.generation}",
					"vpcName":          "${ackVpc.metadata.name}",
					"vpcGeneration":    "${ackVpc.metadata.generation}",
				},
			},
		),
		generator.WithResource(
			"appConfig",
			map[string]interface{}{
				"apiVersion": "v1",
				"kind":       "ConfigMap",
				"metadata": map[string]interface{}{
					"name":      "${schema.spec.name}-app",
					"namespace": "${schema.spec.namespace}",
					"labels": map[string]interface{}{
						"app.kubernetes.io/name":      "${schema.spec.name}",
						"app.kubernetes.io/component": "${schema.spec.config.tier}",
					},
					"annotations": map[string]interface{}{
						"stress.kro.run/owner": "${schema.spec.config.owner}",
					},
				},
				"data": map[string]interface{}{
					"owner":          "${schema.spec.config.owner}",
					"app":            "${schema.spec.name}",
					"component":      "${schema.spec.config.tier}",
					"serviceAddress": "http://${schema.spec.name}.${schema.spec.namespace}.svc.cluster.local:${schema.spec.port}",
				},
			},
			[]string{"${appConfig.data.owner == schema.spec.config.owner}"},
			nil,
		),
		generator.WithResource(
			"featureConfig",
			map[string]interface{}{
				"apiVersion": "v1",
				"kind":       "ConfigMap",
				"metadata": map[string]interface{}{
					"name":      "${schema.spec.name}-feature",
					"namespace": "${schema.spec.namespace}",
				},
				"data": map[string]interface{}{
					"metrics": "${schema.spec.features.metrics ? \"enabled\" : \"disabled\"}",
					"batch":   "${schema.spec.features.batch ? \"enabled\" : \"disabled\"}",
					"ingress": "${schema.spec.features.ingress ? \"enabled\" : \"disabled\"}",
					"ack":     "${schema.spec.features.ack ? \"enabled\" : \"disabled\"}",
				},
			},
			nil,
			nil,
		),
		generator.WithResource(
			"envConfig",
			map[string]interface{}{
				"apiVersion": "v1",
				"kind":       "ConfigMap",
				"metadata": map[string]interface{}{
					"name":      "${schema.spec.name}-env",
					"namespace": "${schema.spec.namespace}",
				},
				"data": map[string]interface{}{
					"APP_PORT":        "${string(schema.spec.port)}",
					"METRICS_PORT":    "${string(schema.spec.metricsPort)}",
					"METRICS_ADDRESS": "http://${metricsService.metadata.name}:${schema.spec.metricsPort}",
					"ACK_BUCKET":      "${ackBucket.metadata.name}",
					"ACK_VPC":         "${ackVpc.metadata.name}",
				},
			},
			nil,
			nil,
		),
		generator.WithResource(
			"appSecret",
			map[string]interface{}{
				"apiVersion": "v1",
				"kind":       "Secret",
				"metadata": map[string]interface{}{
					"name":      "${schema.spec.name}-secret",
					"namespace": "${schema.spec.namespace}",
					"annotations": map[string]interface{}{
						"stress.kro.run/source-config": "${appConfig.metadata.name}",
					},
				},
				"type": "Opaque",
				"stringData": map[string]interface{}{
					"username": "${schema.spec.name}",
					"password": "${schema.spec.name}-${schema.spec.config.owner}",
					"token":    "${schema.spec.name}-${schema.spec.namespace}-${schema.spec.config.tier}",
				},
			},
			nil,
			nil,
		),
		generator.WithResource(
			"serviceAccount",
			map[string]interface{}{
				"apiVersion": "v1",
				"kind":       "ServiceAccount",
				"metadata": map[string]interface{}{
					"name":      "${schema.spec.name}-sa",
					"namespace": "${schema.spec.namespace}",
					"annotations": map[string]interface{}{
						"stress.kro.run/app-secret": "${appSecret.metadata.name}",
						"stress.kro.run/app-config": "${appConfig.metadata.name}",
					},
				},
			},
			nil,
			nil,
		),
		generator.WithResource(
			"readRole",
			map[string]interface{}{
				"apiVersion": "rbac.authorization.k8s.io/v1",
				"kind":       "Role",
				"metadata": map[string]interface{}{
					"name":      "${schema.spec.name}-read",
					"namespace": "${schema.spec.namespace}",
				},
				"rules": []interface{}{
					map[string]interface{}{
						"apiGroups": []interface{}{""},
						"resources": []interface{}{"configmaps", "secrets"},
						"verbs":     []interface{}{"get", "list", "watch"},
					},
				},
			},
			nil,
			nil,
		),
		generator.WithResource(
			"metricsRole",
			map[string]interface{}{
				"apiVersion": "rbac.authorization.k8s.io/v1",
				"kind":       "Role",
				"metadata": map[string]interface{}{
					"name":      "${schema.spec.name}-metrics",
					"namespace": "${schema.spec.namespace}",
				},
				"rules": []interface{}{
					map[string]interface{}{
						"apiGroups": []interface{}{""},
						"resources": []interface{}{"pods", "services", "endpoints"},
						"verbs":     []interface{}{"get", "list", "watch"},
					},
				},
			},
			nil,
			nil,
		),
		generator.WithResource(
			"readBinding",
			map[string]interface{}{
				"apiVersion": "rbac.authorization.k8s.io/v1",
				"kind":       "RoleBinding",
				"metadata": map[string]interface{}{
					"name":      "${schema.spec.name}-read",
					"namespace": "${schema.spec.namespace}",
				},
				"roleRef": map[string]interface{}{
					"apiGroup": "rbac.authorization.k8s.io",
					"kind":     "Role",
					"name":     "${readRole.metadata.name}",
				},
				"subjects": []interface{}{
					map[string]interface{}{
						"kind":      "ServiceAccount",
						"name":      "${serviceAccount.metadata.name}",
						"namespace": "${schema.spec.namespace}",
					},
				},
			},
			nil,
			nil,
		),
		generator.WithResource(
			"metricsBinding",
			map[string]interface{}{
				"apiVersion": "rbac.authorization.k8s.io/v1",
				"kind":       "RoleBinding",
				"metadata": map[string]interface{}{
					"name":      "${schema.spec.name}-metrics",
					"namespace": "${schema.spec.namespace}",
				},
				"roleRef": map[string]interface{}{
					"apiGroup": "rbac.authorization.k8s.io",
					"kind":     "Role",
					"name":     "${metricsRole.metadata.name}",
				},
				"subjects": []interface{}{
					map[string]interface{}{
						"kind":      "ServiceAccount",
						"name":      "${serviceAccount.metadata.name}",
						"namespace": "${schema.spec.namespace}",
					},
				},
			},
			nil,
			nil,
		),
		generator.WithResource(
			"deployment",
			map[string]interface{}{
				"apiVersion": "apps/v1",
				"kind":       "Deployment",
				"metadata": map[string]interface{}{
					"name":      "${schema.spec.name}",
					"namespace": "${schema.spec.namespace}",
					"labels": map[string]interface{}{
						"app.kubernetes.io/name":      "${schema.spec.name}",
						"app.kubernetes.io/component": "${schema.spec.config.tier}",
						"stress.kro.run/real":         "true",
					},
				},
				"spec": map[string]interface{}{
					"replicas": "${schema.spec.replicas}",
					"selector": map[string]interface{}{
						"matchLabels": map[string]interface{}{
							"app.kubernetes.io/name":      "${schema.spec.name}",
							"app.kubernetes.io/component": "${schema.spec.config.tier}",
						},
					},
					"template": map[string]interface{}{
						"metadata": map[string]interface{}{
							"labels": map[string]interface{}{
								"app.kubernetes.io/name":      "${schema.spec.name}",
								"app.kubernetes.io/component": "${schema.spec.config.tier}",
								"stress.kro.run/real":         "true",
							},
							"annotations": map[string]interface{}{
								"stress.kro.run/config": "${appConfig.metadata.name}",
								"stress.kro.run/ack":    "${ackBucket.metadata.name}:${ackVpc.metadata.name}",
							},
						},
						"spec": map[string]interface{}{
							"serviceAccountName": "${serviceAccount.metadata.name}",
							"containers": []interface{}{
								map[string]interface{}{
									"name":  "app",
									"image": "${schema.spec.image}",
									"ports": []interface{}{
										map[string]interface{}{"name": "http", "containerPort": "${schema.spec.port}"},
										map[string]interface{}{"name": "metrics", "containerPort": "${schema.spec.metricsPort}"},
									},
									"envFrom": []interface{}{
										map[string]interface{}{"configMapRef": map[string]interface{}{"name": "${appConfig.metadata.name}"}},
										map[string]interface{}{"configMapRef": map[string]interface{}{"name": "${featureConfig.metadata.name}"}},
										map[string]interface{}{"configMapRef": map[string]interface{}{"name": "${envConfig.metadata.name}"}},
										map[string]interface{}{"secretRef": map[string]interface{}{"name": "${appSecret.metadata.name}"}},
									},
									"env": []interface{}{
										map[string]interface{}{"name": "SERVICE_NAME", "value": "${service.metadata.name}"},
										map[string]interface{}{"name": "METRICS_SERVICE_NAME", "value": "${metricsService.metadata.name}"},
										map[string]interface{}{"name": "ACK_BUCKET_NAME", "value": "${ackBucket.metadata.name}"},
										map[string]interface{}{"name": "ACK_VPC_NAME", "value": "${ackVpc.metadata.name}"},
									},
								},
							},
						},
					},
				},
			},
			[]string{"${deployment.spec.replicas == 0 || deployment.status.readyReplicas == deployment.spec.replicas}"},
			nil,
		),
		generator.WithResource(
			"service",
			map[string]interface{}{
				"apiVersion": "v1",
				"kind":       "Service",
				"metadata": map[string]interface{}{
					"name":      "${schema.spec.name}",
					"namespace": "${schema.spec.namespace}",
				},
				"spec": map[string]interface{}{
					"selector": map[string]interface{}{
						"app.kubernetes.io/name":      "${schema.spec.name}",
						"app.kubernetes.io/component": "${schema.spec.config.tier}",
					},
					"ports": []interface{}{
						map[string]interface{}{
							"name":       "http",
							"port":       80,
							"targetPort": "${schema.spec.port}",
						},
					},
				},
			},
			nil,
			nil,
		),
		generator.WithResource(
			"metricsService",
			map[string]interface{}{
				"apiVersion": "v1",
				"kind":       "Service",
				"metadata": map[string]interface{}{
					"name":      "${schema.spec.name}-metrics",
					"namespace": "${schema.spec.namespace}",
				},
				"spec": map[string]interface{}{
					"selector": map[string]interface{}{
						"app.kubernetes.io/name":      "${schema.spec.name}",
						"app.kubernetes.io/component": "${schema.spec.config.tier}",
					},
					"ports": []interface{}{
						map[string]interface{}{
							"name":       "metrics",
							"port":       "${schema.spec.metricsPort}",
							"targetPort": "${schema.spec.metricsPort}",
						},
					},
				},
			},
			nil,
			[]string{"${schema.spec.features.metrics}"},
		),
		generator.WithResource(
			"networkPolicy",
			map[string]interface{}{
				"apiVersion": "networking.k8s.io/v1",
				"kind":       "NetworkPolicy",
				"metadata": map[string]interface{}{
					"name":      "${schema.spec.name}",
					"namespace": "${schema.spec.namespace}",
				},
				"spec": map[string]interface{}{
					"podSelector": map[string]interface{}{
						"matchLabels": map[string]interface{}{
							"app.kubernetes.io/name":      "${schema.spec.name}",
							"app.kubernetes.io/component": "${schema.spec.config.tier}",
						},
					},
					"policyTypes": []interface{}{"Ingress", "Egress"},
					"ingress": []interface{}{
						map[string]interface{}{
							"ports": []interface{}{
								map[string]interface{}{"protocol": "TCP", "port": "${schema.spec.port}"},
								map[string]interface{}{"protocol": "TCP", "port": "${schema.spec.metricsPort}"},
							},
						},
					},
					"egress": []interface{}{
						map[string]interface{}{},
					},
				},
			},
			nil,
			nil,
		),
		generator.WithResource(
			"podDisruptionBudget",
			map[string]interface{}{
				"apiVersion": "policy/v1",
				"kind":       "PodDisruptionBudget",
				"metadata": map[string]interface{}{
					"name":      "${schema.spec.name}",
					"namespace": "${schema.spec.namespace}",
				},
				"spec": map[string]interface{}{
					"minAvailable": 0,
					"selector": map[string]interface{}{
						"matchLabels": map[string]interface{}{
							"app.kubernetes.io/name":      "${schema.spec.name}",
							"app.kubernetes.io/component": "${schema.spec.config.tier}",
						},
					},
				},
			},
			nil,
			nil,
		),
		generator.WithResource(
			"lease",
			map[string]interface{}{
				"apiVersion": "coordination.k8s.io/v1",
				"kind":       "Lease",
				"metadata": map[string]interface{}{
					"name":      "${schema.spec.name}",
					"namespace": "${schema.spec.namespace}",
				},
				"spec": map[string]interface{}{
					"holderIdentity":       "${deployment.metadata.name}",
					"leaseDurationSeconds": 30,
				},
			},
			nil,
			nil,
		),
		generator.WithResource(
			"preflightJob",
			map[string]interface{}{
				"apiVersion": "batch/v1",
				"kind":       "Job",
				"metadata": map[string]interface{}{
					"name":      "${schema.spec.name}-preflight",
					"namespace": "${schema.spec.namespace}",
				},
				"spec": map[string]interface{}{
					"suspend": true,
					"template": map[string]interface{}{
						"metadata": map[string]interface{}{
							"labels": map[string]interface{}{
								"app.kubernetes.io/name": "${schema.spec.name}",
								"job.kubernetes.io/type": "preflight",
							},
						},
						"spec": map[string]interface{}{
							"restartPolicy":      "Never",
							"serviceAccountName": "${serviceAccount.metadata.name}",
							"containers": []interface{}{
								map[string]interface{}{
									"name":  "preflight",
									"image": "${schema.spec.image}",
									"envFrom": []interface{}{
										map[string]interface{}{"configMapRef": map[string]interface{}{"name": "${appConfig.metadata.name}"}},
										map[string]interface{}{"secretRef": map[string]interface{}{"name": "${appSecret.metadata.name}"}},
									},
								},
							},
						},
					},
				},
			},
			nil,
			[]string{"${schema.spec.features.batch}"},
		),
		generator.WithResource(
			"maintenanceCron",
			map[string]interface{}{
				"apiVersion": "batch/v1",
				"kind":       "CronJob",
				"metadata": map[string]interface{}{
					"name":      "${schema.spec.name}-maintenance",
					"namespace": "${schema.spec.namespace}",
				},
				"spec": map[string]interface{}{
					"schedule": "*/30 * * * *",
					"suspend":  true,
					"jobTemplate": map[string]interface{}{
						"spec": map[string]interface{}{
							"template": map[string]interface{}{
								"spec": map[string]interface{}{
									"restartPolicy":      "Never",
									"serviceAccountName": "${serviceAccount.metadata.name}",
									"containers": []interface{}{
										map[string]interface{}{
											"name":  "maintenance",
											"image": "${schema.spec.image}",
											"env": []interface{}{
												map[string]interface{}{"name": "ROLE_NAME", "value": "${readRole.metadata.name}"},
												map[string]interface{}{"name": "SERVICE_NAME", "value": "${service.metadata.name}"},
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
			[]string{"${schema.spec.features.batch}"},
		),
		generator.WithResource(
			"ingress",
			map[string]interface{}{
				"apiVersion": "networking.k8s.io/v1",
				"kind":       "Ingress",
				"metadata": map[string]interface{}{
					"name":      "${schema.spec.name}",
					"namespace": "${schema.spec.namespace}",
				},
				"spec": map[string]interface{}{
					"rules": []interface{}{
						map[string]interface{}{
							"host": "${schema.spec.name}.${schema.spec.namespace}.stress.kro.run",
							"http": map[string]interface{}{
								"paths": []interface{}{
									map[string]interface{}{
										"path":     "/",
										"pathType": "Prefix",
										"backend": map[string]interface{}{
											"service": map[string]interface{}{
												"name": "${service.metadata.name}",
												"port": map[string]interface{}{"number": 80},
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
			[]string{"${schema.spec.features.ingress}"},
		),
		generator.WithResource(
			"ackBucket",
			map[string]interface{}{
				"apiVersion": "s3.services.k8s.aws/v1alpha1",
				"kind":       "Bucket",
				"metadata": map[string]interface{}{
					"name":      "${schema.spec.name}-s3",
					"namespace": "${schema.spec.namespace}",
				},
				"spec": map[string]interface{}{
					"name": "${schema.spec.name}-s3",
				},
			},
			[]string{"${ackBucket.metadata.generation > 0}"},
			[]string{"${schema.spec.features.ack}"},
		),
		generator.WithResource(
			"ackVpc",
			map[string]interface{}{
				"apiVersion": "ec2.services.k8s.aws/v1alpha1",
				"kind":       "VPC",
				"metadata": map[string]interface{}{
					"name":      "${schema.spec.name}-vpc",
					"namespace": "${schema.spec.namespace}",
				},
				"spec": map[string]interface{}{
					"cidrBlocks":       []interface{}{"10.0.0.0/16"},
					"enableDNSSupport": true,
				},
			},
			[]string{"${ackVpc.metadata.generation > 0}"},
			[]string{"${schema.spec.features.ack}"},
		),
	}

	rgd := generator.NewResourceGraphDefinition(name, opts...)
	rgd.SetLabels(map[string]string{
		TestLabelKey:   TestLabelValue,
		PrefixLabelKey: prefix,
	})

	return mustToUnstructured(rgd, "kro.run/v1alpha1", "ResourceGraphDefinition")
}

func featureMixRGD(prefix string, index int) *unstructured.Unstructured {
	name := RGDName(prefix, index)
	kind := InstanceKind(prefix, index)
	statusSchema := featureMixStatusSchema()

	opts := []generator.ResourceGraphDefinitionOption{
		generator.WithSchema(
			kind,
			"v1alpha1",
			map[string]interface{}{
				"name":      "string",
				"namespace": "string | default=default",
				"replicas":  "integer | default=0",
				"team":      "string | default=platform",
				"env":       "string | default=dev",
				"regions":   `[]string | default=["us-east-1","us-west-2","eu-west-1"]`,
				"tiers":     `[]string | default=["api","worker"]`,
				"shards":    `[]string | default=["a","b"]`,
				"features": map[string]interface{}{
					"metrics": "boolean | default=true",
					"batch":   "boolean | default=true",
					"public":  "boolean | default=true",
					"ack":     "boolean | default=true",
				},
			},
			statusSchema,
		),
	}

	for i := 0; i < 10; i++ {
		id := fmt.Sprintf("cm%d", i+1)
		opts = append(opts, generator.WithResource(
			id,
			map[string]interface{}{
				"apiVersion": "v1",
				"kind":       "ConfigMap",
				"metadata": map[string]interface{}{
					"name":      fmt.Sprintf("${schema.spec.name}-cm-%d", i+1),
					"namespace": "${schema.spec.namespace}",
					"labels": map[string]interface{}{
						"stress.kro.run/component": fmt.Sprintf("config-%d", i+1),
						"stress.kro.run/team":      "${schema.spec.team}",
						"stress.kro.run/env":       "${schema.spec.env}",
					},
				},
				"data": map[string]interface{}{
					"index":         fmt.Sprintf("%d", i+1),
					"team":          "${schema.spec.team}",
					"environment":   "${schema.spec.env}",
					"rootConfigRef": "${extConfig1.metadata.name}",
				},
			},
			nil,
			nil,
		))
	}

	for i := 0; i < 10; i++ {
		id := fmt.Sprintf("secret%d", i+1)
		cmIndex := i + 1
		includeWhen := []string(nil)
		if i%2 == 0 {
			includeWhen = []string{"${schema.spec.features.metrics}"}
		}
		opts = append(opts, generator.WithResource(
			id,
			map[string]interface{}{
				"apiVersion": "v1",
				"kind":       "Secret",
				"metadata": map[string]interface{}{
					"name":      fmt.Sprintf("${schema.spec.name}-secret-%d", i+1),
					"namespace": "${schema.spec.namespace}",
					"annotations": map[string]interface{}{
						"stress.kro.run/source-config": fmt.Sprintf("${cm%d.metadata.name}", cmIndex),
					},
				},
				"type": "Opaque",
				"stringData": map[string]interface{}{
					"sourceConfig": fmt.Sprintf("${cm%d.metadata.name}", cmIndex),
					"bundleRef":    "${extConfig1.metadata.name}",
				},
			},
			nil,
			includeWhen,
		))
	}

	for i := 0; i < 10; i++ {
		id := fmt.Sprintf("sa%d", i+1)
		cmIndex := i + 1
		opts = append(opts, generator.WithResource(
			id,
			map[string]interface{}{
				"apiVersion": "v1",
				"kind":       "ServiceAccount",
				"metadata": map[string]interface{}{
					"name":      fmt.Sprintf("${schema.spec.name}-sa-%d", i+1),
					"namespace": "${schema.spec.namespace}",
					"annotations": map[string]interface{}{
						"stress.kro.run/config":  fmt.Sprintf("${cm%d.metadata.name}", cmIndex),
						"stress.kro.run/default": "${extServiceAccount1.metadata.name}",
					},
				},
			},
			nil,
			nil,
		))
	}

	for i := 0; i < 10; i++ {
		id := fmt.Sprintf("role%d", i+1)
		opts = append(opts, generator.WithResource(
			id,
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
						"resources": []interface{}{"configmaps", "secrets", "serviceaccounts"},
						"verbs":     []interface{}{"get", "list", "watch"},
					},
				},
			},
			nil,
			nil,
		))
	}

	for i := 0; i < 10; i++ {
		id := fmt.Sprintf("rb%d", i+1)
		roleIndex := i + 1
		saIndex := i + 1
		opts = append(opts, generator.WithResource(
			id,
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
						"name":      fmt.Sprintf("${sa%d.metadata.name}", saIndex),
						"namespace": "${schema.spec.namespace}",
					},
				},
			},
			nil,
			nil,
		))
	}

	for i := 0; i < 10; i++ {
		id := fmt.Sprintf("deploy%d", i+1)
		cmIndex := i + 1
		secretIndex := i + 1
		saIndex := i + 1
		includeWhen := []string{"${schema.spec.features.public}"}
		if i%2 == 1 {
			includeWhen = []string{"${schema.spec.features.metrics}"}
		}
		opts = append(opts, generator.WithResource(
			id,
			map[string]interface{}{
				"apiVersion": "apps/v1",
				"kind":       "Deployment",
				"metadata": map[string]interface{}{
					"name":      fmt.Sprintf("${schema.spec.name}-deploy-%d", i+1),
					"namespace": "${schema.spec.namespace}",
					"labels": map[string]interface{}{
						"app.kubernetes.io/name":      "${schema.spec.name}",
						"app.kubernetes.io/component": fmt.Sprintf("deploy-%d", i+1),
						"app.kubernetes.io/instance":  fmt.Sprintf("${schema.spec.name}-deploy-%d", i+1),
					},
				},
				"spec": map[string]interface{}{
					"replicas": "${schema.spec.replicas}",
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
							"serviceAccountName": fmt.Sprintf("${sa%d.metadata.name}", saIndex),
							"containers": []interface{}{
								map[string]interface{}{
									"name":  "app",
									"image": "registry.k8s.io/pause:3.10",
									"envFrom": []interface{}{
										map[string]interface{}{"configMapRef": map[string]interface{}{"name": fmt.Sprintf("${cm%d.metadata.name}", cmIndex)}},
										map[string]interface{}{"secretRef": map[string]interface{}{"name": fmt.Sprintf("${secret%d.metadata.name}", secretIndex)}},
									},
									"env": []interface{}{
										map[string]interface{}{"name": "ROOT_BUNDLE_CONFIG", "value": "${extConfig1.metadata.name}"},
										map[string]interface{}{"name": "DEFAULT_SERVICE_ACCOUNT", "value": "${extServiceAccount1.metadata.name}"},
									},
								},
							},
						},
					},
				},
			},
			[]string{fmt.Sprintf("${%s.spec.replicas == 0 || %s.status.readyReplicas == %s.spec.replicas}", id, id, id)},
			includeWhen,
		))
	}

	for i := 0; i < 5; i++ {
		id := fmt.Sprintf("svc%d", i+1)
		deployIndex := i + 1
		opts = append(opts, generator.WithResource(
			id,
			map[string]interface{}{
				"apiVersion": "v1",
				"kind":       "Service",
				"metadata": map[string]interface{}{
					"name":      fmt.Sprintf("${schema.spec.name}-svc-%d", i+1),
					"namespace": "${schema.spec.namespace}",
				},
				"spec": map[string]interface{}{
					"selector": map[string]interface{}{
						"app.kubernetes.io/instance": fmt.Sprintf("${deploy%d.metadata.labels[\"app.kubernetes.io/instance\"]}", deployIndex),
					},
					"ports": []interface{}{
						map[string]interface{}{"name": "http", "port": 80, "targetPort": 8080},
					},
				},
			},
			nil,
			[]string{"${schema.spec.features.public}"},
		))
	}

	opts = append(opts, generator.WithResource(
		"ackBucket",
		map[string]interface{}{
			"apiVersion": "s3.services.k8s.aws/v1alpha1",
			"kind":       "Bucket",
			"metadata": map[string]interface{}{
				"name":      "${schema.spec.name}-ack-root",
				"namespace": "${schema.spec.namespace}",
			},
			"spec": map[string]interface{}{
				"name": "${schema.spec.name}-ack-root",
			},
		},
		nil,
		[]string{"${schema.spec.features.ack}"},
	))

	opts = append(opts, generator.WithResource(
		"ackVpc",
		map[string]interface{}{
			"apiVersion": "ec2.services.k8s.aws/v1alpha1",
			"kind":       "VPC",
			"metadata": map[string]interface{}{
				"name":      "${schema.spec.name}-ack-vpc",
				"namespace": "${schema.spec.namespace}",
			},
			"spec": map[string]interface{}{
				"cidrBlocks":         []interface{}{"10.80.0.0/16"},
				"enableDNSSupport":   true,
				"enableDNSHostnames": true,
			},
		},
		nil,
		[]string{"${schema.spec.features.ack}"},
	))

	for i := 0; i < 5; i++ {
		id := fmt.Sprintf("job%d", i+1)
		saIndex := i + 1
		cmIndex := i + 1
		opts = append(opts, generator.WithResource(
			id,
			map[string]interface{}{
				"apiVersion": "batch/v1",
				"kind":       "Job",
				"metadata": map[string]interface{}{
					"name":      fmt.Sprintf("${schema.spec.name}-job-%d", i+1),
					"namespace": "${schema.spec.namespace}",
				},
				"spec": map[string]interface{}{
					"suspend": true,
					"template": map[string]interface{}{
						"spec": map[string]interface{}{
							"restartPolicy":      "Never",
							"serviceAccountName": fmt.Sprintf("${sa%d.metadata.name}", saIndex),
							"containers": []interface{}{
								map[string]interface{}{
									"name":  "worker",
									"image": "registry.k8s.io/pause:3.10",
									"env": []interface{}{
										map[string]interface{}{"name": "CONFIG_NAME", "value": fmt.Sprintf("${cm%d.metadata.name}", cmIndex)},
									},
								},
							},
						},
					},
				},
			},
			[]string{fmt.Sprintf("${%s.spec.suspend == true || %s.status.?completionTime.orValue(null) != null}", id, id)},
			[]string{"${schema.spec.features.batch}"},
		))
	}

	opts = append(opts, generator.WithResourceCollection(
		"ackRegionalBuckets1",
		map[string]interface{}{
			"apiVersion": "s3.services.k8s.aws/v1alpha1",
			"kind":       "Bucket",
			"metadata": map[string]interface{}{
				"name":      "${schema.spec.name}-${region}-ack-bucket",
				"namespace": "${schema.spec.namespace}",
			},
			"spec": map[string]interface{}{
				"name": "${schema.spec.name}-${region}-ack-bucket",
			},
		},
		[]krov1alpha1.ForEachDimension{
			{"region": "${schema.spec.regions}"},
		},
		nil,
		[]string{"${schema.spec.features.ack}"},
	))

	opts = append(opts, generator.WithResourceCollection(
		"ackSubnets1",
		map[string]interface{}{
			"apiVersion": "ec2.services.k8s.aws/v1alpha1",
			"kind":       "Subnet",
			"metadata": map[string]interface{}{
				"name":      "${schema.spec.name}-ack-subnet-${region}",
				"namespace": "${schema.spec.namespace}",
			},
			"spec": map[string]interface{}{
				"cidrBlock": `${region == "us-east-1" ? "10.80.1.0/24" : region == "us-west-2" ? "10.80.2.0/24" : "10.80.3.0/24"}`,
				"vpcID":     "${ackVpc.status.vpcID}",
			},
		},
		[]krov1alpha1.ForEachDimension{
			{"region": "${schema.spec.regions}"},
		},
		nil,
		[]string{"${schema.spec.features.ack}"},
	))

	opts = append(opts, generator.WithResourceCollection(
		"ackSecurityGroups1",
		map[string]interface{}{
			"apiVersion": "ec2.services.k8s.aws/v1alpha1",
			"kind":       "SecurityGroup",
			"metadata": map[string]interface{}{
				"name":      "${schema.spec.name}-ack-sg-${subnet.metadata.name}",
				"namespace": "${schema.spec.namespace}",
			},
			"spec": map[string]interface{}{
				"description": "${subnet.status.subnetID}",
				"vpcID":       "${ackVpc.status.vpcID}",
			},
		},
		[]krov1alpha1.ForEachDimension{
			{"subnet": "${ackSubnets1}"},
		},
		nil,
		[]string{"${schema.spec.features.ack}"},
	))

	for i := 0; i < 5; i++ {
		id := fmt.Sprintf("regionConfigs%d", i+1)
		cmIndex := i + 1
		opts = append(opts, generator.WithResourceCollection(
			id,
			map[string]interface{}{
				"apiVersion": "v1",
				"kind":       "ConfigMap",
				"metadata": map[string]interface{}{
					"name":      fmt.Sprintf("${schema.spec.name}-region-%d-${region}", i+1),
					"namespace": "${schema.spec.namespace}",
				},
				"data": map[string]interface{}{
					"region": "${region}",
					"source": fmt.Sprintf("${cm%d.metadata.name}", cmIndex),
				},
			},
			[]krov1alpha1.ForEachDimension{
				{"region": "${schema.spec.regions}"},
			},
			nil,
			[]string{"${schema.spec.features.metrics}"},
		))
	}

	for i := 0; i < 5; i++ {
		id := fmt.Sprintf("matrixSecrets%d", i+1)
		roleIndex := i + 1
		opts = append(opts, generator.WithResourceCollection(
			id,
			map[string]interface{}{
				"apiVersion": "v1",
				"kind":       "Secret",
				"metadata": map[string]interface{}{
					"name":      fmt.Sprintf("${schema.spec.name}-matrix-%d-${tier}-${shard}", i+1),
					"namespace": "${schema.spec.namespace}",
				},
				"type": "Opaque",
				"stringData": map[string]interface{}{
					"tier":  "${tier}",
					"shard": "${shard}",
					"role":  fmt.Sprintf("${role%d.metadata.name}", roleIndex),
				},
			},
			[]krov1alpha1.ForEachDimension{
				{"tier": "${schema.spec.tiers}"},
				{"shard": "${schema.spec.shards}"},
			},
			nil,
			[]string{"${schema.spec.features.metrics}"},
		))
	}

	for i := 0; i < 3; i++ {
		opts = append(opts, generator.WithExternalRef(
			fmt.Sprintf("extConfig%d", i+1),
			&krov1alpha1.ExternalRef{
				APIVersion: "v1",
				Kind:       "ConfigMap",
				Metadata: krov1alpha1.ExternalRefMetadata{
					Name: "kube-root-ca.crt",
				},
			},
			nil,
			nil,
		))
	}

	for i := 0; i < 2; i++ {
		opts = append(opts, generator.WithExternalRef(
			fmt.Sprintf("extServiceAccount%d", i+1),
			&krov1alpha1.ExternalRef{
				APIVersion: "v1",
				Kind:       "ServiceAccount",
				Metadata: krov1alpha1.ExternalRefMetadata{
					Name: "default",
				},
			},
			nil,
			nil,
		))
	}

	for i := 0; i < 5; i++ {
		opts = append(opts, generator.WithExternalRef(
			fmt.Sprintf("extConfigs%d", i+1),
			&krov1alpha1.ExternalRef{
				APIVersion: "v1",
				Kind:       "ConfigMap",
				Metadata: krov1alpha1.ExternalRefMetadata{
					Name: "kube-root-ca.crt",
				},
			},
			nil,
			[]string{"${schema.spec.features.metrics}"},
		))
	}

	for i := 0; i < 5; i++ {
		opts = append(opts, generator.WithExternalRef(
			fmt.Sprintf("extServiceAccounts%d", i+1),
			&krov1alpha1.ExternalRef{
				APIVersion: "v1",
				Kind:       "ServiceAccount",
				Metadata: krov1alpha1.ExternalRefMetadata{
					Name: "default",
				},
			},
			nil,
			[]string{"${schema.spec.features.public}"},
		))
	}

	rgd := generator.NewResourceGraphDefinition(name, opts...)
	rgd.SetLabels(map[string]string{
		TestLabelKey:   TestLabelValue,
		PrefixLabelKey: prefix,
	})

	return mustToUnstructured(rgd, "kro.run/v1alpha1", "ResourceGraphDefinition")
}

func circusRGD(prefix string, index int, cfg Complexity) *unstructured.Unstructured {
	rgd := featureMixRGD(prefix, index)

	resources, found, err := unstructured.NestedSlice(rgd.Object, "spec", "resources")
	if err != nil || !found {
		panic(fmt.Sprintf("feature-mix resources missing: found=%v err=%v", found, err))
	}

	resourceIndexes := make(map[string]int, len(resources))
	resourceIDs := make([]string, 0, len(resources))
	for idx, resource := range resources {
		resourceMap, ok := resource.(map[string]interface{})
		if !ok {
			continue
		}
		id, _ := resourceMap["id"].(string)
		if id == "" {
			continue
		}
		resourceIndexes[id] = idx
		resourceIDs = append(resourceIDs, id)
	}

	dependencies := circusResourceDependencies(resources, resourceIDs)
	lockedIDs := circusDependencyClosure(circusMandatoryIDs, dependencies)
	reverseDependencies := circusReverseDependencies(dependencies)

	optionalIDs := make([]string, 0, len(resourceIDs))
	for _, id := range resourceIDs {
		if lockedIDs[id] {
			continue
		}
		optionalIDs = append(optionalIDs, id)
	}

	dropMin := cfg.DropMin
	dropMax := cfg.DropMax
	if dropMin < 0 {
		dropMin = 0
	}
	if dropMax < dropMin {
		dropMax = dropMin
	}
	if dropMax > len(optionalIDs) {
		dropMax = len(optionalIDs)
	}

	rng := rand.New(rand.NewSource(circusSeed(index, cfg.Seed)))
	dropCount := dropMin
	if dropMax > dropMin {
		dropCount += rng.Intn(dropMax - dropMin + 1)
	}
	rng.Shuffle(len(optionalIDs), func(i, j int) {
		optionalIDs[i], optionalIDs[j] = optionalIDs[j], optionalIDs[i]
	})

	droppedIDs := make(map[string]struct{}, dropCount)
	for _, id := range optionalIDs {
		candidateClosure := circusDependentClosure(id, reverseDependencies)
		if len(candidateClosure) == 0 {
			continue
		}
		nextCount := len(droppedIDs)
		for candidate := range candidateClosure {
			if _, exists := droppedIDs[candidate]; !exists {
				nextCount++
			}
		}
		if nextCount > dropCount {
			continue
		}
		for candidate := range candidateClosure {
			droppedIDs[candidate] = struct{}{}
		}
		if len(droppedIDs) == dropCount {
			break
		}
	}

	if len(droppedIDs) < dropMin {
		for _, id := range optionalIDs {
			if _, exists := droppedIDs[id]; exists {
				continue
			}
			candidateClosure := circusDependentClosure(id, reverseDependencies)
			if len(candidateClosure) == 0 {
				continue
			}
			nextCount := len(droppedIDs)
			for candidate := range candidateClosure {
				if _, exists := droppedIDs[candidate]; !exists {
					nextCount++
				}
			}
			if nextCount > dropMax {
				continue
			}
			for candidate := range candidateClosure {
				droppedIDs[candidate] = struct{}{}
			}
			if len(droppedIDs) >= dropMin {
				break
			}
		}
	}

	droppedIndexes := make(map[int]struct{}, len(droppedIDs))
	for id := range droppedIDs {
		if idx, ok := resourceIndexes[id]; ok {
			droppedIndexes[idx] = struct{}{}
		}
	}

	filtered := make([]interface{}, 0, len(resources)-len(droppedIndexes))
	for idx, resource := range resources {
		if _, skip := droppedIndexes[idx]; skip {
			continue
		}
		filtered = append(filtered, resource)
	}

	if err := unstructured.SetNestedSlice(rgd.Object, filtered, "spec", "resources"); err != nil {
		panic(fmt.Sprintf("set circus resources: %v", err))
	}

	statusSchema, found, err := unstructured.NestedMap(rgd.Object, "spec", "schema", "status")
	if err != nil {
		panic(fmt.Sprintf("get circus status schema: %v", err))
	}
	if found {
		droppedResourceIDs := make([]string, 0, len(droppedIDs))
		for id := range droppedIDs {
			droppedResourceIDs = append(droppedResourceIDs, id)
		}
		sort.Strings(droppedResourceIDs)

		prunedStatus, keep := circusPruneStatusSchema(statusSchema, droppedResourceIDs)
		if !keep {
			prunedStatus = map[string]interface{}{}
		}
		prunedStatusMap, ok := prunedStatus.(map[string]interface{})
		if !ok {
			panic(fmt.Sprintf("circus status schema has unexpected type %T", prunedStatus))
		}
		if err := unstructured.SetNestedMap(rgd.Object, prunedStatusMap, "spec", "schema", "status"); err != nil {
			panic(fmt.Sprintf("set circus status schema: %v", err))
		}
	}

	annotations := rgd.GetAnnotations()
	if annotations == nil {
		annotations = map[string]string{}
	}
	annotations["stress.kro.run/circus-drop-count"] = fmt.Sprintf("%d", len(droppedIndexes))
	annotations["stress.kro.run/circus-seed"] = fmt.Sprintf("%d", cfg.Seed)
	rgd.SetAnnotations(annotations)

	return rgd
}

var circusMandatoryIDs = map[string]bool{
	"cm1":                true,
	"secret1":            true,
	"sa1":                true,
	"role1":              true,
	"rb1":                true,
	"deploy1":            true,
	"svc1":               true,
	"job1":               true,
	"ackSecurityGroups1": true,
	"regionConfigs1":     true,
	"matrixSecrets1":     true,
	"extConfig1":         true,
	"extServiceAccount1": true,
}

func circusSeed(index int, seed int64) int64 {
	h := fnv.New64a()
	_, _ = h.Write([]byte(fmt.Sprintf("%d:%d", index, seed)))
	return int64(h.Sum64())
}

func circusResourceDependencies(resources []interface{}, resourceIDs []string) map[string]map[string]bool {
	dependencies := make(map[string]map[string]bool, len(resources))
	for _, resource := range resources {
		resourceMap, ok := resource.(map[string]interface{})
		if !ok {
			continue
		}
		id, _ := resourceMap["id"].(string)
		if id == "" {
			continue
		}
		refs := make(map[string]bool)
		circusCollectReferences(resourceMap, resourceIDs, refs)
		delete(refs, id)
		dependencies[id] = refs
	}
	return dependencies
}

func circusCollectReferences(value interface{}, resourceIDs []string, refs map[string]bool) {
	switch typed := value.(type) {
	case string:
		for _, id := range resourceIDs {
			if circusContainsIdentifier(typed, id) {
				refs[id] = true
			}
		}
	case []interface{}:
		for _, item := range typed {
			circusCollectReferences(item, resourceIDs, refs)
		}
	case map[string]interface{}:
		for _, item := range typed {
			circusCollectReferences(item, resourceIDs, refs)
		}
	}
}

func circusContainsIdentifier(expression string, id string) bool {
	searchFrom := 0
	for {
		offset := strings.Index(expression[searchFrom:], id)
		if offset < 0 {
			return false
		}
		start := searchFrom + offset
		end := start + len(id)
		beforeOK := start == 0 || !isIdentifierRune(rune(expression[start-1]))
		afterOK := end == len(expression) || !isIdentifierRune(rune(expression[end]))
		if beforeOK && afterOK {
			return true
		}
		searchFrom = start + 1
	}
}

func isIdentifierRune(r rune) bool {
	return unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_'
}

func circusDependencyClosure(seed map[string]bool, dependencies map[string]map[string]bool) map[string]bool {
	closure := make(map[string]bool, len(seed))
	stack := make([]string, 0, len(seed))
	for id := range seed {
		stack = append(stack, id)
	}
	for len(stack) > 0 {
		last := len(stack) - 1
		id := stack[last]
		stack = stack[:last]
		if closure[id] {
			continue
		}
		closure[id] = true
		for dep := range dependencies[id] {
			if !closure[dep] {
				stack = append(stack, dep)
			}
		}
	}
	return closure
}

func circusReverseDependencies(dependencies map[string]map[string]bool) map[string]map[string]bool {
	reverse := make(map[string]map[string]bool, len(dependencies))
	for id := range dependencies {
		if reverse[id] == nil {
			reverse[id] = map[string]bool{}
		}
		for dep := range dependencies[id] {
			if reverse[dep] == nil {
				reverse[dep] = map[string]bool{}
			}
			reverse[dep][id] = true
		}
	}
	return reverse
}

func circusDependentClosure(id string, reverse map[string]map[string]bool) map[string]bool {
	closure := map[string]bool{}
	stack := []string{id}
	for len(stack) > 0 {
		last := len(stack) - 1
		current := stack[last]
		stack = stack[:last]
		if closure[current] {
			continue
		}
		closure[current] = true
		for dependent := range reverse[current] {
			if !closure[dependent] {
				stack = append(stack, dependent)
			}
		}
	}
	return closure
}

func featureMixStatusSchema() map[string]interface{} {
	statusSchema := map[string]interface{}{
		"resources": map[string]interface{}{
			"deploymentName": "${deploy1.metadata.name}",
			"serviceName":    "${svc1.metadata.name}",
			"ackVpcName":     "${ackVpc.metadata.name}",
			"jobName":        "${job1.metadata.name}",
		},
		"counts": map[string]interface{}{
			"regionConfigCount":      "${string(regionConfigs1.size())}",
			"matrixSecretCount":      "${string(matrixSecrets1.size())}",
			"ackRegionalBucketCount": "${string(ackRegionalBuckets1.size())}",
			"ackSubnetCount":         "${string(ackSubnets1.size())}",
			"ackSecurityGroupCount":  "${string(ackSecurityGroups1.size())}",
		},
		"workload": map[string]interface{}{
			"readyReplicas": "${string(deploy1.status.readyReplicas)}",
			"jobPhase":      `${job1.status.?completionTime.orValue(null) != null ? "complete" : "pending"}`,
		},
		"ack": map[string]interface{}{
			"bucketName":          "${ackBucket.metadata.name}",
			"vpcID":               "${ackVpc.status.vpcID}",
			"regionalBucketCount": "${string(ackRegionalBuckets1.size())}",
			"subnetCount":         "${string(ackSubnets1.size())}",
			"securityGroupCount":  "${string(ackSecurityGroups1.size())}",
		},
		"externals": map[string]interface{}{
			"rootConfigName":     "${extConfig1.metadata.name}",
			"defaultAccountName": "${extServiceAccount1.metadata.name}",
		},
		"configs":      map[string]interface{}{},
		"secrets":      map[string]interface{}{},
		"identities":   map[string]interface{}{},
		"rbac":         map[string]interface{}{},
		"deployments":  map[string]interface{}{},
		"services":     map[string]interface{}{},
		"jobs":         map[string]interface{}{},
		"collections":  map[string]interface{}{},
		"externalRefs": map[string]interface{}{},
	}

	configs := statusSchema["configs"].(map[string]interface{})
	secrets := statusSchema["secrets"].(map[string]interface{})
	identities := statusSchema["identities"].(map[string]interface{})
	rbac := statusSchema["rbac"].(map[string]interface{})
	deployments := statusSchema["deployments"].(map[string]interface{})
	services := statusSchema["services"].(map[string]interface{})
	jobs := statusSchema["jobs"].(map[string]interface{})
	collections := statusSchema["collections"].(map[string]interface{})
	externalRefs := statusSchema["externalRefs"].(map[string]interface{})

	for i := 0; i < 10; i++ {
		n := i + 1
		configs[fmt.Sprintf("config%dName", n)] = fmt.Sprintf("${cm%d.metadata.name}", n)
		secrets[fmt.Sprintf("secret%dName", n)] = fmt.Sprintf("${secret%d.metadata.name}", n)
		identities[fmt.Sprintf("serviceAccount%dName", n)] = fmt.Sprintf("${sa%d.metadata.name}", n)
		rbac[fmt.Sprintf("role%dName", n)] = fmt.Sprintf("${role%d.metadata.name}", n)
		rbac[fmt.Sprintf("roleBinding%dName", n)] = fmt.Sprintf("${rb%d.metadata.name}", n)
		deployments[fmt.Sprintf("deployment%dName", n)] = fmt.Sprintf("${deploy%d.metadata.name}", n)
		deployments[fmt.Sprintf("deployment%dReadyReplicas", n)] = fmt.Sprintf("${string(deploy%d.status.readyReplicas)}", n)
	}

	for i := 0; i < 5; i++ {
		n := i + 1
		services[fmt.Sprintf("service%dName", n)] = fmt.Sprintf("${svc%d.metadata.name}", n)
		jobs[fmt.Sprintf("job%dPhase", n)] = fmt.Sprintf(`${job%d.status.?completionTime.orValue(null) != null ? "complete" : "pending"}`, n)
		collections[fmt.Sprintf("regionConfigs%dCount", n)] = fmt.Sprintf("${string(regionConfigs%d.size())}", n)
		collections[fmt.Sprintf("matrixSecrets%dCount", n)] = fmt.Sprintf("${string(matrixSecrets%d.size())}", n)
		externalRefs[fmt.Sprintf("extConfig%dName", n)] = fmt.Sprintf("${extConfigs%d.metadata.name}", n)
		externalRefs[fmt.Sprintf("extServiceAccount%dName", n)] = fmt.Sprintf("${extServiceAccounts%d.metadata.name}", n)
	}

	return statusSchema
}

func circusPruneStatusSchema(value interface{}, droppedResourceIDs []string) (interface{}, bool) {
	switch typed := value.(type) {
	case string:
		for _, id := range droppedResourceIDs {
			if circusContainsIdentifier(typed, id) {
				return nil, false
			}
		}
		return typed, true
	case []interface{}:
		filtered := make([]interface{}, 0, len(typed))
		for _, item := range typed {
			pruned, keep := circusPruneStatusSchema(item, droppedResourceIDs)
			if keep {
				filtered = append(filtered, pruned)
			}
		}
		if len(filtered) == 0 && len(typed) > 0 {
			return nil, false
		}
		return filtered, true
	case map[string]interface{}:
		pruned := make(map[string]interface{}, len(typed))
		keys := make([]string, 0, len(typed))
		for key := range typed {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			prunedValue, keep := circusPruneStatusSchema(typed[key], droppedResourceIDs)
			if keep {
				pruned[key] = prunedValue
			}
		}
		if len(pruned) == 0 && len(typed) > 0 {
			return nil, false
		}
		return pruned, true
	default:
		return typed, true
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
