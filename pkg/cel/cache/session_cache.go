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
	"fmt"
	"sync"

	"github.com/google/cel-go/cel"
	"k8s.io/kube-openapi/pkg/validation/spec"
)

// SessionCache caches per-build CEL artifacts and short-lived extended
// environments. It is created fresh per NewResourceGraphDefinition() call and
// discarded afterward.
//
// When builderCache is set, ParseAndCheck / ParseCheckAndCompile delegate to the
// long-lived BuilderCache so identical expressions can be reused across RGDs.
// Extended environments remain scoped to the current build.
type SessionCache struct {
	builderCache *BuilderCache
	checkedASTs  sync.Map // key: ProgramCacheKey, value: *cel.Ast
	programs     sync.Map // key: ProgramCacheKey, value: *ProgramCacheEntry
	extendedEnvs sync.Map // key: extendedEnvCacheKey, value: *cel.Env
}

// NewSessionCache returns a fresh SessionCache instance.
func NewSessionCache() *SessionCache {
	return &SessionCache{}
}

// NewSessionCacheWithBuilder returns a SessionCache that delegates program/AST
// caching to the long-lived BuilderCache.
func NewSessionCacheWithBuilder(builderCache *BuilderCache) *SessionCache {
	return &SessionCache{builderCache: builderCache}
}

// ParseAndCheck parses and type-checks a CEL expression without compiling
// a program. The checked AST is cached for later reuse by ParseCheckAndCompile.
func (c *SessionCache) ParseAndCheck(env *cel.Env, expr string) (*cel.Ast, error) {
	if c.builderCache != nil {
		return c.builderCache.ParseAndCheck(env, expr)
	}

	key := ProgramCacheKey{Expr: expr, Env: env}

	// Check if we already have a full program cached — reuse its AST.
	if v, ok := c.programs.Load(key); ok {
		sessionCacheHitsTotal.WithLabelValues("checked_ast").Inc()
		entry := v.(*ProgramCacheEntry)
		return entry.Ast, nil
	}

	// Check the AST-only cache.
	if v, ok := c.checkedASTs.Load(key); ok {
		sessionCacheHitsTotal.WithLabelValues("checked_ast").Inc()
		return v.(*cel.Ast), nil
	}

	sessionCacheMissesTotal.WithLabelValues("checked_ast").Inc()

	parsedAST, issues := env.Parse(expr)
	if issues != nil && issues.Err() != nil {
		return nil, issues.Err()
	}

	checkedAST, issues := env.Check(parsedAST)
	if issues != nil && issues.Err() != nil {
		return nil, issues.Err()
	}

	c.checkedASTs.Store(key, checkedAST)
	return checkedAST, nil
}

// ParseCheckAndCompile returns a cached compiled program and checked AST
// for the given expression and environment. On cache miss, it parses,
// type-checks, and compiles the expression, then stores the result.
// If a checked AST was previously cached by ParseAndCheck, it is reused
// to skip the parse and check phases.
func (c *SessionCache) ParseCheckAndCompile(env *cel.Env, expr string) (cel.Program, *cel.Ast, error) {
	if c.builderCache != nil {
		return c.builderCache.ParseCheckAndCompile(env, expr)
	}

	key := ProgramCacheKey{Expr: expr, Env: env}
	if v, ok := c.programs.Load(key); ok {
		sessionCacheHitsTotal.WithLabelValues("program").Inc()
		entry := v.(*ProgramCacheEntry)
		return entry.Program, entry.Ast, nil
	}

	sessionCacheMissesTotal.WithLabelValues("program").Inc()

	// Try to reuse a previously cached checked AST.
	var checkedAST *cel.Ast
	if v, ok := c.checkedASTs.Load(key); ok {
		sessionCacheASTReuseTotal.Inc()
		checkedAST = v.(*cel.Ast)
	} else {
		parsedAST, issues := env.Parse(expr)
		if issues != nil && issues.Err() != nil {
			return nil, nil, issues.Err()
		}

		var checkIssues *cel.Issues
		checkedAST, checkIssues = env.Check(parsedAST)
		if checkIssues != nil && checkIssues.Err() != nil {
			return nil, nil, checkIssues.Err()
		}
	}

	program, err := env.Program(checkedAST)
	if err != nil {
		return nil, nil, fmt.Errorf("compile: %w", err)
	}

	c.programs.Store(key, &ProgramCacheEntry{Program: program, Ast: checkedAST})
	return program, checkedAST, nil
}

// ExtendWithTypedVar returns a cached environment that extends the parent
// with a single typed variable declaration derived from the given schema.
// On cache miss, the create callback is called to build the extended env.
// The callback pattern avoids importing pkg/cel (for DeclTypeProvider etc.).
func (c *SessionCache) ExtendWithTypedVar(parent *cel.Env, varName string, schema *spec.Schema, create func() (*cel.Env, error)) (*cel.Env, error) {
	key := extendedEnvCacheKey{parentEnv: parent, varName: varName, schema: schema}
	if v, ok := c.extendedEnvs.Load(key); ok {
		sessionCacheHitsTotal.WithLabelValues("extended_env").Inc()
		return v.(*cel.Env), nil
	}

	sessionCacheMissesTotal.WithLabelValues("extended_env").Inc()

	extended, err := create()
	if err != nil {
		return nil, err
	}

	c.extendedEnvs.Store(key, extended)
	return extended, nil
}
