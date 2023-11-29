package orchestrator

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/garethgeorge/resticui/internal/config"
)

type testTask struct {
	onRun  func() error
	onNext func(curTime time.Time) *time.Time
}

func (t *testTask) Name() string {
	return "test"
}

func (t *testTask) Next(now time.Time) *time.Time {
	return t.onNext(now)
}

func (t *testTask) Run(ctx context.Context) error {
	return t.onRun()
}

func TestTaskScheduling(t *testing.T) {
	t.Parallel()

	// Arrange
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	orch := NewOrchestrator("", config.NewDefaultConfig(), nil)

	var wg sync.WaitGroup
	wg.Add(1)
	task := &testTask{
		onRun: func() error {
			wg.Done()
			cancel()
			return nil
		},
		onNext: func(t time.Time) *time.Time {
			t = t.Add(10 * time.Millisecond)
			return &t
		},
	}

	wg.Add(1)
	go func() {
		defer wg.Done()
		orch.Run(ctx)
	}()

	// Act
	orch.ScheduleTask(task)

	// Assert passes if all tasks run and the orchestrator exists when cancelled.
	wg.Wait()
}

func TestTaskRescheduling(t *testing.T) {
	t.Parallel()

	// Arrange
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	orch := NewOrchestrator("", config.NewDefaultConfig(), nil)

	var wg sync.WaitGroup
	wg.Add(1)

	go func() {
		defer wg.Done()
		orch.Run(ctx)
	}()

	// Act
	count := 0
	ranTimes := 0

	orch.ScheduleTask(&testTask{
		onNext: func(t time.Time) *time.Time {
			if count < 10 {
				count += 1
				return &t
			}
			return nil
		},
		onRun: func() error {
			ranTimes += 1
			if ranTimes == 10 {
				cancel()
			}
			return nil
		},
	})

	wg.Wait()

	if count != 10 {
		t.Errorf("expected 10 Next calls, got %d", count)
	}

	if ranTimes != 10 {
		t.Errorf("expected 10 Run calls, got %d", ranTimes)
	}
}

func TestGracefulShutdown(t *testing.T) {
	t.Parallel()

	// Arrange
	orch := NewOrchestrator("", config.NewDefaultConfig(), nil)
	ctx, cancel := context.WithCancel(context.Background())

	go func() {
		time.Sleep(10 * time.Millisecond)
		cancel()
	}()

	// Act
	orch.Run(ctx)
}

func TestSchedulerWait(t *testing.T) {
	t.Parallel()

	// Arrange
	curTime := time.Now()
	orch := NewOrchestrator("", config.NewDefaultConfig(), nil)
	orch.now = func() time.Time {
		return curTime
	}

	ran := make(chan struct{})
	orch.ScheduleTask(&testTask{
		onNext: func(t time.Time) *time.Time {
			t = t.Add(5 * time.Millisecond)
			return &t
		},
		onRun: func() error {
			close(ran)
			return nil
		},
	})

	// Act
	go orch.Run(context.Background())

	// Assert
	select {
	case <-time.NewTimer(20 * time.Millisecond).C:
	case <-ran:
		t.Errorf("expected task to not run yet")
	}

	curTime = time.Now()

	// Schedule another task just to trigger a queue refresh
	orch.ScheduleTask(&testTask{
		onNext: func(t time.Time) *time.Time {
			t = t.Add(5 * time.Millisecond)
			return &t
		},
		onRun: func() error {
			t.Fatalf("should never run")
			return nil
		},
	})

	select {
	case <-time.NewTimer(1000 * time.Millisecond).C:
		t.Errorf("expected task to run")
	case <-ran:
	}
}
