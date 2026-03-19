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

package cache

import (
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/cel-go/cel"
	apiservercel "k8s.io/apiserver/pkg/cel"
	"k8s.io/kube-openapi/pkg/validation/spec"
)

// BuilderCache caches cross-RGD CEL artifacts: DeclTypes, named types,
// typed environments, and field type maps. Long-lived, scoped to a Builder
// instance so caches persist across reconciles but are GC-able when the
// Builder is replaced.
type BuilderCache struct {
	declTypes     sync.Map // key: *spec.Schema, value: *apiservercel.DeclType
	namedTypes    sync.Map // key: namedTypeCacheKey, value: *apiservercel.DeclType
	typedEnvs     sync.Map // key: string, value: *TypedEnvEntry
	fieldTypeMaps sync.Map // key: *apiservercel.DeclType, value: map[string]*apiservercel.DeclType
	checkedASTs   sync.Map // key: ProgramCacheKey, value: *cel.Ast
	programs      sync.Map // key: ProgramCacheKey, value: *ProgramCacheEntry

	declTypeCount     atomic.Int64
	namedTypeCount    atomic.Int64
	typedEnvCount     atomic.Int64
	fieldTypeMapCount atomic.Int64
	checkedASTCount   atomic.Int64
	programCount      atomic.Int64
}

// NewBuilderCache returns a fresh BuilderCache instance.
func NewBuilderCache() *BuilderCache {
	return &BuilderCache{}
}

// SchemaDeclType returns a cached DeclType for the given schema pointer.
// On cache miss, the create callback is called to produce the DeclType.
// The create callback receives the schema and should return the DeclType
// (typically via SchemaDeclTypeWithMetadata).
func (c *BuilderCache) SchemaDeclType(schema *spec.Schema, create func(*spec.Schema) *apiservercel.DeclType) *apiservercel.DeclType {
	if schema == nil {
		return nil
	}
	if v, ok := c.declTypes.Load(schema); ok {
		recordBuilderCacheHit(cacheDeclTypes)
		return v.(*apiservercel.DeclType)
	}
	recordBuilderCacheMiss(cacheDeclTypes)
	start := time.Now()
	declType := create(schema)
	builderCacheFillDuration.WithLabelValues(cacheDeclTypes).Observe(time.Since(start).Seconds())
	if declType != nil {
		if actual, loaded := c.declTypes.LoadOrStore(schema, declType); loaded {
			return actual.(*apiservercel.DeclType)
		}
		builderCacheEntries.WithLabelValues(cacheDeclTypes).Set(float64(c.declTypeCount.Add(1)))
	}
	return declType
}

// MaybeAssignTypeName returns a cached named DeclType for the given
// schema pointer and type name. On cache miss, it calls
// declType.MaybeAssignTypeName(typeName) and caches the result.
func (c *BuilderCache) MaybeAssignTypeName(schema *spec.Schema, declType *apiservercel.DeclType, typeName string) *apiservercel.DeclType {
	key := namedTypeCacheKey{schema: schema, name: typeName}
	if v, ok := c.namedTypes.Load(key); ok {
		recordBuilderCacheHit(cacheNamedTypes)
		return v.(*apiservercel.DeclType)
	}
	recordBuilderCacheMiss(cacheNamedTypes)
	start := time.Now()
	named := declType.MaybeAssignTypeName(typeName)
	builderCacheFillDuration.WithLabelValues(cacheNamedTypes).Observe(time.Since(start).Seconds())
	if actual, loaded := c.namedTypes.LoadOrStore(key, named); loaded {
		return actual.(*apiservercel.DeclType)
	}
	builderCacheEntries.WithLabelValues(cacheNamedTypes).Set(float64(c.namedTypeCount.Add(1)))
	return named
}

// TypedEnvironmentWithProvider creates a typed CEL environment and returns
// both the environment and an opaque provider (typically *DeclTypeProvider,
// stored as any to avoid importing pkg/cel). Results are cached by canonical
// schema set. On cache miss, the create callback is called.
func (c *BuilderCache) TypedEnvironmentWithProvider(schemas map[string]*spec.Schema, create func() (*cel.Env, any, error)) (*cel.Env, any, error) {
	if len(schemas) > 0 {
		key := MakeEnvCacheKey(schemas)
		if v, ok := c.typedEnvs.Load(key); ok {
			recordBuilderCacheHit(cacheTypedEnvs)
			entry := v.(*TypedEnvEntry)
			return entry.Env, entry.Provider, nil
		}
		recordBuilderCacheMiss(cacheTypedEnvs)
		start := time.Now()
		env, provider, err := create()
		builderCacheFillDuration.WithLabelValues(cacheTypedEnvs).Observe(time.Since(start).Seconds())
		if err != nil {
			recordBuilderCacheError(cacheTypedEnvs)
			return nil, nil, err
		}
		entry := &TypedEnvEntry{Env: env, Provider: provider}
		if actual, loaded := c.typedEnvs.LoadOrStore(key, entry); loaded {
			cached := actual.(*TypedEnvEntry)
			return cached.Env, cached.Provider, nil
		}
		builderCacheEntries.WithLabelValues(cacheTypedEnvs).Set(float64(c.typedEnvCount.Add(1)))
		return env, provider, nil
	}
	return create()
}

// FieldTypeMap returns a cached field type map for the given DeclType.
// On cache miss, the create callback is called to build the map.
func (c *BuilderCache) FieldTypeMap(t *apiservercel.DeclType, create func() map[string]*apiservercel.DeclType) map[string]*apiservercel.DeclType {
	if v, ok := c.fieldTypeMaps.Load(t); ok {
		recordBuilderCacheHit(cacheFieldTypeMaps)
		return v.(map[string]*apiservercel.DeclType)
	}
	recordBuilderCacheMiss(cacheFieldTypeMaps)
	start := time.Now()
	m := create()
	builderCacheFillDuration.WithLabelValues(cacheFieldTypeMaps).Observe(time.Since(start).Seconds())
	if actual, loaded := c.fieldTypeMaps.LoadOrStore(t, m); loaded {
		return actual.(map[string]*apiservercel.DeclType)
	}
	builderCacheEntries.WithLabelValues(cacheFieldTypeMaps).Set(float64(c.fieldTypeMapCount.Add(1)))
	return m
}

// ParseAndCheck parses and type-checks a CEL expression and caches the checked AST
// across RGD builds by (expression, environment).
func (c *BuilderCache) ParseAndCheck(env *cel.Env, expr string) (*cel.Ast, error) {
	key := ProgramCacheKey{Expr: expr, Env: env}

	if v, ok := c.programs.Load(key); ok {
		recordBuilderCacheHit(cacheCheckedASTs)
		return v.(*ProgramCacheEntry).Ast, nil
	}
	if v, ok := c.checkedASTs.Load(key); ok {
		recordBuilderCacheHit(cacheCheckedASTs)
		return v.(*cel.Ast), nil
	}

	recordBuilderCacheMiss(cacheCheckedASTs)
	start := time.Now()
	parsedAST, issues := env.Parse(expr)
	if issues != nil && issues.Err() != nil {
		recordBuilderCacheError(cacheCheckedASTs)
		return nil, issues.Err()
	}

	checkedAST, issues := env.Check(parsedAST)
	builderCacheFillDuration.WithLabelValues(cacheCheckedASTs).Observe(time.Since(start).Seconds())
	if issues != nil && issues.Err() != nil {
		recordBuilderCacheError(cacheCheckedASTs)
		return nil, issues.Err()
	}

	if actual, loaded := c.checkedASTs.LoadOrStore(key, checkedAST); loaded {
		return actual.(*cel.Ast), nil
	}
	builderCacheEntries.WithLabelValues(cacheCheckedASTs).Set(float64(c.checkedASTCount.Add(1)))
	return checkedAST, nil
}

// ParseCheckAndCompile returns a cached compiled program and checked AST for the
// given expression and environment, reusing shared builder cache entries across RGDs.
func (c *BuilderCache) ParseCheckAndCompile(env *cel.Env, expr string) (cel.Program, *cel.Ast, error) {
	key := ProgramCacheKey{Expr: expr, Env: env}
	if v, ok := c.programs.Load(key); ok {
		recordBuilderCacheHit(cachePrograms)
		entry := v.(*ProgramCacheEntry)
		return entry.Program, entry.Ast, nil
	}

	recordBuilderCacheMiss(cachePrograms)

	var checkedAST *cel.Ast
	if v, ok := c.checkedASTs.Load(key); ok {
		checkedAST = v.(*cel.Ast)
	} else {
		var err error
		checkedAST, err = c.ParseAndCheck(env, expr)
		if err != nil {
			recordBuilderCacheError(cachePrograms)
			return nil, nil, err
		}
	}

	start := time.Now()
	program, err := env.Program(checkedAST)
	builderCacheFillDuration.WithLabelValues(cachePrograms).Observe(time.Since(start).Seconds())
	if err != nil {
		recordBuilderCacheError(cachePrograms)
		return nil, nil, err
	}

	entry := &ProgramCacheEntry{Program: program, Ast: checkedAST}
	if actual, loaded := c.programs.LoadOrStore(key, entry); loaded {
		cached := actual.(*ProgramCacheEntry)
		return cached.Program, cached.Ast, nil
	}
	builderCacheEntries.WithLabelValues(cachePrograms).Set(float64(c.programCount.Add(1)))
	return program, checkedAST, nil
}
