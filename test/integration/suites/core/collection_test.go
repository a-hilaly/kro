// Copyright 2025 The Kubernetes Authors.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package core_test

import (
	"fmt"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/rand"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	krov1alpha1 "github.com/kubernetes-sigs/kro/api/v1alpha1"
	"github.com/kubernetes-sigs/kro/pkg/controller/resourcegraphdefinition"
	"github.com/kubernetes-sigs/kro/pkg/testutil/generator"
)

var _ = Describe("ForEach Collections", func() {
	var (
		namespace string
	)

	BeforeEach(func(ctx SpecContext) {
		namespace = fmt.Sprintf("test-%s", rand.String(5))
		ns := &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: namespace,
			},
		}
		Expect(env.Client.Create(ctx, ns)).To(Succeed())
	})

	AfterEach(func(ctx SpecContext) {
		Expect(env.Client.Delete(ctx, &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: namespace,
			},
		})).To(Succeed())
	})

	It("should create multiple ConfigMaps from a forEach collection", func(ctx SpecContext) {
		rgd := generator.NewResourceGraphDefinition("test-collection",
			generator.WithSchema(
				"MultiConfigMap", "v1alpha1",
				map[string]interface{}{
					"name":   "string",
					"values": "[]string",
				},
				nil,
			),
			generator.WithResourceCollection("configmaps", map[string]interface{}{
				"apiVersion": "v1",
				"kind":       "ConfigMap",
				"metadata": map[string]interface{}{
					"name": "${schema.spec.name}-${value}",
				},
				"data": map[string]interface{}{
					"key": "${value}",
				},
			},
				[]krov1alpha1.ForEachDimension{
					{"value": "${schema.spec.values}"},
				},
				nil, nil),
		)

		// Create ResourceGraphDefinition
		Expect(env.Client.Create(ctx, rgd)).To(Succeed())

		// Verify ResourceGraphDefinition becomes ready
		createdRGD := &krov1alpha1.ResourceGraphDefinition{}
		Eventually(func(g Gomega, ctx SpecContext) {
			err := env.Client.Get(ctx, types.NamespacedName{
				Name: rgd.Name,
			}, createdRGD)
			g.Expect(err).ToNot(HaveOccurred())

			g.Expect(createdRGD.Status.State).To(Equal(krov1alpha1.ResourceGraphDefinitionStateActive))

			var readyCondition krov1alpha1.Condition
			for _, cond := range createdRGD.Status.Conditions {
				if cond.Type == resourcegraphdefinition.Ready {
					readyCondition = cond
				}
			}
			g.Expect(readyCondition.Status).To(Equal(metav1.ConditionTrue))
		}, 10*time.Second, time.Second).WithContext(ctx).Should(Succeed())

		// Create instance with 3 values
		name := "test-multi-cm"
		instance := &unstructured.Unstructured{
			Object: map[string]interface{}{
				"apiVersion": fmt.Sprintf("%s/%s", krov1alpha1.KRODomainName, "v1alpha1"),
				"kind":       "MultiConfigMap",
				"metadata": map[string]interface{}{
					"name":      name,
					"namespace": namespace,
				},
				"spec": map[string]interface{}{
					"name":   name,
					"values": []interface{}{"alpha", "beta", "gamma"},
				},
			},
		}
		Expect(env.Client.Create(ctx, instance)).To(Succeed())

		// Wait for instance to become ACTIVE
		Eventually(func(g Gomega, ctx SpecContext) {
			err := env.Client.Get(ctx, types.NamespacedName{
				Name:      name,
				Namespace: namespace,
			}, instance)
			g.Expect(err).ToNot(HaveOccurred())
			val, found, err := unstructured.NestedString(instance.Object, "status", "state")
			g.Expect(err).ToNot(HaveOccurred())
			g.Expect(found).To(BeTrue())
			g.Expect(val).To(Equal("ACTIVE"))
		}, 30*time.Second, time.Second).WithContext(ctx).Should(Succeed())

		// Verify all 3 ConfigMaps are created
		for _, value := range []string{"alpha", "beta", "gamma"} {
			cm := &corev1.ConfigMap{}
			Eventually(func(g Gomega, ctx SpecContext) {
				err := env.Client.Get(ctx, types.NamespacedName{
					Name:      fmt.Sprintf("%s-%s", name, value),
					Namespace: namespace,
				}, cm)
				g.Expect(err).ToNot(HaveOccurred())
				g.Expect(cm.Data["key"]).To(Equal(value))
			}, 20*time.Second, time.Second).WithContext(ctx).Should(Succeed())
		}

		// Cleanup
		Expect(env.Client.Delete(ctx, instance)).To(Succeed())

		Eventually(func(g Gomega, ctx SpecContext) {
			err := env.Client.Get(ctx, types.NamespacedName{
				Name:      name,
				Namespace: namespace,
			}, instance)
			g.Expect(err).To(MatchError(errors.IsNotFound, "instance should be deleted"))
		}, 20*time.Second, time.Second).WithContext(ctx).Should(Succeed())

		Expect(env.Client.Delete(ctx, rgd)).To(Succeed())

		Eventually(func(g Gomega, ctx SpecContext) {
			err := env.Client.Get(ctx, types.NamespacedName{
				Name: rgd.Name,
			}, &krov1alpha1.ResourceGraphDefinition{})
			g.Expect(err).To(MatchError(errors.IsNotFound, "rgd should be deleted"))
		}, 20*time.Second, time.Second).WithContext(ctx).Should(Succeed())
	})

	It("should handle cartesian product with multiple forEach iterators", func(ctx SpecContext) {
		rgd := generator.NewResourceGraphDefinition("test-cartesian",
			generator.WithSchema(
				"CartesianConfigMaps", "v1alpha1",
				map[string]interface{}{
					"name":    "string",
					"regions": "[]string",
					"tiers":   "[]string",
				},
				nil,
			),
			generator.WithResourceCollection("configmaps", map[string]interface{}{
				"apiVersion": "v1",
				"kind":       "ConfigMap",
				"metadata": map[string]interface{}{
					"name": "${schema.spec.name}-${region}-${tier}",
				},
				"data": map[string]interface{}{
					"region": "${region}",
					"tier":   "${tier}",
				},
			},
				[]krov1alpha1.ForEachDimension{
					{"region": "${schema.spec.regions}"},
					{"tier": "${schema.spec.tiers}"},
				},
				nil, nil),
		)

		// Create ResourceGraphDefinition
		Expect(env.Client.Create(ctx, rgd)).To(Succeed())

		// Verify ResourceGraphDefinition becomes ready
		createdRGD := &krov1alpha1.ResourceGraphDefinition{}
		Eventually(func(g Gomega, ctx SpecContext) {
			err := env.Client.Get(ctx, types.NamespacedName{
				Name: rgd.Name,
			}, createdRGD)
			g.Expect(err).ToNot(HaveOccurred())
			g.Expect(createdRGD.Status.State).To(Equal(krov1alpha1.ResourceGraphDefinitionStateActive))
		}, 10*time.Second, time.Second).WithContext(ctx).Should(Succeed())

		// Create instance with 2 regions x 2 tiers = 4 ConfigMaps
		name := "test-cartesian"
		instance := &unstructured.Unstructured{
			Object: map[string]interface{}{
				"apiVersion": fmt.Sprintf("%s/%s", krov1alpha1.KRODomainName, "v1alpha1"),
				"kind":       "CartesianConfigMaps",
				"metadata": map[string]interface{}{
					"name":      name,
					"namespace": namespace,
				},
				"spec": map[string]interface{}{
					"name":    name,
					"regions": []interface{}{"us", "eu"},
					"tiers":   []interface{}{"web", "api"},
				},
			},
		}
		Expect(env.Client.Create(ctx, instance)).To(Succeed())

		// Wait for instance to become ACTIVE
		Eventually(func(g Gomega, ctx SpecContext) {
			err := env.Client.Get(ctx, types.NamespacedName{
				Name:      name,
				Namespace: namespace,
			}, instance)
			g.Expect(err).ToNot(HaveOccurred())
			val, found, err := unstructured.NestedString(instance.Object, "status", "state")
			g.Expect(err).ToNot(HaveOccurred())
			g.Expect(found).To(BeTrue())
			g.Expect(val).To(Equal("ACTIVE"))
		}, 30*time.Second, time.Second).WithContext(ctx).Should(Succeed())

		// Verify all 4 ConfigMaps are created (cartesian product)
		expectedCombinations := []struct {
			region string
			tier   string
		}{
			{"us", "web"},
			{"us", "api"},
			{"eu", "web"},
			{"eu", "api"},
		}

		for _, combo := range expectedCombinations {
			cm := &corev1.ConfigMap{}
			Eventually(func(g Gomega, ctx SpecContext) {
				err := env.Client.Get(ctx, types.NamespacedName{
					Name:      fmt.Sprintf("%s-%s-%s", name, combo.region, combo.tier),
					Namespace: namespace,
				}, cm)
				g.Expect(err).ToNot(HaveOccurred())
				g.Expect(cm.Data["region"]).To(Equal(combo.region))
				g.Expect(cm.Data["tier"]).To(Equal(combo.tier))
			}, 20*time.Second, time.Second).WithContext(ctx).Should(Succeed())
		}

		// Cleanup
		Expect(env.Client.Delete(ctx, instance)).To(Succeed())

		Eventually(func(g Gomega, ctx SpecContext) {
			err := env.Client.Get(ctx, types.NamespacedName{
				Name:      name,
				Namespace: namespace,
			}, instance)
			g.Expect(err).To(MatchError(errors.IsNotFound, "instance should be deleted"))
		}, 20*time.Second, time.Second).WithContext(ctx).Should(Succeed())

		Expect(env.Client.Delete(ctx, rgd)).To(Succeed())

		Eventually(func(g Gomega, ctx SpecContext) {
			err := env.Client.Get(ctx, types.NamespacedName{
				Name: rgd.Name,
			}, &krov1alpha1.ResourceGraphDefinition{})
			g.Expect(err).To(MatchError(errors.IsNotFound, "rgd should be deleted"))
		}, 20*time.Second, time.Second).WithContext(ctx).Should(Succeed())
	})

	It("should create collection with includeWhen condition", func(ctx SpecContext) {
		rgd := generator.NewResourceGraphDefinition("test-conditional-collection",
			generator.WithSchema(
				"ConditionalCollection", "v1alpha1",
				map[string]interface{}{
					"name":    "string",
					"values":  "[]string",
					"enabled": "boolean",
				},
				nil,
			),
			generator.WithResourceCollection("configmaps", map[string]interface{}{
				"apiVersion": "v1",
				"kind":       "ConfigMap",
				"metadata": map[string]interface{}{
					"name": "${schema.spec.name}-${value}",
				},
				"data": map[string]interface{}{
					"key": "${value}",
				},
			},
				[]krov1alpha1.ForEachDimension{
					{"value": "${schema.spec.values}"},
				},
				nil,
				[]string{"${schema.spec.enabled}"}, // includeWhen
			),
		)

		// Create ResourceGraphDefinition
		Expect(env.Client.Create(ctx, rgd)).To(Succeed())

		// Verify ResourceGraphDefinition becomes ready
		createdRGD := &krov1alpha1.ResourceGraphDefinition{}
		Eventually(func(g Gomega, ctx SpecContext) {
			err := env.Client.Get(ctx, types.NamespacedName{
				Name: rgd.Name,
			}, createdRGD)
			g.Expect(err).ToNot(HaveOccurred())
			g.Expect(createdRGD.Status.State).To(Equal(krov1alpha1.ResourceGraphDefinitionStateActive))
		}, 10*time.Second, time.Second).WithContext(ctx).Should(Succeed())

		// Test 1: Create instance with enabled=false - no ConfigMaps should be created
		name := "test-disabled"
		instance := &unstructured.Unstructured{
			Object: map[string]interface{}{
				"apiVersion": fmt.Sprintf("%s/%s", krov1alpha1.KRODomainName, "v1alpha1"),
				"kind":       "ConditionalCollection",
				"metadata": map[string]interface{}{
					"name":      name,
					"namespace": namespace,
				},
				"spec": map[string]interface{}{
					"name":    name,
					"values":  []interface{}{"alpha", "beta"},
					"enabled": false,
				},
			},
		}
		Expect(env.Client.Create(ctx, instance)).To(Succeed())

		// Wait for instance to become ACTIVE
		Eventually(func(g Gomega, ctx SpecContext) {
			err := env.Client.Get(ctx, types.NamespacedName{
				Name:      name,
				Namespace: namespace,
			}, instance)
			g.Expect(err).ToNot(HaveOccurred())
			val, found, err := unstructured.NestedString(instance.Object, "status", "state")
			g.Expect(err).ToNot(HaveOccurred())
			g.Expect(found).To(BeTrue())
			g.Expect(val).To(Equal("ACTIVE"))
		}, 30*time.Second, time.Second).WithContext(ctx).Should(Succeed())

		// Verify no ConfigMaps are created
		for _, value := range []string{"alpha", "beta"} {
			cm := &corev1.ConfigMap{}
			Consistently(func(g Gomega, ctx SpecContext) {
				err := env.Client.Get(ctx, types.NamespacedName{
					Name:      fmt.Sprintf("%s-%s", name, value),
					Namespace: namespace,
				}, cm)
				g.Expect(errors.IsNotFound(err)).To(BeTrue())
			}, 5*time.Second, time.Second).WithContext(ctx).Should(Succeed())
		}

		// Cleanup
		Expect(env.Client.Delete(ctx, instance)).To(Succeed())
		Eventually(func(g Gomega, ctx SpecContext) {
			err := env.Client.Get(ctx, types.NamespacedName{
				Name:      name,
				Namespace: namespace,
			}, instance)
			g.Expect(err).To(MatchError(errors.IsNotFound, "instance should be deleted"))
		}, 20*time.Second, time.Second).WithContext(ctx).Should(Succeed())

		Expect(env.Client.Delete(ctx, rgd)).To(Succeed())
		Eventually(func(g Gomega, ctx SpecContext) {
			err := env.Client.Get(ctx, types.NamespacedName{
				Name: rgd.Name,
			}, &krov1alpha1.ResourceGraphDefinition{})
			g.Expect(err).To(MatchError(errors.IsNotFound, "rgd should be deleted"))
		}, 20*time.Second, time.Second).WithContext(ctx).Should(Succeed())
	})

	// Test: includeWhen toggling from false to true
	// Verifies that when includeWhen changes from false to true, the collection
	// resources are created, and vice versa.
	It("should toggle collection resources when includeWhen changes", func(ctx SpecContext) {
		rgd := generator.NewResourceGraphDefinition("test-toggle-collection",
			generator.WithSchema(
				"ToggleCollection", "v1alpha1",
				map[string]interface{}{
					"name":    "string",
					"values":  "[]string",
					"enabled": "boolean",
				},
				nil,
			),
			generator.WithResourceCollection("configs", map[string]interface{}{
				"apiVersion": "v1",
				"kind":       "ConfigMap",
				"metadata": map[string]interface{}{
					"name": "${schema.spec.name}-${value}",
				},
				"data": map[string]interface{}{
					"key": "${value}",
				},
			},
				[]krov1alpha1.ForEachDimension{
					{"value": "${schema.spec.values}"},
				},
				nil,
				[]string{"${schema.spec.enabled}"}, // includeWhen
			),
		)

		// Create ResourceGraphDefinition
		Expect(env.Client.Create(ctx, rgd)).To(Succeed())

		// Verify ResourceGraphDefinition becomes ready
		createdRGD := &krov1alpha1.ResourceGraphDefinition{}
		Eventually(func(g Gomega, ctx SpecContext) {
			err := env.Client.Get(ctx, types.NamespacedName{
				Name: rgd.Name,
			}, createdRGD)
			g.Expect(err).ToNot(HaveOccurred())
			g.Expect(createdRGD.Status.State).To(Equal(krov1alpha1.ResourceGraphDefinitionStateActive))
		}, 10*time.Second, time.Second).WithContext(ctx).Should(Succeed())

		// Phase 1: Create instance with enabled=false - no ConfigMaps should be created
		name := "test-toggle"
		instance := &unstructured.Unstructured{
			Object: map[string]interface{}{
				"apiVersion": fmt.Sprintf("%s/%s", krov1alpha1.KRODomainName, "v1alpha1"),
				"kind":       "ToggleCollection",
				"metadata": map[string]interface{}{
					"name":      name,
					"namespace": namespace,
				},
				"spec": map[string]interface{}{
					"name":    name,
					"values":  []interface{}{"alpha", "beta"},
					"enabled": false,
				},
			},
		}
		Expect(env.Client.Create(ctx, instance)).To(Succeed())

		// Wait for instance to become ACTIVE
		Eventually(func(g Gomega, ctx SpecContext) {
			err := env.Client.Get(ctx, types.NamespacedName{
				Name:      name,
				Namespace: namespace,
			}, instance)
			g.Expect(err).ToNot(HaveOccurred())
			val, found, err := unstructured.NestedString(instance.Object, "status", "state")
			g.Expect(err).ToNot(HaveOccurred())
			g.Expect(found).To(BeTrue())
			g.Expect(val).To(Equal("ACTIVE"))
		}, 30*time.Second, time.Second).WithContext(ctx).Should(Succeed())

		// Verify no ConfigMaps are created (enabled=false)
		for _, value := range []string{"alpha", "beta"} {
			cm := &corev1.ConfigMap{}
			Consistently(func(g Gomega, ctx SpecContext) {
				err := env.Client.Get(ctx, types.NamespacedName{
					Name:      fmt.Sprintf("%s-%s", name, value),
					Namespace: namespace,
				}, cm)
				g.Expect(errors.IsNotFound(err)).To(BeTrue())
			}, 5*time.Second, time.Second).WithContext(ctx).Should(Succeed())
		}

		// Phase 2: Toggle enabled to true - ConfigMaps should be created
		Eventually(func(g Gomega, ctx SpecContext) {
			err := env.Client.Get(ctx, types.NamespacedName{
				Name:      name,
				Namespace: namespace,
			}, instance)
			g.Expect(err).ToNot(HaveOccurred())

			err = unstructured.SetNestedField(instance.Object, true, "spec", "enabled")
			g.Expect(err).ToNot(HaveOccurred())

			err = env.Client.Update(ctx, instance)
			g.Expect(err).ToNot(HaveOccurred())
		}, 10*time.Second, time.Second).WithContext(ctx).Should(Succeed())

		// Verify ConfigMaps are now created
		for _, value := range []string{"alpha", "beta"} {
			cm := &corev1.ConfigMap{}
			Eventually(func(g Gomega, ctx SpecContext) {
				err := env.Client.Get(ctx, types.NamespacedName{
					Name:      fmt.Sprintf("%s-%s", name, value),
					Namespace: namespace,
				}, cm)
				g.Expect(err).ToNot(HaveOccurred())
				g.Expect(cm.Data["key"]).To(Equal(value))
			}, 30*time.Second, time.Second).WithContext(ctx).Should(Succeed())
		}

		// Phase 3: Toggle enabled back to false - ConfigMaps should be deleted
		Eventually(func(g Gomega, ctx SpecContext) {
			err := env.Client.Get(ctx, types.NamespacedName{
				Name:      name,
				Namespace: namespace,
			}, instance)
			g.Expect(err).ToNot(HaveOccurred())

			err = unstructured.SetNestedField(instance.Object, false, "spec", "enabled")
			g.Expect(err).ToNot(HaveOccurred())

			err = env.Client.Update(ctx, instance)
			g.Expect(err).ToNot(HaveOccurred())
		}, 10*time.Second, time.Second).WithContext(ctx).Should(Succeed())

		// Verify ConfigMaps are deleted
		for _, value := range []string{"alpha", "beta"} {
			Eventually(func(g Gomega, ctx SpecContext) {
				cm := &corev1.ConfigMap{}
				err := env.Client.Get(ctx, types.NamespacedName{
					Name:      fmt.Sprintf("%s-%s", name, value),
					Namespace: namespace,
				}, cm)
				g.Expect(err).To(MatchError(errors.IsNotFound, "ConfigMap should be deleted"))
			}, 30*time.Second, time.Second).WithContext(ctx).Should(Succeed())
		}

		// Cleanup
		Expect(env.Client.Delete(ctx, instance)).To(Succeed())
		Eventually(func(g Gomega, ctx SpecContext) {
			err := env.Client.Get(ctx, types.NamespacedName{
				Name:      name,
				Namespace: namespace,
			}, instance)
			g.Expect(err).To(MatchError(errors.IsNotFound, "instance should be deleted"))
		}, 20*time.Second, time.Second).WithContext(ctx).Should(Succeed())

		Expect(env.Client.Delete(ctx, rgd)).To(Succeed())
		Eventually(func(g Gomega, ctx SpecContext) {
			err := env.Client.Get(ctx, types.NamespacedName{
				Name: rgd.Name,
			}, &krov1alpha1.ResourceGraphDefinition{})
			g.Expect(err).To(MatchError(errors.IsNotFound, "rgd should be deleted"))
		}, 20*time.Second, time.Second).WithContext(ctx).Should(Succeed())
	})

	It("should create collection with dependency on regular resource", func(ctx SpecContext) {
		rgd := generator.NewResourceGraphDefinition("test-collection-dependency",
			generator.WithSchema(
				"CollectionWithDependency", "v1alpha1",
				map[string]interface{}{
					"name":   "string",
					"values": "[]string",
				},
				nil,
			),
			// First resource: a regular ConfigMap
			generator.WithResource("baseConfig", map[string]interface{}{
				"apiVersion": "v1",
				"kind":       "ConfigMap",
				"metadata": map[string]interface{}{
					"name": "${schema.spec.name}-base",
				},
				"data": map[string]interface{}{
					"version": "v1.0.0",
				},
			}, nil, nil),
			// Second resource: collection that depends on the base config
			generator.WithResourceCollection("configs", map[string]interface{}{
				"apiVersion": "v1",
				"kind":       "ConfigMap",
				"metadata": map[string]interface{}{
					"name": "${schema.spec.name}-${value}",
				},
				"data": map[string]interface{}{
					"key":     "${value}",
					"version": "${baseConfig.data.version}",
				},
			},
				[]krov1alpha1.ForEachDimension{
					{"value": "${schema.spec.values}"},
				},
				nil, nil),
		)

		// Create ResourceGraphDefinition
		Expect(env.Client.Create(ctx, rgd)).To(Succeed())

		// Verify ResourceGraphDefinition becomes ready
		createdRGD := &krov1alpha1.ResourceGraphDefinition{}
		Eventually(func(g Gomega, ctx SpecContext) {
			err := env.Client.Get(ctx, types.NamespacedName{
				Name: rgd.Name,
			}, createdRGD)
			g.Expect(err).ToNot(HaveOccurred())
			g.Expect(createdRGD.Status.State).To(Equal(krov1alpha1.ResourceGraphDefinitionStateActive))
		}, 10*time.Second, time.Second).WithContext(ctx).Should(Succeed())

		// Create instance
		name := "test-dep"
		instance := &unstructured.Unstructured{
			Object: map[string]interface{}{
				"apiVersion": fmt.Sprintf("%s/%s", krov1alpha1.KRODomainName, "v1alpha1"),
				"kind":       "CollectionWithDependency",
				"metadata": map[string]interface{}{
					"name":      name,
					"namespace": namespace,
				},
				"spec": map[string]interface{}{
					"name":   name,
					"values": []interface{}{"one", "two"},
				},
			},
		}
		Expect(env.Client.Create(ctx, instance)).To(Succeed())

		// Wait for instance to become ACTIVE
		Eventually(func(g Gomega, ctx SpecContext) {
			err := env.Client.Get(ctx, types.NamespacedName{
				Name:      name,
				Namespace: namespace,
			}, instance)
			g.Expect(err).ToNot(HaveOccurred())
			val, found, err := unstructured.NestedString(instance.Object, "status", "state")
			g.Expect(err).ToNot(HaveOccurred())
			g.Expect(found).To(BeTrue())
			g.Expect(val).To(Equal("ACTIVE"))
		}, 30*time.Second, time.Second).WithContext(ctx).Should(Succeed())

		// Verify base ConfigMap exists
		baseCM := &corev1.ConfigMap{}
		Eventually(func(g Gomega, ctx SpecContext) {
			err := env.Client.Get(ctx, types.NamespacedName{
				Name:      fmt.Sprintf("%s-base", name),
				Namespace: namespace,
			}, baseCM)
			g.Expect(err).ToNot(HaveOccurred())
			g.Expect(baseCM.Data["version"]).To(Equal("v1.0.0"))
		}, 20*time.Second, time.Second).WithContext(ctx).Should(Succeed())

		// Verify collection ConfigMaps exist with data from base
		for _, value := range []string{"one", "two"} {
			cm := &corev1.ConfigMap{}
			Eventually(func(g Gomega, ctx SpecContext) {
				err := env.Client.Get(ctx, types.NamespacedName{
					Name:      fmt.Sprintf("%s-%s", name, value),
					Namespace: namespace,
				}, cm)
				g.Expect(err).ToNot(HaveOccurred())
				g.Expect(cm.Data["key"]).To(Equal(value))
				g.Expect(cm.Data["version"]).To(Equal("v1.0.0"))
			}, 20*time.Second, time.Second).WithContext(ctx).Should(Succeed())
		}

		// Cleanup
		Expect(env.Client.Delete(ctx, instance)).To(Succeed())
		Eventually(func(g Gomega, ctx SpecContext) {
			err := env.Client.Get(ctx, types.NamespacedName{
				Name:      name,
				Namespace: namespace,
			}, instance)
			g.Expect(err).To(MatchError(errors.IsNotFound, "instance should be deleted"))
		}, 20*time.Second, time.Second).WithContext(ctx).Should(Succeed())

		Expect(env.Client.Delete(ctx, rgd)).To(Succeed())
		Eventually(func(g Gomega, ctx SpecContext) {
			err := env.Client.Get(ctx, types.NamespacedName{
				Name: rgd.Name,
			}, &krov1alpha1.ResourceGraphDefinition{})
			g.Expect(err).To(MatchError(errors.IsNotFound, "rgd should be deleted"))
		}, 20*time.Second, time.Second).WithContext(ctx).Should(Succeed())
	})

	It("should handle collection chaining with dynamic forEach expression", func(ctx SpecContext) {
		// This tests collection chaining where the forEach expression references another resource
		rgd := generator.NewResourceGraphDefinition("test-collection-chaining",
			generator.WithSchema(
				"CollectionChaining", "v1alpha1",
				map[string]interface{}{
					"name":   "string",
					"values": "[]string",
				},
				nil,
			),
			// First resource: a regular ConfigMap that must exist before collection expands
			generator.WithResource("baseConfig", map[string]interface{}{
				"apiVersion": "v1",
				"kind":       "ConfigMap",
				"metadata": map[string]interface{}{
					"name": "${schema.spec.name}-base",
				},
				"data": map[string]interface{}{
					"enabled": "true",
					"prefix":  "chained",
				},
			}, nil, nil),
			// Second resource: collection with forEach that references the first resource
			// The forEach expression checks if baseConfig.data.enabled exists
			generator.WithResourceCollection("chainedConfigs", map[string]interface{}{
				"apiVersion": "v1",
				"kind":       "ConfigMap",
				"metadata": map[string]interface{}{
					"name": "${schema.spec.name}-${val}",
				},
				"data": map[string]interface{}{
					"key":    "${val}",
					"prefix": "${baseConfig.data.prefix}",
				},
			},
				[]krov1alpha1.ForEachDimension{
					// forEach expression references baseConfig (another resource)
					// This creates a dynamic dependency on baseConfig
					{"val": "${has(baseConfig.data.enabled) ? schema.spec.values : []}"},
				},
				nil, nil),
		)

		// Create ResourceGraphDefinition
		Expect(env.Client.Create(ctx, rgd)).To(Succeed())

		// Verify ResourceGraphDefinition becomes ready
		createdRGD := &krov1alpha1.ResourceGraphDefinition{}
		Eventually(func(g Gomega, ctx SpecContext) {
			err := env.Client.Get(ctx, types.NamespacedName{
				Name: rgd.Name,
			}, createdRGD)
			g.Expect(err).ToNot(HaveOccurred())
			g.Expect(createdRGD.Status.State).To(Equal(krov1alpha1.ResourceGraphDefinitionStateActive))
		}, 10*time.Second, time.Second).WithContext(ctx).Should(Succeed())

		// Create instance with values
		name := "test-chaining"
		instance := &unstructured.Unstructured{
			Object: map[string]interface{}{
				"apiVersion": fmt.Sprintf("%s/%s", krov1alpha1.KRODomainName, "v1alpha1"),
				"kind":       "CollectionChaining",
				"metadata": map[string]interface{}{
					"name":      name,
					"namespace": namespace,
				},
				"spec": map[string]interface{}{
					"name":   name,
					"values": []interface{}{"one", "two"},
				},
			},
		}
		Expect(env.Client.Create(ctx, instance)).To(Succeed())

		// Wait for instance to become ACTIVE
		Eventually(func(g Gomega, ctx SpecContext) {
			err := env.Client.Get(ctx, types.NamespacedName{
				Name:      name,
				Namespace: namespace,
			}, instance)
			g.Expect(err).ToNot(HaveOccurred())
			val, found, err := unstructured.NestedString(instance.Object, "status", "state")
			g.Expect(err).ToNot(HaveOccurred())
			g.Expect(found).To(BeTrue())
			g.Expect(val).To(Equal("ACTIVE"))
		}, 30*time.Second, time.Second).WithContext(ctx).Should(Succeed())

		// Verify base ConfigMap exists
		baseCM := &corev1.ConfigMap{}
		Eventually(func(g Gomega, ctx SpecContext) {
			err := env.Client.Get(ctx, types.NamespacedName{
				Name:      fmt.Sprintf("%s-base", name),
				Namespace: namespace,
			}, baseCM)
			g.Expect(err).ToNot(HaveOccurred())
			g.Expect(baseCM.Data["enabled"]).To(Equal("true"))
			g.Expect(baseCM.Data["prefix"]).To(Equal("chained"))
		}, 20*time.Second, time.Second).WithContext(ctx).Should(Succeed())

		// Verify collection ConfigMaps exist with data from base
		for _, value := range []string{"one", "two"} {
			cm := &corev1.ConfigMap{}
			Eventually(func(g Gomega, ctx SpecContext) {
				err := env.Client.Get(ctx, types.NamespacedName{
					Name:      fmt.Sprintf("%s-%s", name, value),
					Namespace: namespace,
				}, cm)
				g.Expect(err).ToNot(HaveOccurred())
				g.Expect(cm.Data["key"]).To(Equal(value))
				g.Expect(cm.Data["prefix"]).To(Equal("chained"))
			}, 20*time.Second, time.Second).WithContext(ctx).Should(Succeed())
		}

		// Cleanup
		Expect(env.Client.Delete(ctx, instance)).To(Succeed())
		Eventually(func(g Gomega, ctx SpecContext) {
			err := env.Client.Get(ctx, types.NamespacedName{
				Name:      name,
				Namespace: namespace,
			}, instance)
			g.Expect(err).To(MatchError(errors.IsNotFound, "instance should be deleted"))
		}, 20*time.Second, time.Second).WithContext(ctx).Should(Succeed())

		Expect(env.Client.Delete(ctx, rgd)).To(Succeed())
		Eventually(func(g Gomega, ctx SpecContext) {
			err := env.Client.Get(ctx, types.NamespacedName{
				Name: rgd.Name,
			}, &krov1alpha1.ResourceGraphDefinition{})
			g.Expect(err).To(MatchError(errors.IsNotFound, "rgd should be deleted"))
		}, 20*time.Second, time.Second).WithContext(ctx).Should(Succeed())
	})

	It("should handle collection-to-collection chaining", func(ctx SpecContext) {
		// This tests one collection iterating over another collection's output
		// The second collection's forEach expression is: ${firstConfigs}
		// which returns the list of expanded ConfigMaps from the first collection
		rgd := generator.NewResourceGraphDefinition("test-collection-to-collection",
			generator.WithSchema(
				"CollectionToCollection", "v1alpha1",
				map[string]interface{}{
					"name":   "string",
					"values": "[]string",
				},
				nil,
			),
			// First collection: creates multiple ConfigMaps
			generator.WithResourceCollection("firstConfigs", map[string]interface{}{
				"apiVersion": "v1",
				"kind":       "ConfigMap",
				"metadata": map[string]interface{}{
					"name": "${schema.spec.name}-first-${val}",
				},
				"data": map[string]interface{}{
					"key":    "${val}",
					"source": "first-collection",
				},
			},
				[]krov1alpha1.ForEachDimension{
					{"val": "${schema.spec.values}"},
				},
				nil, nil),
			// Second collection: iterates over the first collection
			// ${firstConfigs} is typed as list(ConfigMap)
			generator.WithResourceCollection("secondConfigs", map[string]interface{}{
				"apiVersion": "v1",
				"kind":       "ConfigMap",
				"metadata": map[string]interface{}{
					"name": "${schema.spec.name}-second-${config.data.key}",
				},
				"data": map[string]interface{}{
					"originalKey":    "${config.data.key}",
					"originalSource": "${config.data.source}",
					"source":         "second-collection",
				},
			},
				[]krov1alpha1.ForEachDimension{
					// forEach iterates over another collection - ${firstConfigs} returns list(ConfigMap)
					{"config": "${firstConfigs}"},
				},
				nil, nil),
		)

		// Create ResourceGraphDefinition
		Expect(env.Client.Create(ctx, rgd)).To(Succeed())

		// Verify ResourceGraphDefinition becomes ready
		createdRGD := &krov1alpha1.ResourceGraphDefinition{}
		Eventually(func(g Gomega, ctx SpecContext) {
			err := env.Client.Get(ctx, types.NamespacedName{
				Name: rgd.Name,
			}, createdRGD)
			g.Expect(err).ToNot(HaveOccurred())
			g.Expect(createdRGD.Status.State).To(Equal(krov1alpha1.ResourceGraphDefinitionStateActive))
		}, 10*time.Second, time.Second).WithContext(ctx).Should(Succeed())

		// Create instance with values
		name := "test-c2c"
		instance := &unstructured.Unstructured{
			Object: map[string]interface{}{
				"apiVersion": fmt.Sprintf("%s/%s", krov1alpha1.KRODomainName, "v1alpha1"),
				"kind":       "CollectionToCollection",
				"metadata": map[string]interface{}{
					"name":      name,
					"namespace": namespace,
				},
				"spec": map[string]interface{}{
					"name":   name,
					"values": []interface{}{"alpha", "beta"},
				},
			},
		}
		Expect(env.Client.Create(ctx, instance)).To(Succeed())

		// Wait for instance to become ACTIVE
		Eventually(func(g Gomega, ctx SpecContext) {
			err := env.Client.Get(ctx, types.NamespacedName{
				Name:      name,
				Namespace: namespace,
			}, instance)
			g.Expect(err).ToNot(HaveOccurred())
			val, found, err := unstructured.NestedString(instance.Object, "status", "state")
			g.Expect(err).ToNot(HaveOccurred())
			g.Expect(found).To(BeTrue())
			g.Expect(val).To(Equal("ACTIVE"))
		}, 30*time.Second, time.Second).WithContext(ctx).Should(Succeed())

		// Verify first collection ConfigMaps exist
		for _, value := range []string{"alpha", "beta"} {
			cm := &corev1.ConfigMap{}
			Eventually(func(g Gomega, ctx SpecContext) {
				err := env.Client.Get(ctx, types.NamespacedName{
					Name:      fmt.Sprintf("%s-first-%s", name, value),
					Namespace: namespace,
				}, cm)
				g.Expect(err).ToNot(HaveOccurred())
				g.Expect(cm.Data["key"]).To(Equal(value))
				g.Expect(cm.Data["source"]).To(Equal("first-collection"))
			}, 20*time.Second, time.Second).WithContext(ctx).Should(Succeed())
		}

		// Verify second collection ConfigMaps exist (created by iterating over first collection)
		for _, value := range []string{"alpha", "beta"} {
			cm := &corev1.ConfigMap{}
			Eventually(func(g Gomega, ctx SpecContext) {
				err := env.Client.Get(ctx, types.NamespacedName{
					Name:      fmt.Sprintf("%s-second-%s", name, value),
					Namespace: namespace,
				}, cm)
				g.Expect(err).ToNot(HaveOccurred())
				g.Expect(cm.Data["originalKey"]).To(Equal(value))
				g.Expect(cm.Data["originalSource"]).To(Equal("first-collection"))
				g.Expect(cm.Data["source"]).To(Equal("second-collection"))
			}, 20*time.Second, time.Second).WithContext(ctx).Should(Succeed())
		}

		// Cleanup
		Expect(env.Client.Delete(ctx, instance)).To(Succeed())
		Eventually(func(g Gomega, ctx SpecContext) {
			err := env.Client.Get(ctx, types.NamespacedName{
				Name:      name,
				Namespace: namespace,
			}, instance)
			g.Expect(err).To(MatchError(errors.IsNotFound, "instance should be deleted"))
		}, 20*time.Second, time.Second).WithContext(ctx).Should(Succeed())

		Expect(env.Client.Delete(ctx, rgd)).To(Succeed())
		Eventually(func(g Gomega, ctx SpecContext) {
			err := env.Client.Get(ctx, types.NamespacedName{
				Name: rgd.Name,
			}, &krov1alpha1.ResourceGraphDefinition{})
			g.Expect(err).To(MatchError(errors.IsNotFound, "rgd should be deleted"))
		}, 20*time.Second, time.Second).WithContext(ctx).Should(Succeed())
	})

	It("should handle empty collection list gracefully", func(ctx SpecContext) {
		rgd := generator.NewResourceGraphDefinition("test-empty-collection",
			generator.WithSchema(
				"EmptyCollection", "v1alpha1",
				map[string]interface{}{
					"name":   "string",
					"values": "[]string",
				},
				nil,
			),
			generator.WithResourceCollection("configmaps", map[string]interface{}{
				"apiVersion": "v1",
				"kind":       "ConfigMap",
				"metadata": map[string]interface{}{
					"name": "${schema.spec.name}-${value}",
				},
				"data": map[string]interface{}{
					"key": "${value}",
				},
			},
				[]krov1alpha1.ForEachDimension{
					{"value": "${schema.spec.values}"},
				},
				nil, nil),
		)

		// Create ResourceGraphDefinition
		Expect(env.Client.Create(ctx, rgd)).To(Succeed())

		// Verify ResourceGraphDefinition becomes ready
		createdRGD := &krov1alpha1.ResourceGraphDefinition{}
		Eventually(func(g Gomega, ctx SpecContext) {
			err := env.Client.Get(ctx, types.NamespacedName{
				Name: rgd.Name,
			}, createdRGD)
			g.Expect(err).ToNot(HaveOccurred())
			g.Expect(createdRGD.Status.State).To(Equal(krov1alpha1.ResourceGraphDefinitionStateActive))
		}, 10*time.Second, time.Second).WithContext(ctx).Should(Succeed())

		// Create instance with empty values list
		name := "test-empty"
		instance := &unstructured.Unstructured{
			Object: map[string]interface{}{
				"apiVersion": fmt.Sprintf("%s/%s", krov1alpha1.KRODomainName, "v1alpha1"),
				"kind":       "EmptyCollection",
				"metadata": map[string]interface{}{
					"name":      name,
					"namespace": namespace,
				},
				"spec": map[string]interface{}{
					"name":   name,
					"values": []interface{}{}, // Empty list
				},
			},
		}
		Expect(env.Client.Create(ctx, instance)).To(Succeed())

		// Instance should still become ACTIVE (no resources to create is valid)
		Eventually(func(g Gomega, ctx SpecContext) {
			err := env.Client.Get(ctx, types.NamespacedName{
				Name:      name,
				Namespace: namespace,
			}, instance)
			g.Expect(err).ToNot(HaveOccurred())
			val, found, err := unstructured.NestedString(instance.Object, "status", "state")
			g.Expect(err).ToNot(HaveOccurred())
			g.Expect(found).To(BeTrue())
			g.Expect(val).To(Equal("ACTIVE"))
		}, 30*time.Second, time.Second).WithContext(ctx).Should(Succeed())

		// Cleanup
		Expect(env.Client.Delete(ctx, instance)).To(Succeed())

		Eventually(func(g Gomega, ctx SpecContext) {
			err := env.Client.Get(ctx, types.NamespacedName{
				Name:      name,
				Namespace: namespace,
			}, instance)
			g.Expect(err).To(MatchError(errors.IsNotFound, "instance should be deleted"))
		}, 20*time.Second, time.Second).WithContext(ctx).Should(Succeed())

		Expect(env.Client.Delete(ctx, rgd)).To(Succeed())

		Eventually(func(g Gomega, ctx SpecContext) {
			err := env.Client.Get(ctx, types.NamespacedName{
				Name: rgd.Name,
			}, &krov1alpha1.ResourceGraphDefinition{})
			g.Expect(err).To(MatchError(errors.IsNotFound, "rgd should be deleted"))
		}, 20*time.Second, time.Second).WithContext(ctx).Should(Succeed())
	})

	// This is a comprehensive test for deep chaining:
	// BaseConfig (ConfigMap) -> Collection1 -> Collection2 -> SummaryConfig (ConfigMap) -> FinalPods (Collection)
	// Then we scale up/down by editing the instance spec and verify all dependent collections update.
	// The schema status captures information from the final pods collection.
	It("should handle deep chaining with scale up and scale down", func(ctx SpecContext) {
		rgd := generator.NewResourceGraphDefinition("test-deep-chaining",
			generator.WithSchema(
				"DeepChain", "v1alpha1",
				map[string]interface{}{
					"name":   "string",
					"items":  "[]string",
					"prefix": "string",
				},
				nil, // Status expressions removed to test collection chaining first
			),
			// Resource 1: Base ConfigMap that holds the items list
			generator.WithResource("baseConfig", map[string]interface{}{
				"apiVersion": "v1",
				"kind":       "ConfigMap",
				"metadata": map[string]interface{}{
					"name": "${schema.spec.name}-base",
				},
				"data": map[string]interface{}{
					"prefix":    "${schema.spec.prefix}",
					"itemCount": "${string(size(schema.spec.items))}",
				},
			}, nil, nil),
			// Resource 2: Level 1 Collection - iterates over schema.spec.items
			generator.WithResourceCollection("level1Configs", map[string]interface{}{
				"apiVersion": "v1",
				"kind":       "ConfigMap",
				"metadata": map[string]interface{}{
					"name": "${schema.spec.name}-l1-${entry}",
				},
				"data": map[string]interface{}{
					"entry":  "${entry}",
					"prefix": "${baseConfig.data.prefix}",
					"level":  "1",
				},
			},
				[]krov1alpha1.ForEachDimension{
					{"entry": "${schema.spec.items}"},
				},
				nil, nil),
			// Resource 3: Level 2 Collection - iterates over level1Configs (collection-to-collection)
			generator.WithResourceCollection("level2Configs", map[string]interface{}{
				"apiVersion": "v1",
				"kind":       "ConfigMap",
				"metadata": map[string]interface{}{
					"name": "${schema.spec.name}-l2-${l1.data.entry}",
				},
				"data": map[string]interface{}{
					"sourceEntry":  "${l1.data.entry}",
					"sourcePrefix": "${l1.data.prefix}",
					"level":        "2",
				},
			},
				[]krov1alpha1.ForEachDimension{
					{"l1": "${level1Configs}"},
				},
				nil, nil),
			// Resource 4: Summary ConfigMap - aggregates data from level2 collection
			generator.WithResource("summaryConfig", map[string]interface{}{
				"apiVersion": "v1",
				"kind":       "ConfigMap",
				"metadata": map[string]interface{}{
					"name": "${schema.spec.name}-summary",
				},
				"data": map[string]interface{}{
					"level1Count": "${string(size(level1Configs))}",
					"level2Count": "${string(size(level2Configs))}",
				},
			}, nil, nil),
			// Resource 5: Final Pods Collection - iterates over level2Configs, depends on summaryConfig
			generator.WithResourceCollection("finalPods", map[string]interface{}{
				"apiVersion": "v1",
				"kind":       "Pod",
				"metadata": map[string]interface{}{
					"name": "${schema.spec.name}-pod-${l2.data.sourceEntry}",
				},
				"spec": map[string]interface{}{
					"restartPolicy": "Never",
					"containers": []interface{}{
						map[string]interface{}{
							"name":    "worker",
							"image":   "busybox:latest",
							"command": []interface{}{"sh", "-c", "echo ${summaryConfig.data.level2Count} items && sleep 3600"},
						},
					},
				},
			},
				[]krov1alpha1.ForEachDimension{
					{"l2": "${level2Configs}"},
				},
				nil, nil),
		)

		// Create ResourceGraphDefinition
		Expect(env.Client.Create(ctx, rgd)).To(Succeed())

		// Verify ResourceGraphDefinition becomes ready
		createdRGD := &krov1alpha1.ResourceGraphDefinition{}
		Eventually(func(g Gomega, ctx SpecContext) {
			err := env.Client.Get(ctx, types.NamespacedName{
				Name: rgd.Name,
			}, createdRGD)
			g.Expect(err).ToNot(HaveOccurred())
			g.Expect(createdRGD.Status.State).To(Equal(krov1alpha1.ResourceGraphDefinitionStateActive))
		}, 15*time.Second, time.Second).WithContext(ctx).Should(Succeed())

		// Create instance with initial items [a, b]
		name := "test-deep"
		instance := &unstructured.Unstructured{
			Object: map[string]interface{}{
				"apiVersion": fmt.Sprintf("%s/%s", krov1alpha1.KRODomainName, "v1alpha1"),
				"kind":       "DeepChain",
				"metadata": map[string]interface{}{
					"name":      name,
					"namespace": namespace,
				},
				"spec": map[string]interface{}{
					"name":   name,
					"items":  []interface{}{"a", "b"},
					"prefix": "test",
				},
			},
		}
		Expect(env.Client.Create(ctx, instance)).To(Succeed())

		// Wait for instance to become ACTIVE
		Eventually(func(g Gomega, ctx SpecContext) {
			err := env.Client.Get(ctx, types.NamespacedName{
				Name:      name,
				Namespace: namespace,
			}, instance)
			g.Expect(err).ToNot(HaveOccurred())
			val, found, err := unstructured.NestedString(instance.Object, "status", "state")
			g.Expect(err).ToNot(HaveOccurred())
			g.Expect(found).To(BeTrue())
			g.Expect(val).To(Equal("ACTIVE"))
		}, 60*time.Second, time.Second).WithContext(ctx).Should(Succeed())

		// Verify base config exists
		baseConfig := &corev1.ConfigMap{}
		Eventually(func(g Gomega, ctx SpecContext) {
			err := env.Client.Get(ctx, types.NamespacedName{
				Name:      fmt.Sprintf("%s-base", name),
				Namespace: namespace,
			}, baseConfig)
			g.Expect(err).ToNot(HaveOccurred())
			g.Expect(baseConfig.Data["prefix"]).To(Equal("test"))
			g.Expect(baseConfig.Data["itemCount"]).To(Equal("2"))
		}, 20*time.Second, time.Second).WithContext(ctx).Should(Succeed())

		// Verify level 1 ConfigMaps (should be 2: a, b)
		for _, item := range []string{"a", "b"} {
			cm := &corev1.ConfigMap{}
			Eventually(func(g Gomega, ctx SpecContext) {
				err := env.Client.Get(ctx, types.NamespacedName{
					Name:      fmt.Sprintf("%s-l1-%s", name, item),
					Namespace: namespace,
				}, cm)
				g.Expect(err).ToNot(HaveOccurred())
				g.Expect(cm.Data["entry"]).To(Equal(item))
				g.Expect(cm.Data["prefix"]).To(Equal("test"))
				g.Expect(cm.Data["level"]).To(Equal("1"))
			}, 20*time.Second, time.Second).WithContext(ctx).Should(Succeed())
		}

		// Verify level 2 ConfigMaps (should be 2: derived from l1)
		for _, item := range []string{"a", "b"} {
			cm := &corev1.ConfigMap{}
			Eventually(func(g Gomega, ctx SpecContext) {
				err := env.Client.Get(ctx, types.NamespacedName{
					Name:      fmt.Sprintf("%s-l2-%s", name, item),
					Namespace: namespace,
				}, cm)
				g.Expect(err).ToNot(HaveOccurred())
				g.Expect(cm.Data["sourceEntry"]).To(Equal(item))
				g.Expect(cm.Data["sourcePrefix"]).To(Equal("test"))
				g.Expect(cm.Data["level"]).To(Equal("2"))
			}, 20*time.Second, time.Second).WithContext(ctx).Should(Succeed())
		}

		// Verify summary config
		summaryConfig := &corev1.ConfigMap{}
		Eventually(func(g Gomega, ctx SpecContext) {
			err := env.Client.Get(ctx, types.NamespacedName{
				Name:      fmt.Sprintf("%s-summary", name),
				Namespace: namespace,
			}, summaryConfig)
			g.Expect(err).ToNot(HaveOccurred())
			g.Expect(summaryConfig.Data["level1Count"]).To(Equal("2"))
			g.Expect(summaryConfig.Data["level2Count"]).To(Equal("2"))
		}, 20*time.Second, time.Second).WithContext(ctx).Should(Succeed())

		// Verify final pods are created (should be 2: derived from l2)
		for _, item := range []string{"a", "b"} {
			pod := &corev1.Pod{}
			Eventually(func(g Gomega, ctx SpecContext) {
				err := env.Client.Get(ctx, types.NamespacedName{
					Name:      fmt.Sprintf("%s-pod-%s", name, item),
					Namespace: namespace,
				}, pod)
				g.Expect(err).ToNot(HaveOccurred())
				g.Expect(pod.Spec.Containers[0].Name).To(Equal("worker"))
			}, 30*time.Second, time.Second).WithContext(ctx).Should(Succeed())
		}

		// SCALE UP: Update instance to add item "c"
		Eventually(func(g Gomega, ctx SpecContext) {
			err := env.Client.Get(ctx, types.NamespacedName{
				Name:      name,
				Namespace: namespace,
			}, instance)
			g.Expect(err).ToNot(HaveOccurred())

			// Update items to [a, b, c]
			err = unstructured.SetNestedSlice(instance.Object, []interface{}{"a", "b", "c"}, "spec", "items")
			g.Expect(err).ToNot(HaveOccurred())

			err = env.Client.Update(ctx, instance)
			g.Expect(err).ToNot(HaveOccurred())
		}, 10*time.Second, time.Second).WithContext(ctx).Should(Succeed())

		// Wait for new level 1 and level 2 ConfigMaps for "c"
		Eventually(func(g Gomega, ctx SpecContext) {
			cm := &corev1.ConfigMap{}
			err := env.Client.Get(ctx, types.NamespacedName{
				Name:      fmt.Sprintf("%s-l1-%s", name, "c"),
				Namespace: namespace,
			}, cm)
			g.Expect(err).ToNot(HaveOccurred())
			g.Expect(cm.Data["entry"]).To(Equal("c"))
		}, 30*time.Second, time.Second).WithContext(ctx).Should(Succeed())

		Eventually(func(g Gomega, ctx SpecContext) {
			cm := &corev1.ConfigMap{}
			err := env.Client.Get(ctx, types.NamespacedName{
				Name:      fmt.Sprintf("%s-l2-%s", name, "c"),
				Namespace: namespace,
			}, cm)
			g.Expect(err).ToNot(HaveOccurred())
			g.Expect(cm.Data["sourceEntry"]).To(Equal("c"))
		}, 30*time.Second, time.Second).WithContext(ctx).Should(Succeed())

		// Verify summary config updated with new counts
		Eventually(func(g Gomega, ctx SpecContext) {
			err := env.Client.Get(ctx, types.NamespacedName{
				Name:      fmt.Sprintf("%s-summary", name),
				Namespace: namespace,
			}, summaryConfig)
			g.Expect(err).ToNot(HaveOccurred())
			g.Expect(summaryConfig.Data["level1Count"]).To(Equal("3"))
			g.Expect(summaryConfig.Data["level2Count"]).To(Equal("3"))
		}, 30*time.Second, time.Second).WithContext(ctx).Should(Succeed())

		// Verify ALL 3 final pods exist after scale up (a, b, c)
		for _, item := range []string{"a", "b", "c"} {
			Eventually(func(g Gomega, ctx SpecContext) {
				pod := &corev1.Pod{}
				err := env.Client.Get(ctx, types.NamespacedName{
					Name:      fmt.Sprintf("%s-pod-%s", name, item),
					Namespace: namespace,
				}, pod)
				g.Expect(err).ToNot(HaveOccurred())
				g.Expect(pod.Spec.Containers[0].Name).To(Equal("worker"))
			}, 30*time.Second, time.Second).WithContext(ctx).Should(Succeed())
		}

		// SCALE DOWN: Update instance to remove item "b"
		Eventually(func(g Gomega, ctx SpecContext) {
			err := env.Client.Get(ctx, types.NamespacedName{
				Name:      name,
				Namespace: namespace,
			}, instance)
			g.Expect(err).ToNot(HaveOccurred())

			// Update items to [a, c] (remove b)
			err = unstructured.SetNestedSlice(instance.Object, []interface{}{"a", "c"}, "spec", "items")
			g.Expect(err).ToNot(HaveOccurred())

			err = env.Client.Update(ctx, instance)
			g.Expect(err).ToNot(HaveOccurred())
		}, 10*time.Second, time.Second).WithContext(ctx).Should(Succeed())

		// Wait for "b" ConfigMaps to be deleted (pruned by applyset)
		Eventually(func(g Gomega, ctx SpecContext) {
			cm := &corev1.ConfigMap{}
			err := env.Client.Get(ctx, types.NamespacedName{
				Name:      fmt.Sprintf("%s-l1-%s", name, "b"),
				Namespace: namespace,
			}, cm)
			g.Expect(err).To(MatchError(errors.IsNotFound, "l1-b should be deleted"))
		}, 30*time.Second, time.Second).WithContext(ctx).Should(Succeed())

		Eventually(func(g Gomega, ctx SpecContext) {
			cm := &corev1.ConfigMap{}
			err := env.Client.Get(ctx, types.NamespacedName{
				Name:      fmt.Sprintf("%s-l2-%s", name, "b"),
				Namespace: namespace,
			}, cm)
			g.Expect(err).To(MatchError(errors.IsNotFound, "l2-b should be deleted"))
		}, 30*time.Second, time.Second).WithContext(ctx).Should(Succeed())

		// Wait for pod "b" to be deleted (pruned by applyset)
		Eventually(func(g Gomega, ctx SpecContext) {
			pod := &corev1.Pod{}
			err := env.Client.Get(ctx, types.NamespacedName{
				Name:      fmt.Sprintf("%s-pod-%s", name, "b"),
				Namespace: namespace,
			}, pod)
			g.Expect(err).To(MatchError(errors.IsNotFound, "pod-b should be deleted"))
		}, 30*time.Second, time.Second).WithContext(ctx).Should(Succeed())

		// Verify summary config updated with new counts
		Eventually(func(g Gomega, ctx SpecContext) {
			err := env.Client.Get(ctx, types.NamespacedName{
				Name:      fmt.Sprintf("%s-summary", name),
				Namespace: namespace,
			}, summaryConfig)
			g.Expect(err).ToNot(HaveOccurred())
			g.Expect(summaryConfig.Data["level1Count"]).To(Equal("2"))
			g.Expect(summaryConfig.Data["level2Count"]).To(Equal("2"))
		}, 30*time.Second, time.Second).WithContext(ctx).Should(Succeed())

		// Verify remaining ConfigMaps still exist with correct data
		for _, item := range []string{"a", "c"} {
			cm := &corev1.ConfigMap{}
			err := env.Client.Get(ctx, types.NamespacedName{
				Name:      fmt.Sprintf("%s-l1-%s", name, item),
				Namespace: namespace,
			}, cm)
			Expect(err).ToNot(HaveOccurred())
			Expect(cm.Data["entry"]).To(Equal(item))
		}

		// Verify remaining pods still exist
		for _, item := range []string{"a", "c"} {
			pod := &corev1.Pod{}
			err := env.Client.Get(ctx, types.NamespacedName{
				Name:      fmt.Sprintf("%s-pod-%s", name, item),
				Namespace: namespace,
			}, pod)
			Expect(err).ToNot(HaveOccurred())
			Expect(pod.Spec.Containers[0].Name).To(Equal("worker"))
		}

		// Cleanup
		Expect(env.Client.Delete(ctx, instance)).To(Succeed())
		Eventually(func(g Gomega, ctx SpecContext) {
			err := env.Client.Get(ctx, types.NamespacedName{
				Name:      name,
				Namespace: namespace,
			}, instance)
			g.Expect(err).To(MatchError(errors.IsNotFound, "instance should be deleted"))
		}, 30*time.Second, time.Second).WithContext(ctx).Should(Succeed())

		Expect(env.Client.Delete(ctx, rgd)).To(Succeed())
		Eventually(func(g Gomega, ctx SpecContext) {
			err := env.Client.Get(ctx, types.NamespacedName{
				Name: rgd.Name,
			}, &krov1alpha1.ResourceGraphDefinition{})
			g.Expect(err).To(MatchError(errors.IsNotFound, "rgd should be deleted"))
		}, 20*time.Second, time.Second).WithContext(ctx).Should(Succeed())
	})

	// Test: Collection readyWhen blocks dependent resources in the DAG
	// This verifies that when a collection has a readyWhen expression, dependent
	// resources are NOT created until ALL collection items satisfy the readyWhen condition.
	// Pattern: Worker Pods (collection) -> Coordinator ConfigMap (depends on all workers Running)
	It("should block dependent resources until collection readyWhen is satisfied", func(ctx SpecContext) {
		rgd := generator.NewResourceGraphDefinition("test-collection-dag-blocking",
			generator.WithSchema(
				"WorkerCluster", "v1alpha1",
				map[string]interface{}{
					"name":    "string",
					"workers": "[]string",
				},
				nil,
			),
			// Collection of worker pods - each worker name gets a Pod
			// readyWhen requires ALL pods to have status.phase == "Running"
			generator.WithResourceCollection("workerPods", map[string]interface{}{
				"apiVersion": "v1",
				"kind":       "Pod",
				"metadata": map[string]interface{}{
					"name": "${schema.spec.name}-worker-${worker}",
				},
				"spec": map[string]interface{}{
					"restartPolicy": "Never",
					"containers": []interface{}{
						map[string]interface{}{
							"name":    "worker",
							"image":   "busybox:latest",
							"command": []interface{}{"sh", "-c", "sleep 3600"},
						},
					},
				},
			},
				[]krov1alpha1.ForEachDimension{
					{"worker": "${schema.spec.workers}"},
				},
				// readyWhen: each worker pod must be Running (AND semantics across all items)
				[]string{"${each.status.phase == 'Running'}"},
				nil),
			// Coordinator ConfigMap depends on workers - should NOT be created until all workers are Running
			generator.WithResource("coordinator", map[string]interface{}{
				"apiVersion": "v1",
				"kind":       "ConfigMap",
				"metadata": map[string]interface{}{
					"name": "${schema.spec.name}-coordinator",
				},
				"data": map[string]interface{}{
					// Reference worker pods to create dependency
					"workerCount":  "${string(size(workerPods))}",
					"firstWorker":  "${workerPods[0].metadata.name}",
					"allWorkerIPs": "${workerPods.map(w, has(w.status.podIP) ? w.status.podIP : 'pending').join(',')}",
				},
			}, nil, nil),
		)

		// Create ResourceGraphDefinition
		Expect(env.Client.Create(ctx, rgd)).To(Succeed())

		// Verify ResourceGraphDefinition becomes ready
		createdRGD := &krov1alpha1.ResourceGraphDefinition{}
		Eventually(func(g Gomega, ctx SpecContext) {
			err := env.Client.Get(ctx, types.NamespacedName{
				Name: rgd.Name,
			}, createdRGD)
			g.Expect(err).ToNot(HaveOccurred())
			g.Expect(createdRGD.Status.State).To(Equal(krov1alpha1.ResourceGraphDefinitionStateActive))
		}, 10*time.Second, time.Second).WithContext(ctx).Should(Succeed())

		// Create instance with 3 workers
		name := "test-dag"
		workers := []string{"alpha", "beta", "gamma"}
		instance := &unstructured.Unstructured{
			Object: map[string]interface{}{
				"apiVersion": fmt.Sprintf("%s/%s", krov1alpha1.KRODomainName, "v1alpha1"),
				"kind":       "WorkerCluster",
				"metadata": map[string]interface{}{
					"name":      name,
					"namespace": namespace,
				},
				"spec": map[string]interface{}{
					"name":    name,
					"workers": []interface{}{"alpha", "beta", "gamma"},
				},
			},
		}
		Expect(env.Client.Create(ctx, instance)).To(Succeed())

		// Wait for worker Pods to be created
		for _, worker := range workers {
			Eventually(func(g Gomega, ctx SpecContext) {
				pod := &corev1.Pod{}
				err := env.Client.Get(ctx, types.NamespacedName{
					Name:      fmt.Sprintf("%s-worker-%s", name, worker),
					Namespace: namespace,
				}, pod)
				g.Expect(err).ToNot(HaveOccurred())
			}, 30*time.Second, time.Second).WithContext(ctx).Should(Succeed())
		}

		// Verify instance is IN_PROGRESS (workers not ready yet)
		Eventually(func(g Gomega, ctx SpecContext) {
			err := env.Client.Get(ctx, types.NamespacedName{
				Name:      name,
				Namespace: namespace,
			}, instance)
			g.Expect(err).ToNot(HaveOccurred())
			status, found, err := unstructured.NestedString(instance.Object, "status", "state")
			g.Expect(err).ToNot(HaveOccurred())
			g.Expect(found).To(BeTrue())
			g.Expect(status).To(Equal("IN_PROGRESS"))
		}, 20*time.Second, time.Second).WithContext(ctx).Should(Succeed())

		// Verify coordinator ConfigMap is NOT created yet (workers not Running)
		coordinatorName := fmt.Sprintf("%s-coordinator", name)
		Consistently(func(g Gomega, ctx SpecContext) {
			cm := &corev1.ConfigMap{}
			err := env.Client.Get(ctx, types.NamespacedName{
				Name:      coordinatorName,
				Namespace: namespace,
			}, cm)
			g.Expect(errors.IsNotFound(err)).To(BeTrue(), "coordinator should NOT be created while workers are not Running")
		}, 5*time.Second, time.Second).WithContext(ctx).Should(Succeed())

		// Patch only 2 of 3 worker pods to Running - coordinator should still not be created
		for _, worker := range workers[:2] {
			pod := &corev1.Pod{}
			err := env.Client.Get(ctx, types.NamespacedName{
				Name:      fmt.Sprintf("%s-worker-%s", name, worker),
				Namespace: namespace,
			}, pod)
			Expect(err).ToNot(HaveOccurred())
			pod.Status.Phase = corev1.PodRunning
			pod.Status.PodIP = fmt.Sprintf("10.0.0.%d", len(worker))
			Expect(env.Client.Status().Update(ctx, pod)).To(Succeed())
		}

		// Verify coordinator still not created (only 2/3 workers Running)
		Consistently(func(g Gomega, ctx SpecContext) {
			cm := &corev1.ConfigMap{}
			err := env.Client.Get(ctx, types.NamespacedName{
				Name:      coordinatorName,
				Namespace: namespace,
			}, cm)
			g.Expect(errors.IsNotFound(err)).To(BeTrue(), "coordinator should NOT be created until ALL workers are Running")
		}, 5*time.Second, time.Second).WithContext(ctx).Should(Succeed())

		// Now patch the last worker pod to Running
		pod := &corev1.Pod{}
		err := env.Client.Get(ctx, types.NamespacedName{
			Name:      fmt.Sprintf("%s-worker-%s", name, workers[2]),
			Namespace: namespace,
		}, pod)
		Expect(err).ToNot(HaveOccurred())
		pod.Status.Phase = corev1.PodRunning
		pod.Status.PodIP = "10.0.0.99"
		Expect(env.Client.Status().Update(ctx, pod)).To(Succeed())

		// Now coordinator ConfigMap should be created
		Eventually(func(g Gomega, ctx SpecContext) {
			cm := &corev1.ConfigMap{}
			err := env.Client.Get(ctx, types.NamespacedName{
				Name:      coordinatorName,
				Namespace: namespace,
			}, cm)
			g.Expect(err).ToNot(HaveOccurred())
			g.Expect(cm.Data["workerCount"]).To(Equal("3"))
			// Verify it has worker pod names
			g.Expect(cm.Data["firstWorker"]).To(ContainSubstring("worker-alpha"))
		}, 30*time.Second, time.Second).WithContext(ctx).Should(Succeed())

		// Verify instance is now ACTIVE
		Eventually(func(g Gomega, ctx SpecContext) {
			err := env.Client.Get(ctx, types.NamespacedName{
				Name:      name,
				Namespace: namespace,
			}, instance)
			g.Expect(err).ToNot(HaveOccurred())
			status, found, err := unstructured.NestedString(instance.Object, "status", "state")
			g.Expect(err).ToNot(HaveOccurred())
			g.Expect(found).To(BeTrue())
			g.Expect(status).To(Equal("ACTIVE"))
		}, 20*time.Second, time.Second).WithContext(ctx).Should(Succeed())

		// Cleanup
		Expect(env.Client.Delete(ctx, instance)).To(Succeed())
		Eventually(func(g Gomega, ctx SpecContext) {
			err := env.Client.Get(ctx, types.NamespacedName{
				Name:      name,
				Namespace: namespace,
			}, instance)
			g.Expect(err).To(MatchError(errors.IsNotFound, "instance should be deleted"))
		}, 20*time.Second, time.Second).WithContext(ctx).Should(Succeed())

		Expect(env.Client.Delete(ctx, rgd)).To(Succeed())
		Eventually(func(g Gomega, ctx SpecContext) {
			err := env.Client.Get(ctx, types.NamespacedName{
				Name: rgd.Name,
			}, &krov1alpha1.ResourceGraphDefinition{})
			g.Expect(err).To(MatchError(errors.IsNotFound, "rgd should be deleted"))
		}, 20*time.Second, time.Second).WithContext(ctx).Should(Succeed())
	})

	// Test: readyWhen with `each` keyword for per-item checks in collections
	// Verifies that collections can use `each` to check per-item readiness:
	// - ${each.data.ready == 'true'}   // Per-item check, AND semantics across all items
	It("should evaluate readyWhen per-item expressions with each keyword", func(ctx SpecContext) {
		rgd := generator.NewResourceGraphDefinition("test-readywhen-collection",
			generator.WithSchema(
				"ReadyWhenCollection", "v1alpha1",
				map[string]interface{}{
					"name":   "string",
					"values": "[]string",
				},
				nil,
			),
			generator.WithResourceCollection("configs", map[string]interface{}{
				"apiVersion": "v1",
				"kind":       "ConfigMap",
				"metadata": map[string]interface{}{
					"name": "${schema.spec.name}-${value}",
				},
				"data": map[string]interface{}{
					"key":   "${value}",
					"ready": "true",
				},
			},
				[]krov1alpha1.ForEachDimension{
					{"value": "${schema.spec.values}"},
				},
				// readyWhen with `each` - all ConfigMaps must have data.ready == "true"
				[]string{"${each.data.ready == 'true'}"},
				nil),
		)

		// Create ResourceGraphDefinition
		Expect(env.Client.Create(ctx, rgd)).To(Succeed())

		// Verify ResourceGraphDefinition becomes ready
		createdRGD := &krov1alpha1.ResourceGraphDefinition{}
		Eventually(func(g Gomega, ctx SpecContext) {
			err := env.Client.Get(ctx, types.NamespacedName{
				Name: rgd.Name,
			}, createdRGD)
			g.Expect(err).ToNot(HaveOccurred())
			g.Expect(createdRGD.Status.State).To(Equal(krov1alpha1.ResourceGraphDefinitionStateActive))
		}, 10*time.Second, time.Second).WithContext(ctx).Should(Succeed())

		// Create instance with 3 values
		name := "test-ready"
		instance := &unstructured.Unstructured{
			Object: map[string]interface{}{
				"apiVersion": fmt.Sprintf("%s/%s", krov1alpha1.KRODomainName, "v1alpha1"),
				"kind":       "ReadyWhenCollection",
				"metadata": map[string]interface{}{
					"name":      name,
					"namespace": namespace,
				},
				"spec": map[string]interface{}{
					"name":   name,
					"values": []interface{}{"one", "two", "three"},
				},
			},
		}
		Expect(env.Client.Create(ctx, instance)).To(Succeed())

		// Wait for instance to become ACTIVE
		// This verifies the readyWhen aggregate expression was evaluated
		Eventually(func(g Gomega, ctx SpecContext) {
			err := env.Client.Get(ctx, types.NamespacedName{
				Name:      name,
				Namespace: namespace,
			}, instance)
			g.Expect(err).ToNot(HaveOccurred())
			val, found, err := unstructured.NestedString(instance.Object, "status", "state")
			g.Expect(err).ToNot(HaveOccurred())
			g.Expect(found).To(BeTrue())
			g.Expect(val).To(Equal("ACTIVE"))
		}, 30*time.Second, time.Second).WithContext(ctx).Should(Succeed())

		// Verify all 3 ConfigMaps are created with the expected data
		for _, value := range []string{"one", "two", "three"} {
			cm := &corev1.ConfigMap{}
			Eventually(func(g Gomega, ctx SpecContext) {
				err := env.Client.Get(ctx, types.NamespacedName{
					Name:      fmt.Sprintf("%s-%s", name, value),
					Namespace: namespace,
				}, cm)
				g.Expect(err).ToNot(HaveOccurred())
				g.Expect(cm.Data["key"]).To(Equal(value))
				g.Expect(cm.Data["ready"]).To(Equal("true"))
			}, 20*time.Second, time.Second).WithContext(ctx).Should(Succeed())
		}

		// Cleanup
		Expect(env.Client.Delete(ctx, instance)).To(Succeed())
		Eventually(func(g Gomega, ctx SpecContext) {
			err := env.Client.Get(ctx, types.NamespacedName{
				Name:      name,
				Namespace: namespace,
			}, instance)
			g.Expect(err).To(MatchError(errors.IsNotFound, "instance should be deleted"))
		}, 20*time.Second, time.Second).WithContext(ctx).Should(Succeed())

		Expect(env.Client.Delete(ctx, rgd)).To(Succeed())
		Eventually(func(g Gomega, ctx SpecContext) {
			err := env.Client.Get(ctx, types.NamespacedName{
				Name: rgd.Name,
			}, &krov1alpha1.ResourceGraphDefinition{})
			g.Expect(err).To(MatchError(errors.IsNotFound, "rgd should be deleted"))
		}, 20*time.Second, time.Second).WithContext(ctx).Should(Succeed())
	})

	// Test: Collection without readyWhen expressions
	// Verifies that collections work correctly without readyWhen - all items are considered ready
	It("should create collection resources without readyWhen expressions", func(ctx SpecContext) {
		rgd := generator.NewResourceGraphDefinition("test-no-readywhen-collection",
			generator.WithSchema(
				"NoReadyWhenCollection", "v1alpha1",
				map[string]interface{}{
					"name":   "string",
					"values": "[]string",
				},
				nil,
			),
			generator.WithResourceCollection("configs", map[string]interface{}{
				"apiVersion": "v1",
				"kind":       "ConfigMap",
				"metadata": map[string]interface{}{
					"name": "${schema.spec.name}-${value}",
				},
				"data": map[string]interface{}{
					"key": "${value}",
				},
			},
				[]krov1alpha1.ForEachDimension{
					{"value": "${schema.spec.values}"},
				},
				// No readyWhen - collection is ready once all items are created
				nil,
				nil),
		)

		// Create ResourceGraphDefinition
		Expect(env.Client.Create(ctx, rgd)).To(Succeed())

		// Verify ResourceGraphDefinition becomes ready
		createdRGD := &krov1alpha1.ResourceGraphDefinition{}
		Eventually(func(g Gomega, ctx SpecContext) {
			err := env.Client.Get(ctx, types.NamespacedName{
				Name: rgd.Name,
			}, createdRGD)
			g.Expect(err).ToNot(HaveOccurred())
			g.Expect(createdRGD.Status.State).To(Equal(krov1alpha1.ResourceGraphDefinitionStateActive))
		}, 10*time.Second, time.Second).WithContext(ctx).Should(Succeed())

		// Create instance with 3 values
		name := "test-no-ready"
		instance := &unstructured.Unstructured{
			Object: map[string]interface{}{
				"apiVersion": fmt.Sprintf("%s/%s", krov1alpha1.KRODomainName, "v1alpha1"),
				"kind":       "NoReadyWhenCollection",
				"metadata": map[string]interface{}{
					"name":      name,
					"namespace": namespace,
				},
				"spec": map[string]interface{}{
					"name":   name,
					"values": []interface{}{"a", "b", "c"},
				},
			},
		}
		Expect(env.Client.Create(ctx, instance)).To(Succeed())

		// Wait for instance to become ACTIVE
		Eventually(func(g Gomega, ctx SpecContext) {
			err := env.Client.Get(ctx, types.NamespacedName{
				Name:      name,
				Namespace: namespace,
			}, instance)
			g.Expect(err).ToNot(HaveOccurred())
			val, found, err := unstructured.NestedString(instance.Object, "status", "state")
			g.Expect(err).ToNot(HaveOccurred())
			g.Expect(found).To(BeTrue())
			g.Expect(val).To(Equal("ACTIVE"))
		}, 30*time.Second, time.Second).WithContext(ctx).Should(Succeed())

		// Verify all 3 ConfigMaps are created
		for _, value := range []string{"a", "b", "c"} {
			cm := &corev1.ConfigMap{}
			Eventually(func(g Gomega, ctx SpecContext) {
				err := env.Client.Get(ctx, types.NamespacedName{
					Name:      fmt.Sprintf("%s-%s", name, value),
					Namespace: namespace,
				}, cm)
				g.Expect(err).ToNot(HaveOccurred())
				g.Expect(cm.Data["key"]).To(Equal(value))
			}, 20*time.Second, time.Second).WithContext(ctx).Should(Succeed())
		}

		// Cleanup
		Expect(env.Client.Delete(ctx, instance)).To(Succeed())
		Eventually(func(g Gomega, ctx SpecContext) {
			err := env.Client.Get(ctx, types.NamespacedName{
				Name:      name,
				Namespace: namespace,
			}, instance)
			g.Expect(err).To(MatchError(errors.IsNotFound, "instance should be deleted"))
		}, 20*time.Second, time.Second).WithContext(ctx).Should(Succeed())

		Expect(env.Client.Delete(ctx, rgd)).To(Succeed())
		Eventually(func(g Gomega, ctx SpecContext) {
			err := env.Client.Get(ctx, types.NamespacedName{
				Name: rgd.Name,
			}, &krov1alpha1.ResourceGraphDefinition{})
			g.Expect(err).To(MatchError(errors.IsNotFound, "rgd should be deleted"))
		}, 20*time.Second, time.Second).WithContext(ctx).Should(Succeed())
	})

	// Test: Collection resource cleanup on instance deletion
	// Verifies that when an instance is deleted, ALL collection resources are also deleted.
	// This tests the deletion code path in the instance controller.
	It("should delete all collection resources when instance is deleted", func(ctx SpecContext) {
		rgd := generator.NewResourceGraphDefinition("test-deletion-collection",
			generator.WithSchema(
				"DeletionTest", "v1alpha1",
				map[string]interface{}{
					"name":   "string",
					"values": "[]string",
				},
				nil,
			),
			generator.WithResourceCollection("configs", map[string]interface{}{
				"apiVersion": "v1",
				"kind":       "ConfigMap",
				"metadata": map[string]interface{}{
					"name": "${schema.spec.name}-${value}",
				},
				"data": map[string]interface{}{
					"key": "${value}",
				},
			},
				[]krov1alpha1.ForEachDimension{
					{"value": "${schema.spec.values}"},
				},
				nil, nil),
		)

		// Create ResourceGraphDefinition
		Expect(env.Client.Create(ctx, rgd)).To(Succeed())

		// Verify ResourceGraphDefinition becomes ready
		createdRGD := &krov1alpha1.ResourceGraphDefinition{}
		Eventually(func(g Gomega, ctx SpecContext) {
			err := env.Client.Get(ctx, types.NamespacedName{
				Name: rgd.Name,
			}, createdRGD)
			g.Expect(err).ToNot(HaveOccurred())
			g.Expect(createdRGD.Status.State).To(Equal(krov1alpha1.ResourceGraphDefinitionStateActive))
		}, 10*time.Second, time.Second).WithContext(ctx).Should(Succeed())

		// Create instance with 3 values
		name := "test-deletion"
		values := []string{"one", "two", "three"}
		instance := &unstructured.Unstructured{
			Object: map[string]interface{}{
				"apiVersion": fmt.Sprintf("%s/%s", krov1alpha1.KRODomainName, "v1alpha1"),
				"kind":       "DeletionTest",
				"metadata": map[string]interface{}{
					"name":      name,
					"namespace": namespace,
				},
				"spec": map[string]interface{}{
					"name":   name,
					"values": []interface{}{"one", "two", "three"},
				},
			},
		}
		Expect(env.Client.Create(ctx, instance)).To(Succeed())

		// Wait for instance to become ACTIVE
		Eventually(func(g Gomega, ctx SpecContext) {
			err := env.Client.Get(ctx, types.NamespacedName{
				Name:      name,
				Namespace: namespace,
			}, instance)
			g.Expect(err).ToNot(HaveOccurred())
			val, found, err := unstructured.NestedString(instance.Object, "status", "state")
			g.Expect(err).ToNot(HaveOccurred())
			g.Expect(found).To(BeTrue())
			g.Expect(val).To(Equal("ACTIVE"))
		}, 30*time.Second, time.Second).WithContext(ctx).Should(Succeed())

		// Verify all 3 ConfigMaps are created
		for _, value := range values {
			cm := &corev1.ConfigMap{}
			Eventually(func(g Gomega, ctx SpecContext) {
				err := env.Client.Get(ctx, types.NamespacedName{
					Name:      fmt.Sprintf("%s-%s", name, value),
					Namespace: namespace,
				}, cm)
				g.Expect(err).ToNot(HaveOccurred())
				g.Expect(cm.Data["key"]).To(Equal(value))
			}, 20*time.Second, time.Second).WithContext(ctx).Should(Succeed())
		}

		// Delete the instance
		Expect(env.Client.Delete(ctx, instance)).To(Succeed())

		// Verify instance is deleted
		Eventually(func(g Gomega, ctx SpecContext) {
			err := env.Client.Get(ctx, types.NamespacedName{
				Name:      name,
				Namespace: namespace,
			}, instance)
			g.Expect(err).To(MatchError(errors.IsNotFound, "instance should be deleted"))
		}, 30*time.Second, time.Second).WithContext(ctx).Should(Succeed())

		// CRITICAL: Verify ALL collection ConfigMaps are also deleted
		// This is the key assertion - collection resources must be cleaned up
		for _, value := range values {
			Eventually(func(g Gomega, ctx SpecContext) {
				cm := &corev1.ConfigMap{}
				err := env.Client.Get(ctx, types.NamespacedName{
					Name:      fmt.Sprintf("%s-%s", name, value),
					Namespace: namespace,
				}, cm)
				g.Expect(err).To(MatchError(errors.IsNotFound, fmt.Sprintf("ConfigMap %s-%s should be deleted", name, value)))
			}, 30*time.Second, time.Second).WithContext(ctx).Should(Succeed())
		}

		// Cleanup RGD
		Expect(env.Client.Delete(ctx, rgd)).To(Succeed())
		Eventually(func(g Gomega, ctx SpecContext) {
			err := env.Client.Get(ctx, types.NamespacedName{
				Name: rgd.Name,
			}, &krov1alpha1.ResourceGraphDefinition{})
			g.Expect(err).To(MatchError(errors.IsNotFound, "rgd should be deleted"))
		}, 20*time.Second, time.Second).WithContext(ctx).Should(Succeed())
	})

	// Test: Drift detection for collection resources
	// Verifies that when a collection resource is manually modified (drift),
	// kro detects and restores it to the desired state.
	It("should detect and restore drift in collection resources", func(ctx SpecContext) {
		rgd := generator.NewResourceGraphDefinition("test-drift-collection",
			generator.WithSchema(
				"DriftTest", "v1alpha1",
				map[string]interface{}{
					"name":   "string",
					"items":  "[]string",
					"prefix": "string",
				},
				nil,
			),
			generator.WithResourceCollection("configs", map[string]interface{}{
				"apiVersion": "v1",
				"kind":       "ConfigMap",
				"metadata": map[string]interface{}{
					"name": "${schema.spec.name}-${entry}",
				},
				"data": map[string]interface{}{
					"entry":  "${entry}",
					"prefix": "${schema.spec.prefix}",
					"static": "unchanged",
				},
			},
				[]krov1alpha1.ForEachDimension{
					{"entry": "${schema.spec.items}"},
				},
				nil, nil),
		)

		// Create ResourceGraphDefinition
		Expect(env.Client.Create(ctx, rgd)).To(Succeed())

		// Wait for RGD to become active
		Eventually(func(g Gomega, ctx SpecContext) {
			err := env.Client.Get(ctx, types.NamespacedName{
				Name: rgd.Name,
			}, rgd)
			g.Expect(err).ToNot(HaveOccurred())
			g.Expect(rgd.Status.State).To(Equal(krov1alpha1.ResourceGraphDefinitionStateActive))
		}, 20*time.Second, time.Second).WithContext(ctx).Should(Succeed())

		namespace := fmt.Sprintf("test-%s", rand.String(5))
		ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: namespace}}
		Expect(env.Client.Create(ctx, ns)).To(Succeed())

		name := "test-drift"
		instance := &unstructured.Unstructured{
			Object: map[string]interface{}{
				"apiVersion": fmt.Sprintf("%s/%s", krov1alpha1.KRODomainName, "v1alpha1"),
				"kind":       "DriftTest",
				"metadata": map[string]interface{}{
					"name":      name,
					"namespace": namespace,
				},
				"spec": map[string]interface{}{
					"name":   name,
					"items":  []interface{}{"alpha", "beta"},
					"prefix": "original",
				},
			},
		}
		Expect(env.Client.Create(ctx, instance)).To(Succeed())

		// Wait for ConfigMaps to be created
		for _, item := range []string{"alpha", "beta"} {
			Eventually(func(g Gomega, ctx SpecContext) {
				cm := &corev1.ConfigMap{}
				err := env.Client.Get(ctx, types.NamespacedName{
					Name:      fmt.Sprintf("%s-%s", name, item),
					Namespace: namespace,
				}, cm)
				g.Expect(err).ToNot(HaveOccurred())
				g.Expect(cm.Data["entry"]).To(Equal(item))
				g.Expect(cm.Data["prefix"]).To(Equal("original"))
				g.Expect(cm.Data["static"]).To(Equal("unchanged"))
			}, 20*time.Second, time.Second).WithContext(ctx).Should(Succeed())
		}

		// Now simulate drift by modifying one of the ConfigMaps
		alphaCM := &corev1.ConfigMap{}
		err := env.Client.Get(ctx, types.NamespacedName{
			Name:      fmt.Sprintf("%s-alpha", name),
			Namespace: namespace,
		}, alphaCM)
		Expect(err).ToNot(HaveOccurred())

		// Modify the ConfigMap data (drift)
		alphaCM.Data["prefix"] = "DRIFTED"
		alphaCM.Data["extra"] = "unexpected"
		Expect(env.Client.Update(ctx, alphaCM)).To(Succeed())

		// Verify kro restores the ConfigMap to its desired state
		// Note: Server-side apply only restores managed fields, it won't remove
		// fields added by other controllers (the "extra" field stays).
		Eventually(func(g Gomega, ctx SpecContext) {
			cm := &corev1.ConfigMap{}
			err := env.Client.Get(ctx, types.NamespacedName{
				Name:      fmt.Sprintf("%s-alpha", name),
				Namespace: namespace,
			}, cm)
			g.Expect(err).ToNot(HaveOccurred())
			// Managed fields should be restored to original values
			g.Expect(cm.Data["prefix"]).To(Equal("original"))
			g.Expect(cm.Data["entry"]).To(Equal("alpha"))
			g.Expect(cm.Data["static"]).To(Equal("unchanged"))
		}, 30*time.Second, time.Second).WithContext(ctx).Should(Succeed())

		// Cleanup
		Expect(env.Client.Delete(ctx, instance)).To(Succeed())
		Eventually(func(g Gomega, ctx SpecContext) {
			err := env.Client.Get(ctx, types.NamespacedName{
				Name:      name,
				Namespace: namespace,
			}, instance)
			g.Expect(err).To(MatchError(errors.IsNotFound, "instance should be deleted"))
		}, 30*time.Second, time.Second).WithContext(ctx).Should(Succeed())

		Expect(env.Client.Delete(ctx, rgd)).To(Succeed())
		Eventually(func(g Gomega, ctx SpecContext) {
			err := env.Client.Get(ctx, types.NamespacedName{
				Name: rgd.Name,
			}, &krov1alpha1.ResourceGraphDefinition{})
			g.Expect(err).To(MatchError(errors.IsNotFound, "rgd should be deleted"))
		}, 20*time.Second, time.Second).WithContext(ctx).Should(Succeed())
	})

	// Test: Collection readyWhen WITHOUT dependent resources
	// This specifically tests that the instance stays IN_PROGRESS when a collection's
	// readyWhen is not satisfied, even when there are NO downstream dependencies.
	// This is critical because without dependents, there's no other trigger to
	// keep reconciliation going - the collection readiness must drive the requeue.
	It("should keep instance IN_PROGRESS until collection readyWhen is satisfied (no dependents)", func(ctx SpecContext) {
		rgd := generator.NewResourceGraphDefinition("test-collection-readywhen-no-deps",
			generator.WithSchema(
				"StandaloneCollection", "v1alpha1",
				map[string]interface{}{
					"name":      "string",
					"values":    "[]string",
					"makeReady": "boolean | default=false", // Controls whether collection items are "ready"
				},
				nil,
			),
			// Collection of ConfigMaps with readyWhen but NO dependent resources
			// The ready value is controlled by the instance spec, not external updates
			generator.WithResourceCollection("configs", map[string]interface{}{
				"apiVersion": "v1",
				"kind":       "ConfigMap",
				"metadata": map[string]interface{}{
					"name": "${schema.spec.name}-${value}",
				},
				"data": map[string]interface{}{
					"key":   "${value}",
					"ready": "${string(schema.spec.makeReady)}", // Controlled by instance spec
				},
			},
				[]krov1alpha1.ForEachDimension{
					{"value": "${schema.spec.values}"},
				},
				// readyWhen: each ConfigMap must have data.ready == "true"
				[]string{"${each.data.ready == 'true'}"},
				nil),
			// NO dependent resources - this is the key difference from other tests
		)

		// Create ResourceGraphDefinition
		Expect(env.Client.Create(ctx, rgd)).To(Succeed())

		// Verify ResourceGraphDefinition becomes ready
		createdRGD := &krov1alpha1.ResourceGraphDefinition{}
		Eventually(func(g Gomega, ctx SpecContext) {
			err := env.Client.Get(ctx, types.NamespacedName{
				Name: rgd.Name,
			}, createdRGD)
			g.Expect(err).ToNot(HaveOccurred())
			g.Expect(createdRGD.Status.State).To(Equal(krov1alpha1.ResourceGraphDefinitionStateActive))
		}, 10*time.Second, time.Second).WithContext(ctx).Should(Succeed())

		// Create instance with 3 values - initially NOT ready (makeReady=false)
		name := "test-standalone"
		instance := &unstructured.Unstructured{
			Object: map[string]interface{}{
				"apiVersion": fmt.Sprintf("%s/%s", krov1alpha1.KRODomainName, "v1alpha1"),
				"kind":       "StandaloneCollection",
				"metadata": map[string]interface{}{
					"name":      name,
					"namespace": namespace,
				},
				"spec": map[string]interface{}{
					"name":      name,
					"values":    []interface{}{"alpha", "beta", "gamma"},
					"makeReady": false, // Initially not ready
				},
			},
		}
		Expect(env.Client.Create(ctx, instance)).To(Succeed())

		// Wait for ConfigMaps to be created with ready=false
		for _, value := range []string{"alpha", "beta", "gamma"} {
			Eventually(func(g Gomega, ctx SpecContext) {
				cm := &corev1.ConfigMap{}
				err := env.Client.Get(ctx, types.NamespacedName{
					Name:      fmt.Sprintf("%s-%s", name, value),
					Namespace: namespace,
				}, cm)
				g.Expect(err).ToNot(HaveOccurred())
				g.Expect(cm.Data["key"]).To(Equal(value))
				g.Expect(cm.Data["ready"]).To(Equal("false"))
			}, 30*time.Second, time.Second).WithContext(ctx).Should(Succeed())
		}

		// Verify instance is IN_PROGRESS (collection items not ready yet)
		// This is the key assertion - without the fix, instance would be ACTIVE
		Eventually(func(g Gomega, ctx SpecContext) {
			err := env.Client.Get(ctx, types.NamespacedName{
				Name:      name,
				Namespace: namespace,
			}, instance)
			g.Expect(err).ToNot(HaveOccurred())
			status, found, err := unstructured.NestedString(instance.Object, "status", "state")
			g.Expect(err).ToNot(HaveOccurred())
			g.Expect(found).To(BeTrue())
			g.Expect(status).To(Equal("IN_PROGRESS"))
		}, 20*time.Second, time.Second).WithContext(ctx).Should(Succeed())

		// Verify instance stays IN_PROGRESS while collection items are not ready
		Consistently(func(g Gomega, ctx SpecContext) {
			err := env.Client.Get(ctx, types.NamespacedName{
				Name:      name,
				Namespace: namespace,
			}, instance)
			g.Expect(err).ToNot(HaveOccurred())
			status, _, _ := unstructured.NestedString(instance.Object, "status", "state")
			g.Expect(status).To(Equal("IN_PROGRESS"), "instance should stay IN_PROGRESS while collection items are not ready")
		}, 5*time.Second, time.Second).WithContext(ctx).Should(Succeed())

		// Now update the instance spec to make all ConfigMaps ready
		err := env.Client.Get(ctx, types.NamespacedName{
			Name:      name,
			Namespace: namespace,
		}, instance)
		Expect(err).ToNot(HaveOccurred())
		unstructured.SetNestedField(instance.Object, true, "spec", "makeReady")
		Expect(env.Client.Update(ctx, instance)).To(Succeed())

		// Verify all ConfigMaps are updated to ready=true
		for _, value := range []string{"alpha", "beta", "gamma"} {
			Eventually(func(g Gomega, ctx SpecContext) {
				cm := &corev1.ConfigMap{}
				err := env.Client.Get(ctx, types.NamespacedName{
					Name:      fmt.Sprintf("%s-%s", name, value),
					Namespace: namespace,
				}, cm)
				g.Expect(err).ToNot(HaveOccurred())
				g.Expect(cm.Data["ready"]).To(Equal("true"))
			}, 30*time.Second, time.Second).WithContext(ctx).Should(Succeed())
		}

		// Now instance should become ACTIVE
		Eventually(func(g Gomega, ctx SpecContext) {
			err := env.Client.Get(ctx, types.NamespacedName{
				Name:      name,
				Namespace: namespace,
			}, instance)
			g.Expect(err).ToNot(HaveOccurred())
			status, found, err := unstructured.NestedString(instance.Object, "status", "state")
			g.Expect(err).ToNot(HaveOccurred())
			g.Expect(found).To(BeTrue())
			g.Expect(status).To(Equal("ACTIVE"))
		}, 30*time.Second, time.Second).WithContext(ctx).Should(Succeed())

		// Cleanup
		Expect(env.Client.Delete(ctx, instance)).To(Succeed())
		Eventually(func(g Gomega, ctx SpecContext) {
			err := env.Client.Get(ctx, types.NamespacedName{
				Name:      name,
				Namespace: namespace,
			}, instance)
			g.Expect(err).To(MatchError(errors.IsNotFound, "instance should be deleted"))
		}, 20*time.Second, time.Second).WithContext(ctx).Should(Succeed())

		Expect(env.Client.Delete(ctx, rgd)).To(Succeed())
		Eventually(func(g Gomega, ctx SpecContext) {
			err := env.Client.Get(ctx, types.NamespacedName{
				Name: rgd.Name,
			}, &krov1alpha1.ResourceGraphDefinition{})
			g.Expect(err).To(MatchError(errors.IsNotFound, "rgd should be deleted"))
		}, 20*time.Second, time.Second).WithContext(ctx).Should(Succeed())
	})
})
