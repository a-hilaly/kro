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

func TestHeatQueue_FIFO_WhenNoTouch(t *testing.T) {
	q := NewHeatQueue[string]()
	q.Push("a")
	q.Push("b")
	q.Push("c")

	assert.Equal(t, 3, q.Len())
	assert.Equal(t, "a", q.Pop())
	assert.Equal(t, "b", q.Pop())
	assert.Equal(t, "c", q.Pop())
	assert.Equal(t, 0, q.Len())
}

func TestHeatQueue_TouchBumpsToFront(t *testing.T) {
	q := NewHeatQueue[string]()
	q.Push("cold1")
	q.Push("cold2")
	q.Push("hot")

	q.Touch("hot")

	assert.Equal(t, "hot", q.Pop())
	assert.Equal(t, "cold1", q.Pop())
	assert.Equal(t, "cold2", q.Pop())
}

func TestHeatQueue_MultipleTouch(t *testing.T) {
	q := NewHeatQueue[string]()
	q.Push("a")
	q.Push("b")
	q.Push("c")

	q.Touch("a") // heat=1
	q.Touch("c") // heat=1
	q.Touch("c") // heat=2

	assert.Equal(t, "c", q.Pop()) // hottest
	assert.Equal(t, "a", q.Pop()) // heat=1, older than remaining
	assert.Equal(t, "b", q.Pop()) // cold
}

func TestHeatQueue_PopResetsHeat(t *testing.T) {
	q := NewHeatQueue[string]()
	q.Push("a")
	q.Touch("a")
	q.Touch("a")
	assert.Equal(t, "a", q.Pop())

	// Re-push: starts cold again
	q.Push("b")
	q.Push("a") // was hot, now cold
	assert.Equal(t, "b", q.Pop()) // FIFO among cold
	assert.Equal(t, "a", q.Pop())
}

func TestHeatQueue_TouchNonExistent(t *testing.T) {
	q := NewHeatQueue[string]()
	q.Push("a")
	q.Touch("nonexistent") // should not panic
	assert.Equal(t, "a", q.Pop())
}

func TestHeatQueue_FIFOTiebreak(t *testing.T) {
	q := NewHeatQueue[string]()
	q.Push("first")
	q.Push("second")
	q.Push("third")

	// Touch all equally
	q.Touch("first")
	q.Touch("second")
	q.Touch("third")

	// Same heat, FIFO order
	assert.Equal(t, "first", q.Pop())
	assert.Equal(t, "second", q.Pop())
	assert.Equal(t, "third", q.Pop())
}
