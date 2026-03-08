// Copyright 2026 The Kubernetes Authors.
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
	apiservercel "k8s.io/apiserver/pkg/cel"
	"k8s.io/apiserver/pkg/cel/common"
	"k8s.io/kube-openapi/pkg/validation/spec"

	krocel "github.com/kubernetes-sigs/kro/pkg/cel"
	"github.com/kubernetes-sigs/kro/pkg/graph/variable"
)

func TestDeclTypeBuildCache_ReusesTypeFamilyPerGVRWithinSingleBuild(t *testing.T) {
	original := schemaDeclTypeWithMetadata
	callCount := 0
	schemaDeclTypeWithMetadata = func(s common.Schema, isResourceRoot bool) *apiservercel.DeclType {
		callCount++
		return original(s, isResourceRoot)
	}
	defer func() {
		schemaDeclTypeWithMetadata = original
	}()

	sharedSchema := &spec.Schema{
		SchemaProps: spec.SchemaProps{
			Type: []string{"object"},
			Properties: map[string]spec.Schema{
				"spec": {
					SchemaProps: spec.SchemaProps{
						Type: []string{"object"},
						Properties: map[string]spec.Schema{
							"replicas": {SchemaProps: spec.SchemaProps{Type: []string{"integer"}}},
						},
					},
				},
			},
		},
	}

	cache := newDeclTypeBuildCache()
	deploymentRootType := krocel.TypeNamePrefix + "gvr_apps_v1_deployments"

	env, provider, err := buildTypedEnvironmentWithCache(map[string]typedSchemaInput{
		"left": {
			schema:       sharedSchema,
			rootTypeName: deploymentRootType,
		},
		"right": {
			schema:       sharedSchema,
			rootTypeName: deploymentRootType,
		},
	}, cache)
	require.NoError(t, err)
	require.NotNil(t, env)
	require.NotNil(t, provider)

	leftType := getExpectedTypeForField(&variable.FieldDescriptor{
		Path:                 "spec",
		StandaloneExpression: true,
	}, sharedSchema, deploymentRootType, provider, cache)
	rightType := getExpectedTypeForField(&variable.FieldDescriptor{
		Path:                 "spec",
		StandaloneExpression: true,
	}, sharedSchema, deploymentRootType, provider, cache)
	require.NotNil(t, leftType)
	require.NotNil(t, rightType)

	assert.Equal(t, 1, callCount, "the shared schema should only be converted to a DeclType once per GVR root type within a build")
	assert.Len(t, provider.TypeNames(), 2, "shared GVRs should register one root object and one nested object type family")
	assert.Equal(t, leftType.String(), rightType.String(), "identical GVR schemas should share the same CEL type family")
}
