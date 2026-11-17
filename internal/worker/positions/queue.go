package positions

import (
	"sync"

	"github.com/google/uuid"
	"go.opentelemetry.io/otel/trace"
)

type Task struct {
	TripID   uuid.UUID
	DriverID uuid.UUID
	Span     trace.SpanContext
}

type EnqueueResult string

const (
	Queued    EnqueueResult = "queued"
	Duplicate EnqueueResult = "duplicate"
	Dropped   EnqueueResult = "dropped"
)

type entry struct {
	task    Task
	started bool
}

type Queue struct {
	mu      sync.Mutex
	tasks   chan *entry
	pending map[uuid.UUID]*entry
}

func NewQueue(size int) *Queue {
	return &Queue{tasks: make(chan *entry, size), pending: make(map[uuid.UUID]*entry)}
}

func (queue *Queue) Enqueue(task Task) EnqueueResult {
	queue.mu.Lock()
	defer queue.mu.Unlock()
	if _, exists := queue.pending[task.TripID]; exists {
		return Duplicate
	}
	item := &entry{task: task}
	select {
	case queue.tasks <- item:
		queue.pending[task.TripID] = item
		return Queued
	default:
		return Dropped
	}
}

func (queue *Queue) Cancel(id uuid.UUID) {
	queue.mu.Lock()
	defer queue.mu.Unlock()
	if item, exists := queue.pending[id]; exists && !item.started {
		delete(queue.pending, id)
	}
}

func (queue *Queue) Start(item *entry) bool {
	queue.mu.Lock()
	defer queue.mu.Unlock()
	if queue.pending[item.task.TripID] != item {
		return false
	}
	item.started = true
	return true
}

func (queue *Queue) Finish(item *entry) {
	queue.mu.Lock()
	defer queue.mu.Unlock()
	if queue.pending[item.task.TripID] == item {
		delete(queue.pending, item.task.TripID)
	}
}
