package simulator

import "container/heap"

type eventKind int

const (
	eventSupplyOffline eventKind = iota
	eventArrive
	eventDwellDone
	eventAssignmentDeadline
	eventPickupDeadline
	eventHandoffDeadline
	eventSupplyOnline
	eventRequest
	eventBatchTick
)

// Tie-break priorities from the spec: offline, dropoff, pickup, cancellation,
// supply online, request, batch tick.
const (
	priorityOffline = iota
	priorityDropoff
	priorityPickup
	priorityCancellation
	prioritySupplyOnline
	priorityRequest
	priorityBatchTick
)

type event struct {
	at       int64
	priority int
	id       string
	sequence int64
	kind     eventKind
	version  int64
}

type eventQueue []*event

func (q eventQueue) Len() int { return len(q) }
func (q eventQueue) Less(i, j int) bool {
	a, b := q[i], q[j]
	if a.at != b.at {
		return a.at < b.at
	}
	if a.priority != b.priority {
		return a.priority < b.priority
	}
	if a.id != b.id {
		return a.id < b.id
	}
	return a.sequence < b.sequence
}
func (q eventQueue) Swap(i, j int) { q[i], q[j] = q[j], q[i] }
func (q *eventQueue) Push(x any)   { *q = append(*q, x.(*event)) }
func (q *eventQueue) Pop() any {
	old := *q
	e := old[len(old)-1]
	*q = old[:len(old)-1]
	return e
}

func (s *Simulator) push(e *event) {
	s.sequence++
	e.sequence = s.sequence
	heap.Push(&s.queue, e)
}
