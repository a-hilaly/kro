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

// EnvSet provides scoped CEL environments for expression compilation.
//
// Each expression in an RGD references a specific set of identifiers (e.g.,
// "schema", "vpc", "deployment"). EnvSet creates CEL environments that declare
// exactly those identifiers, ensuring compile-time validation matches runtime:
// if an expression references an undeclared identifier, compilation fails.
//
// Architecture:
//
//	┌─────────────────────────────────────────────────────────────────┐
//	│  Base Environment                                               │
//	│  - CEL extensions (strings, lists, etc.)                        │
//	│  - Shared type provider (all schemas registered)                │
//	│  - No variable declarations                                     │
//	└─────────────────────────────────────────────────────────────────┘
//	                              │
//	           ┌──────────────────┼──────────────────┐
//	           ▼                  ▼                  ▼
//	   ┌──────────────┐   ┌──────────────┐   ┌──────────────┐
//	   │ Env: schema  │   │ Env: schema, │   │ Env: vpc,    │
//	   │              │   │      vpc     │   │      config  │
//	   └──────────────┘   └──────────────┘   └──────────────┘
//	   (includeWhen)      (template expr)    (template expr)
//
// Environments are cached by their reference set, so expressions with the same
// references share an environment.
//
// Thread safety: EnvSet is NOT thread-safe (single-threaded build-time use).
// The cel.Env and compiled cel.Programs ARE thread-safe for concurrent use.
type EnvSet struct {
	// declTypes maps identifiers to their CEL DeclTypes.
	// Computed once from schemas during construction.
	declTypes map[string]*apiservercel.DeclType

	// baseEnv has extensions and type provider, but no variable declarations.
	baseEnv *cel.Env

	// envs caches environments by sorted reference names.
	envs map[string]*cel.Env
}

// NewEnvSet creates an EnvSet from the given schemas.
//
// The schemas map keys are identifiers that expressions may reference
// (e.g., "schema", "vpc", "deployment"). Values are OpenAPI schemas
// used for CEL type checking.
func NewEnvSet(schemas map[string]*spec.Schema) (*EnvSet, error) {
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

	return &EnvSet{
		declTypes: declTypes,
		baseEnv:   baseEnv,
		envs:      make(map[string]*cel.Env),
	}, nil
}

// GetOrCreate returns a CEL environment declaring exactly the given references.
//
// The returned environment can parse, type-check, and compile expressions that
// use those references. Environments are cached, so repeated calls with the
// same references return the same environment.
//
// Returns an error if any reference is unknown (not in the schemas passed to
// NewEnvSet).
func (s *EnvSet) GetOrCreate(references []string) (*cel.Env, error) {
	key := s.cacheKey(references)
	if env, ok := s.envs[key]; ok {
		return env, nil
	}

	env, err := s.extendBaseEnv(references)
	if err != nil {
		return nil, err
	}

	s.envs[key] = env
	return env, nil
}

// extendBaseEnv creates a new environment with the given references declared.
func (s *EnvSet) extendBaseEnv(references []string) (*cel.Env, error) {
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
func (s *EnvSet) cacheKey(references []string) string {
	sorted := slices.Clone(references)
	slices.Sort(sorted)
	return strings.Join(sorted, ",")
}

// Size returns the number of cached environments.
func (s *EnvSet) Size() int {
	return len(s.envs)
}
