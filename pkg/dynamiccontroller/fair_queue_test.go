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

package dynamiccontroller

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func keyFn(s string) string {
	if len(s) > 0 {
		return string(s[0]) // first char as key
	}
	return ""
}

func TestFairQueue_RoundRobin(t *testing.T) {
	q := NewFairQueue(keyFn)

	// Key "a": 3 items, Key "b": 1 item
	q.Push("a1")
	q.Push("a2")
	q.Push("a3")
	q.Push("b1")

	assert.Equal(t, 4, q.Len())

	// Round-robin: a, b, a, a
	assert.Equal(t, "a1", q.Pop())
	assert.Equal(t, "b1", q.Pop())
	assert.Equal(t, "a2", q.Pop())
	// b is empty, skipped
	assert.Equal(t, "a3", q.Pop())
	assert.Equal(t, 0, q.Len())
}

func TestFairQueue_FIFOWithinKey(t *testing.T) {
	q := NewFairQueue(keyFn)

	q.Push("x1")
	q.Push("x2")
	q.Push("x3")

	assert.Equal(t, "x1", q.Pop())
	assert.Equal(t, "x2", q.Pop())
	assert.Equal(t, "x3", q.Pop())
}

func TestFairQueue_ManyKeys(t *testing.T) {
	q := NewFairQueue(keyFn)

	q.Push("a1")
	q.Push("b1")
	q.Push("c1")
	q.Push("a2")
	q.Push("b2")

	// Round-robin: a, b, c, a, b
	results := make([]string, 5)
	for i := range results {
		results[i] = q.Pop()
	}

	assert.Equal(t, []string{"a1", "b1", "c1", "a2", "b2"}, results)
}

func TestFairQueue_SkipsEmptyQueues(t *testing.T) {
	q := NewFairQueue(keyFn)

	q.Push("a1")
	q.Push("b1")
	q.Push("a2")

	assert.Equal(t, "a1", q.Pop()) // a
	assert.Equal(t, "b1", q.Pop()) // b
	assert.Equal(t, "a2", q.Pop()) // a (b empty, skipped)
}

func TestFairQueue_SingleKey(t *testing.T) {
	q := NewFairQueue(keyFn)

	q.Push("z1")
	q.Push("z2")
	q.Push("z3")

	assert.Equal(t, "z1", q.Pop())
	assert.Equal(t, "z2", q.Pop())
	assert.Equal(t, "z3", q.Pop())
	assert.Equal(t, 0, q.Len())
}

func TestFairQueue_FloodOneKey(t *testing.T) {
	q := NewFairQueue(keyFn)

	// Simulate: parent requeues 10x, leaf has 2 items
	for i := 0; i < 10; i++ {
		q.Push("p" + string(rune('0'+i))) // all key "p"
	}
	q.Push("l1") // key "l"
	q.Push("l2")

	// First pop: p (robin starts at 0)
	// Second pop: l
	// Third pop: p
	// Fourth pop: l
	// Then p, p, p, p, p, p, p, p
	first := q.Pop()
	second := q.Pop()
	assert.Equal(t, "p0", first)
	assert.Equal(t, "l1", second)

	third := q.Pop()
	fourth := q.Pop()
	assert.Equal(t, "p1", third)
	assert.Equal(t, "l2", fourth)

	// Remaining 8 are all "p"
	for i := 0; i < 8; i++ {
		item := q.Pop()
		assert.Equal(t, string(item[0]), "p")
	}
	assert.Equal(t, 0, q.Len())
}
