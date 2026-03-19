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

// MLFQueue is a Multi-Level Feedback Queue implementation of workqueue.Queue[T].
//
// Items start at level 0 (highest priority) on first Push. If an item is
// popped and re-pushed (failed reconcile), it demotes to level 1, then level 2.
// Touch (watch event for an already-queued item) promotes back to level 0.
// Pop always drains level 0 first, then level 1, then level 2.
//
// All operations are O(1) amortized except Touch which is O(n) in the
// worst case but rare in practice.
type MLFQueue[T comparable] struct {
	levels [3][]T
	depth  map[T]int      // push count per item, determines demotion level
	where  map[T]int      // item → level (for Touch)
	total  int
}

// NewMLFQueue creates a new Multi-Level Feedback Queue.
func NewMLFQueue[T comparable]() *MLFQueue[T] {
	return &MLFQueue[T]{
		depth: make(map[T]int),
		where: make(map[T]int),
	}
}

// Push adds an item. First push → level 0. Subsequent pushes (re-adds after
// failure) demote: level 1 after 1 failure, level 2 after 2+.
func (q *MLFQueue[T]) Push(item T) {
	d := q.depth[item]
	level := d
	if level > 2 {
		level = 2
	}
	q.levels[level] = append(q.levels[level], item)
	q.where[item] = level
	q.depth[item] = d + 1
	q.total++
}

// Pop returns the highest priority item. Drains level 0, then 1, then 2.
// FIFO within each level.
func (q *MLFQueue[T]) Pop() T {
	for i := 0; i < 3; i++ {
		if len(q.levels[i]) > 0 {
			item := q.levels[i][0]
			var zero T
			q.levels[i][0] = zero
			q.levels[i] = q.levels[i][1:]
			delete(q.where, item)
			// Keep depth — if the item is re-pushed it should demote
			q.total--
			return item
		}
	}
	var zero T
	return zero
}

// Touch promotes an item back to level 0. Called when a watch event fires
// for an item already in the queue (re-Add of existing item).
func (q *MLFQueue[T]) Touch(item T) {
	level, ok := q.where[item]
	if !ok || level == 0 {
		if ok {
			q.depth[item] = 0
		}
		return
	}
	// Remove from current level (linear scan — Touch is rare)
	for j, v := range q.levels[level] {
		if v == item {
			q.levels[level] = append(q.levels[level][:j], q.levels[level][j+1:]...)
			break
		}
	}
	// Add to end of level 0
	q.levels[0] = append(q.levels[0], item)
	q.where[item] = 0
	q.depth[item] = 0
}

// Len returns total items across all levels.
func (q *MLFQueue[T]) Len() int {
	return q.total
}
