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
	Preset          string
	ConfigMaps      int
	ServiceAccounts int
	Roles           int
	RoleBindings    int
	Deployments     int
	Replicas        int64
}

var DefaultComplexities = map[string]Complexity{
	"low":              {Preset: "low", ConfigMaps: 3},
	"medium":           {Preset: "medium", ConfigMaps: 5, ServiceAccounts: 5, Roles: 5, RoleBindings: 5},
	"high":             {Preset: "high", ConfigMaps: 25, ServiceAccounts: 25, Roles: 25, RoleBindings: 25},
	"deployments":      {Preset: "deployments", ConfigMaps: 50, Deployments: 50, Replicas: 0},
	"deployment-heavy": {Preset: "deployment-heavy", ConfigMaps: 50, Deployments: 50, Replicas: 0},
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
