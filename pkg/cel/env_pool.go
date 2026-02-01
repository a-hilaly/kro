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

package cel

import (
	"fmt"
	"slices"
	"strings"

	"github.com/google/cel-go/cel"
	"github.com/google/cel-go/common/types"
	apiservercel "k8s.io/apiserver/pkg/cel"
	"k8s.io/apiserver/pkg/cel/openapi"
	"k8s.io/kube-openapi/pkg/validation/spec"
)

// EnvPool constructs minimal, reusable CEL environments for a specific RGD.
//
// The inspector detects which identifiers each expression references (e.g.,
// "schema", "vpc"). EnvPool then creates environments declaring exactly those
// identifiers, ensuring compile-time validation matches runtime: if an
// expression references an undeclared identifier, compilation fails.
//
// Architecture:
//   - Base environment: CEL extensions (strings, lists, etc.) + shared type
//     provider (all schemas registered) + no variable declarations
//   - Extended environments: Base + variable declarations for specific references
//     (e.g., env with "schema" declared, env with "schema,vpc" declared)
//
// Environments are cached by sorted reference names, so expressions with the
// same references share an environment. Future: could be reused across RGDs
// that reference similar resource GVKs.
//
// Thread safety: EnvPool is NOT thread-safe (build-time use only).
// The cel.Env and compiled cel.Programs ARE thread-safe for concurrent evaluation.
type EnvPool struct {
	// declTypes maps identifiers to their CEL DeclTypes.
	// Computed once from schemas during construction.
	declTypes map[string]*apiservercel.DeclType

	// baseEnv has extensions and type provider, but no variable declarations.
	baseEnv *cel.Env

	// envs caches environments by sorted reference names.
	envs map[string]*cel.Env
}

// NewEnvPool creates an EnvPool from the given schemas.
//
// The schemas map keys are identifiers that expressions may reference
// (e.g., "schema", "vpc", "deployment"). Values are OpenAPI schemas
// used for CEL type checking.
func NewEnvPool(schemas map[string]*spec.Schema) (*EnvPool, error) {
	// Convert schemas to CEL DeclTypes
	declTypes := make(map[string]*apiservercel.DeclType, len(schemas))
	allDeclTypes := make([]*apiservercel.DeclType, 0, len(schemas))

	for name, schema := range schemas {
		declType := SchemaDeclTypeWithMetadata(&openapi.Schema{Schema: schema}, false)
		if declType != nil {
			typeName := TypeNamePrefix + name
			declType = declType.MaybeAssignTypeName(typeName)
			declTypes[name] = declType
			allDeclTypes = append(allDeclTypes, declType)
		}
	}

	// Build base environment with extensions and shared type provider
	baseDeclarations := BaseDeclarations()

	if len(allDeclTypes) > 0 {
		sharedProvider := NewDeclTypeProvider(allDeclTypes...)
		sharedProvider.SetRecognizeKeywordAsFieldName(true)

		registry := types.NewEmptyRegistry()
		wrappedProvider, err := sharedProvider.WithTypeProvider(registry)
		if err != nil {
			return nil, fmt.Errorf("create type provider: %w", err)
		}
		baseDeclarations = append(baseDeclarations, cel.CustomTypeProvider(wrappedProvider))
	}

	baseEnv, err := cel.NewEnv(baseDeclarations...)
	if err != nil {
		return nil, fmt.Errorf("create base environment: %w", err)
	}

	return &EnvPool{
		declTypes: declTypes,
		baseEnv:   baseEnv,
		envs:      make(map[string]*cel.Env),
	}, nil
}

// GetOrCreate returns a CEL environment declaring the given references and extra types.
//
// The returned environment can parse, type-check, and compile expressions that
// use those references. Base environments (without extraTypes) are cached, so
// repeated calls with the same references return the same environment.
//
// extraTypes allows declaring additional variables (e.g., iterator variables from
// forEach) that aren't in the schema set. These are filtered from references and
// added via Extend(). Environments with extraTypes are NOT cached to avoid
// conflicts when the same variable name has different types across nodes.
//
// Returns an error if any reference (after filtering extraTypes) is unknown.
func (s *EnvPool) GetOrCreate(references []string, extraTypes map[string]*cel.Type) (*cel.Env, error) {
	// Filter extraTypes keys from references
	var schemaRefs []string
	for _, ref := range references {
		if _, isExtra := extraTypes[ref]; !isExtra {
			schemaRefs = append(schemaRefs, ref)
		}
	}

	// Get/create cached base env for schema references
	key := s.cacheKey(schemaRefs)
	baseEnv, ok := s.envs[key]
	if !ok {
		var err error
		baseEnv, err = s.extendBaseEnv(schemaRefs)
		if err != nil {
			return nil, err
		}
		s.envs[key] = baseEnv
	}

	// Extend with extra types if any (not cached)
	if len(extraTypes) > 0 {
		var decls []cel.EnvOption
		for name, celType := range extraTypes {
			decls = append(decls, cel.Variable(name, celType))
		}
		return baseEnv.Extend(decls...)
	}

	return baseEnv, nil
}

// extendBaseEnv creates a new environment with the given references declared.
func (s *EnvPool) extendBaseEnv(references []string) (*cel.Env, error) {
	if len(references) == 0 {
		return s.baseEnv, nil
	}

	declarations := make([]cel.EnvOption, 0, len(references))
	for _, ref := range references {
		dt, ok := s.declTypes[ref]
		if !ok {
			return nil, fmt.Errorf("unknown reference %q: not in schema set", ref)
		}
		declarations = append(declarations, cel.Variable(ref, dt.CelType()))
	}

	return s.baseEnv.Extend(declarations...)
}

// cacheKey normalizes references into a cache key.
func (s *EnvPool) cacheKey(references []string) string {
	sorted := slices.Clone(references)
	slices.Sort(sorted)
	return strings.Join(sorted, ",")
}

// Size returns the number of cached environments.
func (s *EnvPool) Size() int {
	return len(s.envs)
}
