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

package library

import (
	"testing"

	"github.com/google/cel-go/cel"
	"github.com/google/cel-go/common/types"
)

func TestRandomString(t *testing.T) {
	// Create a new CEL environment with just the RandomString function
	env, err := cel.NewEnv(RandomString())
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

				// Verify length
				if len(str) != tc.length {
					t.Errorf("Expected string length of %d, got %d", tc.length, len(str))
				}

				// Verify character set
				for _, c := range str {
					if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'z')) {
						t.Errorf("Invalid character in random string: %c", c)
					}
				}

				// Verify uniqueness
				if results[str] {
					t.Error("Got duplicate random string")
				}
				results[str] = true
			}
		})
	}
}

func TestRandomStringErrors(t *testing.T) {
	env, err := cel.NewEnv(RandomString())
	if err != nil {
		t.Fatalf("Failed to create CEL environment: %v", err)
	}

	testCases := []struct {
		name    string
		expr    string
		wantErr string
	}{
		{
			name:    "negative length",
			expr:    "randomString(-1)",
			wantErr: "randomString length must be positive",
		},
		{
			name:    "zero length",
			expr:    "randomString(0)",
			wantErr: "randomString length must be positive",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			ast, issues := env.Compile(tc.expr)
			if issues != nil && issues.Err() != nil {
				t.Fatalf("Failed to compile expression: %v", issues.Err())
			}

			prg, err := env.Program(ast)
			if err != nil {
				t.Fatalf("Failed to create program: %v", err)
			}

			result, _, err := prg.Eval(map[string]interface{}{})
			if err == nil {
				t.Error("Expected error, got none")
			}
			if errVal, ok := result.(*types.Err); !ok || errVal.Error() != tc.wantErr {
				t.Errorf("Expected error %q, got %v", tc.wantErr, result)
			}
		})
	}
}
