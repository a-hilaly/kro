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
	"crypto/rand"
	"encoding/binary"
	"math"

	"github.com/google/cel-go/cel"
	"github.com/google/cel-go/common/types"
	"github.com/google/cel-go/common/types/ref"
)

const (
	// alphanumericChars contains all possible characters for the random string
	alphanumericChars = "0123456789abcdefghijklmnopqrstuvwxyz"
)

// RandomString returns a CEL function that generates random strings
func RandomString() cel.EnvOption {
	return cel.Function("randomString",
		cel.Overload("randomString_int_string",
			[]*cel.Type{cel.IntType},
			cel.StringType,
			cel.UnaryBinding(generateRandomString),
		),
	)
}

// generateRandomString creates a random string of lowercase alphanumeric characters
func generateRandomString(arg ref.Val) ref.Val {
	length, ok := arg.(types.Int)
	if !ok {
		return types.NewErr("randomString argument must be an integer")
	}

	n := int(length.Value().(int64))
	if n <= 0 {
		return types.NewErr("randomString length must be positive")
	}

	bytes := make([]byte, n*4)
	if _, err := rand.Read(bytes); err != nil {
		return types.NewErr("failed to generate random string: %v", err)
	}

	result := make([]byte, n)
	charsLen := int64(len(alphanumericChars))
	for i := 0; i < n; i++ {
		idx := binary.BigEndian.Uint32(bytes[i*4 : (i+1)*4])
		idx = uint32(float64(idx) / float64(math.MaxUint32) * float64(charsLen))
		result[i] = alphanumericChars[idx]
	}

	return types.String(string(result))
}
