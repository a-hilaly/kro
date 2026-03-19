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
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestMLFQueue_NewItemsAtLevel0(t *testing.T) {
	q := NewMLFQueue[string]()
	q.Push("a")
	q.Push("b")
	q.Push("c")

	assert.Equal(t, 3, q.Len())
	assert.Equal(t, "a", q.Pop())
	assert.Equal(t, "b", q.Pop())
	assert.Equal(t, "c", q.Pop())
	assert.Equal(t, 0, q.Len())
}

func TestMLFQueue_DemotionOnRepush(t *testing.T) {
	q := NewMLFQueue[string]()

	// First push → level 0
	q.Push("a")
	q.Push("b")
	assert.Equal(t, "a", q.Pop())
	assert.Equal(t, "b", q.Pop())

	// Re-push a (failed) → level 1
	q.Push("a")
	// New item → level 0
	q.Push("c")

	// c should come first (level 0), then a (level 1)
	assert.Equal(t, "c", q.Pop())
	assert.Equal(t, "a", q.Pop())
}

func TestMLFQueue_DoubleDemotion(t *testing.T) {
	q := NewMLFQueue[string]()

	q.Push("a") // level 0
	q.Pop()
	q.Push("a") // level 1
	q.Pop()
	q.Push("a") // level 2

	q.Push("fresh") // level 0

	assert.Equal(t, "fresh", q.Pop()) // level 0 first
	assert.Equal(t, "a", q.Pop())     // level 2
}

func TestMLFQueue_TouchPromotesToLevel0(t *testing.T) {
	q := NewMLFQueue[string]()

	// Demote "a" to level 2
	q.Push("a")
	q.Pop()
	q.Push("a")
	q.Pop()
	q.Push("a") // level 2

	q.Push("b") // level 0

	// Touch "a" — promote to level 0
	q.Touch("a")

	// Both at level 0 now, "b" was there first, then "a" promoted
	first := q.Pop()
	second := q.Pop()
	// b was at level 0 before touch, a was appended after
	assert.Equal(t, "b", first)
	assert.Equal(t, "a", second)
}

func TestMLFQueue_TouchAlreadyLevel0(t *testing.T) {
	q := NewMLFQueue[string]()
	q.Push("a")
	q.Touch("a") // already level 0, should not panic or duplicate
	assert.Equal(t, 1, q.Len())
	assert.Equal(t, "a", q.Pop())
	assert.Equal(t, 0, q.Len())
}

func TestMLFQueue_TouchNonExistent(t *testing.T) {
	q := NewMLFQueue[string]()
	q.Push("a")
	q.Touch("nonexistent") // should not panic
	assert.Equal(t, 1, q.Len())
}

func TestMLFQueue_Level0BeforeLevel1BeforeLevel2(t *testing.T) {
	q := NewMLFQueue[string]()

	// Create items at different levels
	q.Push("l2")
	q.Pop()
	q.Push("l2")
	q.Pop()
	q.Push("l2") // level 2

	q.Push("l1")
	q.Pop()
	q.Push("l1") // level 1

	q.Push("l0") // level 0

	assert.Equal(t, "l0", q.Pop()) // level 0
	assert.Equal(t, "l1", q.Pop()) // level 1
	assert.Equal(t, "l2", q.Pop()) // level 2
}

func TestMLFQueue_DemotionCapsAtLevel2(t *testing.T) {
	q := NewMLFQueue[string]()

	// Push/pop 10 times — should cap at level 2
	for i := 0; i < 10; i++ {
		q.Push("stubborn")
		q.Pop()
	}
	q.Push("stubborn") // still level 2, not level 10

	q.Push("fresh") // level 0

	assert.Equal(t, "fresh", q.Pop())
	assert.Equal(t, "stubborn", q.Pop())
}

func TestMLFQueue_DepthPersistsAcrossPop(t *testing.T) {
	q := NewMLFQueue[string]()

	// Push, pop, push → level 1
	q.Push("a") // depth 0 → level 0
	q.Pop()
	q.Push("a") // depth 1 → level 1
	q.Pop()
	q.Push("a") // depth 2 → level 2

	q.Push("b") // depth 0 → level 0

	// b at level 0, a at level 2
	assert.Equal(t, "b", q.Pop())
	assert.Equal(t, "a", q.Pop())
}

func TestMLFQueue_ManyItemsMixedLevels(t *testing.T) {
	q := NewMLFQueue[string]()

	// 100 items at level 2 (failed twice)
	for i := 0; i < 100; i++ {
		name := fmt.Sprintf("fail-%d", i)
		q.Push(name)
		q.Pop()
		q.Push(name)
		q.Pop()
		q.Push(name)
	}

	// 10 items at level 0 (fresh)
	for i := 0; i < 10; i++ {
		q.Push(fmt.Sprintf("fresh-%d", i))
	}

	assert.Equal(t, 110, q.Len())

	// First 10 pops should all be fresh (level 0)
	for i := 0; i < 10; i++ {
		item := q.Pop()
		assert.Contains(t, item, "fresh")
	}

	// Remaining 100 should all be fail (level 2)
	for i := 0; i < 100; i++ {
		item := q.Pop()
		assert.Contains(t, item, "fail")
	}

	assert.Equal(t, 0, q.Len())
}

func TestMLFQueue_TouchPromotesFromLevel2ToLevel0(t *testing.T) {
	q := NewMLFQueue[string]()

	// 5 items at level 2
	for i := 0; i < 5; i++ {
		name := fmt.Sprintf("item-%d", i)
		q.Push(name)
		q.Pop()
		q.Push(name)
		q.Pop()
		q.Push(name) // level 2
	}

	// Touch item-3 — should promote to level 0
	q.Touch("item-3")

	// item-3 should come first
	assert.Equal(t, "item-3", q.Pop())

	// Remaining 4 at level 2
	for i := 0; i < 4; i++ {
		item := q.Pop()
		assert.Contains(t, item, "item-")
		assert.NotEqual(t, "item-3", item)
	}
}

func TestMLFQueue_LenConsistency(t *testing.T) {
	q := NewMLFQueue[string]()

	for i := 0; i < 50; i++ {
		q.Push(fmt.Sprintf("item-%d", i))
	}
	assert.Equal(t, 50, q.Len())

	for i := 0; i < 25; i++ {
		q.Pop()
	}
	assert.Equal(t, 25, q.Len())

	// Re-push 10 popped items (they'll go to level 1)
	for i := 0; i < 10; i++ {
		q.Push(fmt.Sprintf("item-%d", i))
	}
	assert.Equal(t, 35, q.Len())

	// Touch 5 of them
	for i := 0; i < 5; i++ {
		q.Touch(fmt.Sprintf("item-%d", i))
	}
	assert.Equal(t, 35, q.Len()) // Touch doesn't change count
}

func TestMLFQueue_EmptyPop(t *testing.T) {
	q := NewMLFQueue[string]()
	item := q.Pop()
	assert.Equal(t, "", item)
}

func TestMLFQueue_SimulateHierarchy(t *testing.T) {
	q := NewMLFQueue[string]()

	// Simulate: parent and child both enter queue
	q.Push("parent")
	q.Push("child")

	// Both at level 0, child processes first (FIFO-ish)
	q.Pop() // parent
	q.Pop() // child

	// Child succeeds, parent fails (DataPending), both re-push
	q.Push("parent") // level 1 (failed before)
	// Child doesn't re-push (it succeeded)

	// New child instance created, enters fresh
	q.Push("child-2") // level 0

	// child-2 should be processed before parent
	assert.Equal(t, "child-2", q.Pop()) // level 0
	assert.Equal(t, "parent", q.Pop())  // level 1
}
