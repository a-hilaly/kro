// Copyright 2025 The Kube Resource Orchestrator Authors.
//
// Licensed under the Apache License, Version 2.0 (the "License"). You may
// not use this file except in compliance with the License. A copy of the
// License is located at
//
//    http://www.apache.org/licenses/LICENSE-2.0
//
// or in the "license" file accompanying this file. This file is distributed
// on an "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either
// express or implied. See the License for the specific language governing
// permissions and limitations under the License.

package cel

import (
	"testing"

	"github.com/google/cel-go/common/types"
)

func TestRandomStringFunction(t *testing.T) {
	env, err := DefaultEnvironment()
	if err != nil {
		t.Fatalf("Failed to create CEL environment: %v", err)
	}

	testCases := []struct {
		name   string
		expr   string
		length int
	}{
		{
			name:   "generate 10 character string",
			expr:   `randomString(10)`,
			length: 10,
		},
		{
			name:   "generate 20 character string",
			expr:   `randomString(20)`,
			length: 20,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Test that we can compile and evaluate the randomString function
			ast, issues := env.Compile(tc.expr)
			if issues != nil && issues.Err() != nil {
				t.Fatalf("Failed to compile expression: %v", issues.Err())
			}

			prg, err := env.Program(ast)
			if err != nil {
				t.Fatalf("Failed to create program: %v", err)
			}

			// Run the function multiple times to verify we get different results
			results := make(map[string]bool)
			for i := 0; i < 10; i++ {
				result, _, err := prg.Eval(map[string]interface{}{})
				if err != nil {
					t.Fatalf("Failed to evaluate expression: %v", err)
				}

				// Get the string value from the CEL result
				str := result.(types.String).Value().(string)
				if len(str) != tc.length {
					t.Errorf("Expected string length of %d, got %d", tc.length, len(str))
				}

				// Verify we're getting unique strings
				if results[str] {
					t.Error("Got duplicate random string")
				}
				results[str] = true
			}
		})
	}
}
