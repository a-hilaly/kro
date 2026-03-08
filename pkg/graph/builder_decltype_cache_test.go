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

	"github.com/google/cel-go/cel"
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

	env, provider, _, err := buildTypedEnvironmentWithCache(map[string]typedSchemaInput{
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

func TestDeclTypeBuildCache_ReusesUnnamedDeclTreeAcrossTypeNames(t *testing.T) {
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
							"name": {SchemaProps: spec.SchemaProps{Type: []string{"string"}}},
						},
					},
				},
			},
		},
	}

	cache := newDeclTypeBuildCache()
	left := cache.namedDeclType(sharedSchema, krocel.TypeNamePrefix+"type_left")
	right := cache.namedDeclType(sharedSchema, krocel.TypeNamePrefix+"type_right")

	require.NotNil(t, left)
	require.NotNil(t, right)
	assert.Equal(t, 1, callCount, "the raw DeclType tree should only be built once per schema within a build even when named multiple ways")
	assert.NotSame(t, left, right, "distinct type names should still get their own named DeclType wrappers")
}

func TestCompiledExpressionBuildCache_ReusesProgramsForSameExpressionAndEnv(t *testing.T) {
	original := buildCELProgram
	compileCount := 0
	buildCELProgram = func(env *cel.Env, checkedAST *cel.Ast) (cel.Program, error) {
		compileCount++
		return original(env, checkedAST)
	}
	defer func() {
		buildCELProgram = original
	}()

	sharedSchema := &spec.Schema{
		SchemaProps: spec.SchemaProps{
			Type: []string{"object"},
			Properties: map[string]spec.Schema{
				"spec": {
					SchemaProps: spec.SchemaProps{
						Type: []string{"object"},
						Properties: map[string]spec.Schema{
							"name": {SchemaProps: spec.SchemaProps{Type: []string{"string"}}},
						},
					},
				},
			},
		},
	}

	declTypeCache := newDeclTypeBuildCache()
	env, _, envSignature, err := buildTypedEnvironmentWithCache(map[string]typedSchemaInput{
		"schema": {
			schema:       sharedSchema,
			rootTypeName: fallbackRootTypeName("schema"),
		},
	}, declTypeCache)
	require.NoError(t, err)

	compileCache := newCompiledExpressionBuildCache()
	left := krocel.NewUncompiled("schema.spec.name")
	right := krocel.NewUncompiled("schema.spec.name")

	leftAST, err := parseCheckAndCompile(env, envSignature, left, compileCache)
	require.NoError(t, err)
	rightAST, err := parseCheckAndCompile(env, envSignature, right, compileCache)
	require.NoError(t, err)

	assert.Equal(t, 1, compileCount, "identical expressions in the same typed environment should only compile once per build")
	assert.Same(t, leftAST, rightAST, "cached compilation should reuse the checked AST")

	extendedEnv, err := env.Extend(cel.Variable("item", cel.IntType))
	require.NoError(t, err)
	extendedSignature := compileEnvironmentSignature(envSignature, map[string]*cel.Type{"item": cel.IntType})
	third := krocel.NewUncompiled("schema.spec.name")
	_, err = parseCheckAndCompile(extendedEnv, extendedSignature, third, compileCache)
	require.NoError(t, err)

	assert.Equal(t, 2, compileCount, "changing the typed environment signature should force a distinct compilation")
}
