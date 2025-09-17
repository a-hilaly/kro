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

package graph

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/client-go/discovery/cached/memory"
	"k8s.io/client-go/restmapper"

	krov1alpha1 "github.com/kubernetes-sigs/kro/api/v1alpha1"
	"github.com/kubernetes-sigs/kro/pkg/testutil/generator"
	"github.com/kubernetes-sigs/kro/pkg/testutil/k8s"
)

func TestGraphBuilder_CollectionChaining(t *testing.T) {
	fakeResolver, fakeDiscovery := k8s.NewFakeResolver()
	restMapper := restmapper.NewDeferredDiscoveryRESTMapper(memory.NewMemCacheClient(fakeDiscovery))
	builder := &Builder{
		schemaResolver: fakeResolver,
		restMapper:     restMapper,
	}

	tests := []struct {
		name                        string
		resourceGraphDefinitionOpts []generator.ResourceGraphDefinitionOption
		wantErr                     bool
		errMsg                      string
		checkGraph                  func(t *testing.T, g *Graph)
	}{
		{
			name: "collection with forEach referencing another resource (dynamic forEach)",
			resourceGraphDefinitionOpts: []generator.ResourceGraphDefinitionOption{
				generator.WithSchema(
					"CollectionChaining", "v1alpha1",
					map[string]interface{}{
						"name":       "string",
						"cidrBlocks": "[]string",
					},
					nil,
				),
				// First resource: a regular VPC
				generator.WithResource("vpc", map[string]interface{}{
					"apiVersion": "ec2.services.k8s.aws/v1alpha1",
					"kind":       "VPC",
					"metadata": map[string]interface{}{
						"name": "${schema.spec.name}-vpc",
					},
					"spec": map[string]interface{}{
						"cidrBlocks": []interface{}{"10.0.0.0/16"},
					},
				}, nil, nil),
				// Second resource: collection with forEach that references the first resource
				// The expression uses a ternary that checks vpc, making it a dynamic dependency
				generator.WithResourceCollection("chainedSubnets", map[string]interface{}{
					"apiVersion": "ec2.services.k8s.aws/v1alpha1",
					"kind":       "Subnet",
					"metadata": map[string]interface{}{
						"name": "${schema.spec.name}-${cidr}",
					},
					"spec": map[string]interface{}{
						"cidrBlock": "${cidr}",
						"vpcID":     "${vpc.status.vpcID}",
					},
				},
					[]krov1alpha1.ForEachDimension{
						// forEach expression references vpc (another resource)
						{"cidr": "${has(vpc.status.vpcID) ? schema.spec.cidrBlocks : []}"},
					},
					nil, nil),
			},
			wantErr: false,
			checkGraph: func(t *testing.T, g *Graph) {
				// Verify the collection resource depends on vpc
				chainedResource := g.Resources["chainedSubnets"]
				assert.NotNil(t, chainedResource)
				assert.True(t, chainedResource.IsCollection())
				assert.Contains(t, chainedResource.GetDependencies(), "vpc",
					"collection with forEach referencing vpc should have vpc as dependency")
			},
		},
		{
			name: "collection-to-collection chaining (forEach iterating over another collection)",
			resourceGraphDefinitionOpts: []generator.ResourceGraphDefinitionOption{
				generator.WithSchema(
					"CollectionToCollection", "v1alpha1",
					map[string]interface{}{
						"name":       "string",
						"cidrBlocks": "[]string",
					},
					nil,
				),
				// First collection: creates multiple subnets
				generator.WithResourceCollection("subnets", map[string]interface{}{
					"apiVersion": "ec2.services.k8s.aws/v1alpha1",
					"kind":       "Subnet",
					"metadata": map[string]interface{}{
						"name": "${schema.spec.name}-${cidr}",
					},
					"spec": map[string]interface{}{
						"cidrBlock": "${cidr}",
						"vpcID":     "vpc-123",
					},
				},
					[]krov1alpha1.ForEachDimension{
						{"cidr": "${schema.spec.cidrBlocks}"},
					},
					nil, nil),
				// Second collection: iterates over the first collection
				// ${subnets} is typed as list(Subnet) so we can iterate over it
				generator.WithResourceCollection("securityGroups", map[string]interface{}{
					"apiVersion": "ec2.services.k8s.aws/v1alpha1",
					"kind":       "SecurityGroup",
					"metadata": map[string]interface{}{
						"name": "${schema.spec.name}-sg-${subnet.metadata.name}",
					},
					"spec": map[string]interface{}{
						"description": "${subnet.status.subnetID}",
						"vpcID":       "vpc-123",
					},
				},
					[]krov1alpha1.ForEachDimension{
						// forEach iterates over another collection - ${subnets} returns list(Subnet)
						{"subnet": "${subnets}"},
					},
					nil, nil),
			},
			wantErr: false,
			checkGraph: func(t *testing.T, g *Graph) {
				// Verify first collection exists
				subnetsResource := g.Resources["subnets"]
				assert.NotNil(t, subnetsResource)
				assert.True(t, subnetsResource.IsCollection())

				// Verify second collection depends on first collection
				sgResource := g.Resources["securityGroups"]
				assert.NotNil(t, sgResource)
				assert.True(t, sgResource.IsCollection())
				assert.Contains(t, sgResource.GetDependencies(), "subnets",
					"securityGroups should depend on subnets collection")

				// Verify topological order: subnets before securityGroups
				subnetsIdx := -1
				sgIdx := -1
				for i, id := range g.TopologicalOrder {
					if id == "subnets" {
						subnetsIdx = i
					}
					if id == "securityGroups" {
						sgIdx = i
					}
				}
				assert.True(t, subnetsIdx < sgIdx,
					"subnets should come before securityGroups in topological order")
			},
		},
		{
			name: "collection with filter on another collection",
			resourceGraphDefinitionOpts: []generator.ResourceGraphDefinitionOption{
				generator.WithSchema(
					"FilteredCollection", "v1alpha1",
					map[string]interface{}{
						"name":       "string",
						"cidrBlocks": "[]string",
					},
					nil,
				),
				// First collection: creates multiple subnets
				generator.WithResourceCollection("subnets", map[string]interface{}{
					"apiVersion": "ec2.services.k8s.aws/v1alpha1",
					"kind":       "Subnet",
					"metadata": map[string]interface{}{
						"name": "${schema.spec.name}-${cidr}",
					},
					"spec": map[string]interface{}{
						"cidrBlock": "${cidr}",
						"vpcID":     "vpc-123",
					},
				},
					[]krov1alpha1.ForEachDimension{
						{"cidr": "${schema.spec.cidrBlocks}"},
					},
					nil, nil),
				// Second collection: uses filter() on the first collection
				generator.WithResourceCollection("filteredSecurityGroups", map[string]interface{}{
					"apiVersion": "ec2.services.k8s.aws/v1alpha1",
					"kind":       "SecurityGroup",
					"metadata": map[string]interface{}{
						"name": "${schema.spec.name}-sg-${subnet.metadata.name}",
					},
					"spec": map[string]interface{}{
						"description": "${subnet.status.subnetID}",
						"vpcID":       "vpc-123",
					},
				},
					[]krov1alpha1.ForEachDimension{
						// forEach uses filter() on collection - CEL list function on list(Subnet)
						{"subnet": "${subnets.filter(s, has(s.status.subnetID))}"},
					},
					nil, nil),
			},
			wantErr: false,
			checkGraph: func(t *testing.T, g *Graph) {
				// Verify second collection depends on first collection
				filteredResource := g.Resources["filteredSecurityGroups"]
				assert.NotNil(t, filteredResource)
				assert.True(t, filteredResource.IsCollection())
				assert.Contains(t, filteredResource.GetDependencies(), "subnets",
					"filteredSecurityGroups should depend on subnets collection")
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rgd := generator.NewResourceGraphDefinition("test-rgd", tt.resourceGraphDefinitionOpts...)
			graph, err := builder.NewResourceGraphDefinition(rgd)

			if tt.wantErr {
				require.Error(t, err)
				if tt.errMsg != "" {
					assert.Contains(t, err.Error(), tt.errMsg)
				}
				return
			}

			require.NoError(t, err)
			assert.NotNil(t, graph)

			if tt.checkGraph != nil {
				tt.checkGraph(t, graph)
			}
		})
	}
}

func TestGraphBuilder_CollectionValidation(t *testing.T) {
	fakeResolver, fakeDiscovery := k8s.NewFakeResolver()
	restMapper := restmapper.NewDeferredDiscoveryRESTMapper(memory.NewMemCacheClient(fakeDiscovery))
	builder := &Builder{
		schemaResolver: fakeResolver,
		restMapper:     restMapper,
	}

	tests := []struct {
		name                        string
		resourceGraphDefinitionOpts []generator.ResourceGraphDefinitionOption
		wantErr                     bool
		errMsg                      string
	}{
		{
			name: "valid collection with single iterator from schema list",
			resourceGraphDefinitionOpts: []generator.ResourceGraphDefinitionOption{
				generator.WithSchema(
					"MultiZoneVPC", "v1alpha1",
					map[string]interface{}{
						"name":       "string",
						"cidrBlocks": "[]string",
					},
					nil,
				),
				generator.WithResourceCollection("zonedSubnet", map[string]interface{}{
					"apiVersion": "ec2.services.k8s.aws/v1alpha1",
					"kind":       "Subnet",
					"metadata": map[string]interface{}{
						"name": "${schema.spec.name}-${cidr}",
					},
					"spec": map[string]interface{}{
						"cidrBlock": "${cidr}",
						"vpcID":     "vpc-123",
					},
				},
					[]krov1alpha1.ForEachDimension{
						{"cidr": "${schema.spec.cidrBlocks}"},
					},
					nil, nil),
			},
			wantErr: false,
		},
		{
			name: "valid collection with multiple iterators (cartesian product)",
			resourceGraphDefinitionOpts: []generator.ResourceGraphDefinitionOption{
				generator.WithSchema(
					"MultiRegionTierDeployment", "v1alpha1",
					map[string]interface{}{
						"name":       "string",
						"cidrBlocks": "[]string",
						"vpcIDs":     "[]string",
					},
					nil,
				),
				generator.WithResourceCollection("regionTierSubnet", map[string]interface{}{
					"apiVersion": "ec2.services.k8s.aws/v1alpha1",
					"kind":       "Subnet",
					"metadata": map[string]interface{}{
						"name": "${schema.spec.name}-${cidr}-${vpcID}",
					},
					"spec": map[string]interface{}{
						"cidrBlock": "${cidr}",
						"vpcID":     "${vpcID}",
					},
				},
					[]krov1alpha1.ForEachDimension{
						{"cidr": "${schema.spec.cidrBlocks}"},
						{"vpcID": "${schema.spec.vpcIDs}"},
					},
					nil, nil),
			},
			wantErr: false,
		},
		{
			name: "invalid collection - forEach expression does not return a list",
			resourceGraphDefinitionOpts: []generator.ResourceGraphDefinitionOption{
				generator.WithSchema(
					"InvalidCollection", "v1alpha1",
					map[string]interface{}{
						"name": "string",
					},
					nil,
				),
				generator.WithResourceCollection("badSubnet", map[string]interface{}{
					"apiVersion": "ec2.services.k8s.aws/v1alpha1",
					"kind":       "Subnet",
					"metadata": map[string]interface{}{
						"name": "${schema.spec.name}-${element}",
					},
					"spec": map[string]interface{}{
						"cidrBlock": "${element}",
						"vpcID":     "vpc-123",
					},
				},
					[]krov1alpha1.ForEachDimension{
						{"element": "${schema.spec.name}"}, // string, not a list
					},
					nil, nil),
			},
			wantErr: true,
			errMsg:  "must return a list",
		},
		{
			name: "collection with iterator variable used in template",
			resourceGraphDefinitionOpts: []generator.ResourceGraphDefinitionOption{
				generator.WithSchema(
					"WorkerPool", "v1alpha1",
					map[string]interface{}{
						"name":    "string",
						"workers": "[]string",
					},
					nil,
				),
				generator.WithResourceCollection("workerSubnet", map[string]interface{}{
					"apiVersion": "ec2.services.k8s.aws/v1alpha1",
					"kind":       "Subnet",
					"metadata": map[string]interface{}{
						"name": "${schema.spec.name}-${worker}",
					},
					"spec": map[string]interface{}{
						"cidrBlock": "${worker}",
						"vpcID":     "${schema.spec.name}",
					},
				},
					[]krov1alpha1.ForEachDimension{
						{"worker": "${schema.spec.workers}"},
					},
					nil, nil),
			},
			wantErr: false,
		},
		{
			name: "invalid collection - forEach iterator references another iterator",
			resourceGraphDefinitionOpts: []generator.ResourceGraphDefinitionOption{
				generator.WithSchema(
					"InvalidIteratorRef", "v1alpha1",
					map[string]interface{}{
						"name":  "string",
						"items": "[]string",
					},
					nil,
				),
				generator.WithResourceCollection("badPod", map[string]interface{}{
					"apiVersion": "v1",
					"kind":       "Pod",
					"metadata": map[string]interface{}{
						"name": "${schema.spec.name}-${element}-${derived}",
					},
					"spec": map[string]interface{}{
						"containers": []interface{}{
							map[string]interface{}{
								"name":  "main",
								"image": "nginx",
							},
						},
					},
				},
					[]krov1alpha1.ForEachDimension{
						{"element": "${schema.spec.items}"},
						// This references the 'element' iterator - not allowed
						{"derived": "${element}"},
					},
					nil, nil),
			},
			wantErr: true,
			errMsg:  "cannot reference other iterators",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rgd := generator.NewResourceGraphDefinition("test-rgd", tt.resourceGraphDefinitionOpts...)
			graph, err := builder.NewResourceGraphDefinition(rgd)

			if tt.wantErr {
				require.Error(t, err)
				if tt.errMsg != "" {
					assert.Contains(t, err.Error(), tt.errMsg)
				}
				return
			}

			require.NoError(t, err)
			assert.NotNil(t, graph)
		})
	}
}
