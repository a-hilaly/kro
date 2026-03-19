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

import "container/heap"

// HeatQueue is a workqueue.Queue[T] implementation that prioritizes items
// by heat. Items enter cold (heat=0). Each Touch (re-Add of an already-queued
// item) increments heat. Pop returns the hottest item. Ties broken by
// insertion order (FIFO). Once popped, the item is gone — if re-added later
// it starts cold again.
//
// This produces bottom-up convergence in RGD hierarchies without any
// topology awareness: leaf resources succeed and fire watch events that
// Touch their parents in the queue, making parents hotter and processed
// before other cold items.
type HeatQueue[T comparable] struct {
	items []heapEntry[T]
	index map[T]int // item → position in items
	seq   uint64    // monotonic counter for FIFO tiebreak
}

type heapEntry[T comparable] struct {
	item T
	heat int
	seq  uint64 // lower = older = higher priority among same heat
	pos  int    // index in heap
}

// NewHeatQueue creates a new HeatQueue.
func NewHeatQueue[T comparable]() *HeatQueue[T] {
	return &HeatQueue[T]{
		index: make(map[T]int),
	}
}

// Touch increments the heat of an already-queued item.
// Called by workqueue when Add is called for an item already in the queue.
func (q *HeatQueue[T]) Touch(item T) {
	pos, ok := q.index[item]
	if !ok {
		return
	}
	q.items[pos].heat++
	heap.Fix((*heatHeap[T])(q), pos)
}

// Push adds a new item with heat=0.
func (q *HeatQueue[T]) Push(item T) {
	q.seq++
	entry := heapEntry[T]{item: item, heat: 0, seq: q.seq}
	q.items = append(q.items, entry)
	pos := len(q.items) - 1
	q.items[pos].pos = pos
	q.index[item] = pos
	heap.Fix((*heatHeap[T])(q), pos)
}

// Pop removes and returns the hottest item (highest heat, FIFO tiebreak).
func (q *HeatQueue[T]) Pop() T {
	entry := heap.Pop((*heatHeap[T])(q)).(heapEntry[T])
	delete(q.index, entry.item)
	return entry.item
}

// Len returns the number of items in the queue.
func (q *HeatQueue[T]) Len() int {
	return len(q.items)
}

// heatHeap adapts HeatQueue for container/heap.
// Higher heat = higher priority. Among equal heat, lower seq = higher priority (FIFO).
type heatHeap[T comparable] HeatQueue[T]

func (h *heatHeap[T]) Len() int { return len(h.items) }

func (h *heatHeap[T]) Less(i, j int) bool {
	if h.items[i].heat != h.items[j].heat {
		return h.items[i].heat > h.items[j].heat // higher heat first
	}
	return h.items[i].seq < h.items[j].seq // older first (FIFO)
}

func (h *heatHeap[T]) Swap(i, j int) {
	h.items[i], h.items[j] = h.items[j], h.items[i]
	h.items[i].pos = i
	h.items[j].pos = j
	h.index[h.items[i].item] = i
	h.index[h.items[j].item] = j
}

func (h *heatHeap[T]) Push(x any) {
	// Called by heap.Push — not used directly, HeatQueue.Push handles insertion.
	entry := x.(heapEntry[T])
	entry.pos = len(h.items)
	h.items = append(h.items, entry)
	h.index[entry.item] = entry.pos
}

func (h *heatHeap[T]) Pop() any {
	n := len(h.items)
	entry := h.items[n-1]
	h.items[n-1] = *new(heapEntry[T]) // zero out for GC
	h.items = h.items[:n-1]
	return entry
}
