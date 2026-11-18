package positions

import (
	"testing"

	"github.com/google/uuid"
)

func TestQueueDeduplicatesAndDropsWhenFull(t *testing.T) {
	queue := NewQueue(1)
	first := Task{TripID: uuid.New()}
	second := Task{TripID: uuid.New()}
	if got := queue.Enqueue(first); got != Queued {
		t.Fatalf("first enqueue = %s, want queued", got)
	}
	if got := queue.Enqueue(first); got != Duplicate {
		t.Fatalf("duplicate enqueue = %s, want duplicate", got)
	}
	if got := queue.Enqueue(second); got != Dropped {
		t.Fatalf("full queue enqueue = %s, want dropped", got)
	}
}

func TestQueueCancellationDoesNotInvalidateLaterTask(t *testing.T) {
	queue := NewQueue(2)
	trip := Task{TripID: uuid.New()}
	if got := queue.Enqueue(trip); got != Queued {
		t.Fatal(got)
	}
	queue.Cancel(trip.TripID)
	if got := queue.Enqueue(trip); got != Queued {
		t.Fatal(got)
	}
	old := <-queue.tasks
	if queue.Start(old) {
		t.Fatal("cancelled task should not start")
	}
	newTask := <-queue.tasks
	if !queue.Start(newTask) {
		t.Fatal("new task was cancelled by an old task")
	}
	queue.Finish(newTask)
}
