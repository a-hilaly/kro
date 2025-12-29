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
	"testing"

	"github.com/google/cel-go/cel"
	"github.com/google/cel-go/common/types/ref"
)

func TestGoNativeType(t *testing.T) {
	tests := []struct {
		name      string
		expr      string
		variables []cel.EnvOption
		input     map[string]interface{}
	}{
		// primitives
		{name: "bool", expr: "true"},
		{name: "int", expr: "42"},
		{name: "uint", expr: "42u"},
		{name: "double", expr: "3.14"},
		{name: "string", expr: `"hello"`},
		{name: "null", expr: "null"},

		// simple collections
		{name: "list", expr: "[1, 2, 3]"},
		{name: "map", expr: `{"a": 1, "b": 2}`},

		// nested collections
		{name: "nested_list", expr: "[[1, 2], [3, 4]]"},
		{name: "nested_map", expr: `{"a": {"b": {"c": 1}}}`},
		{name: "list_of_maps", expr: `[{"a": 1}, {"b": 2}]`},
		{name: "map_of_lists", expr: `{"x": [1, 2], "y": [3, 4]}`},
		{name: "mixed_nesting", expr: `{"list": [{"nested": [1, 2]}]}`},

		// dyn types
		{
			name:      "dyn_variable",
			expr:      "x",
			variables: []cel.EnvOption{cel.Variable("x", cel.DynType)},
			input:     map[string]interface{}{"x": map[string]interface{}{"nested": "value"}},
		},
		{
			name:      "dyn_list_element",
			expr:      "items[0]",
			variables: []cel.EnvOption{cel.Variable("items", cel.ListType(cel.DynType))},
			input:     map[string]interface{}{"items": []interface{}{map[string]interface{}{"a": 1}}},
		},
		{
			name:      "list_comprehension",
			expr:      `items.map(i, {"key": i})`,
			variables: []cel.EnvOption{cel.Variable("items", cel.ListType(cel.StringType))},
			input:     map[string]interface{}{"items": []string{"a", "b"}},
		},
		{
			name:      "nested_comprehension",
			expr:      `items.map(i, {"outer": {"inner": i}})`,
			variables: []cel.EnvOption{cel.Variable("items", cel.ListType(cel.StringType))},
			input:     map[string]interface{}{"items": []string{"a", "b"}},
		},
		{
			// https://github.com/kubernetes-sigs/kro/issues/907
			name:      "issue_907",
			expr:      `names.map(s, {"name": s, "valueFrom": {"secretKeyRef": {"name": secret, "key": s}}})`,
			variables: []cel.EnvOption{cel.Variable("names", cel.ListType(cel.StringType)), cel.Variable("secret", cel.StringType)},
			input:     map[string]interface{}{"names": []string{"a", "b"}, "secret": "my-secret"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			env, err := cel.NewEnv(tc.variables...)
			if err != nil {
				t.Fatalf("NewEnv: %v", err)
			}

			ast, issues := env.Compile(tc.expr)
			if issues != nil && issues.Err() != nil {
				t.Fatalf("Compile: %v", issues.Err())
			}

			program, err := env.Program(ast)
			if err != nil {
				t.Fatalf("Program: %v", err)
			}

			input := tc.input
			if input == nil {
				input = map[string]interface{}{}
			}

			result, _, err := program.Eval(input)
			if err != nil {
				t.Fatalf("Eval: %v", err)
			}

			native, err := GoNativeType(result)
			if err != nil {
				t.Fatalf("GoNativeType: %v", err)
			}

			assertNoRefVal(t, native)
		})
	}
}

func assertNoRefVal(t *testing.T, v interface{}) {
	t.Helper()
	if _, ok := v.(ref.Val); ok {
		t.Errorf("found unconverted ref.Val: %T", v)
		return
	}
	switch val := v.(type) {
	case []interface{}:
		for _, elem := range val {
			assertNoRefVal(t, elem)
		}
	case map[string]interface{}:
		for _, elem := range val {
			assertNoRefVal(t, elem)
		}
	}
}
