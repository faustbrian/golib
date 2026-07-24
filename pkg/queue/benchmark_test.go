package queue

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/faustbrian/golib/pkg/queue/core"
	"github.com/faustbrian/golib/pkg/queue/job"
)

var count = 1

type testqueue interface {
	Queue(task core.TaskMessage) error
	Request() (core.TaskMessage, error)
}

func testQueue(b *testing.B, pool testqueue) {
	message := job.NewTask(func(context.Context) error {
		return nil
	},
		job.AllowOption{
			RetryCount: job.Int64(100),
			RetryDelay: job.Time(30 * time.Millisecond),
			Timeout:    job.Time(3 * time.Millisecond),
		},
	)

	b.ReportAllocs()
	b.ResetTimer()
	for n := 0; n < b.N; n++ {
		for i := 0; i < count; i++ {
			_ = pool.Queue(&message)
			_, _ = pool.Request()
		}
	}
}

func BenchmarkNewRing(b *testing.B) {
	pool := NewRing(
		WithQueueSize(b.N*count),
		WithLogger(emptyLogger{}),
	)

	testQueue(b, pool)
}

func BenchmarkQueueTask(b *testing.B) {
	w := NewRing(WithQueueSize(b.N))
	q, _ := NewQueue(
		WithWorker(w),
		WithLogger(emptyLogger{}),
	)
	b.ReportAllocs()
	b.ResetTimer()

	m := job.NewTask(func(context.Context) error {
		return nil
	})

	for n := 0; n < b.N; n++ {
		if err := q.queue(&m); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkQueue(b *testing.B) {
	w := NewRing(WithQueueSize(b.N))
	q, _ := NewQueue(
		WithWorker(w),
		WithLogger(emptyLogger{}),
	)
	b.ReportAllocs()
	b.ResetTimer()

	m := job.NewMessage(&mockMessage{
		message: "foo",
	})

	for n := 0; n < b.N; n++ {
		if err := q.queue(&m); err != nil {
			b.Fatal(err)
		}
	}
}

// func BenchmarkRingPayload(b *testing.B) {
// 	b.ReportAllocs()

// 	task := &job.Message{
// 		Timeout: 100 * time.Millisecond,
// 		Payload: []byte(`{"timeout":3600000000000}`),
// 	}
// 	w := NewRing(
// 		WithFn(func(ctx context.Context, m core.TaskMessage) error {
// 			return nil
// 		}),
// 	)

// 	q, _ := NewQueue(
// 		WithWorker(w),
// 		WithLogger(emptyLogger{}),
// 	)

// 	for n := 0; n < b.N; n++ {
// 		_ = q.run(task)
// 	}
// }

func BenchmarkRingWithTask(b *testing.B) {
	b.ReportAllocs()

	task := job.Message{
		Timeout: 100 * time.Millisecond,
		Task: func(_ context.Context) error {
			return nil
		},
	}
	w := NewRing(
		WithFn(func(ctx context.Context, m core.TaskMessage) error {
			return nil
		}),
	)

	q, _ := NewQueue(
		WithWorker(w),
		WithLogger(emptyLogger{}),
	)

	for n := 0; n < b.N; n++ {
		_ = q.run(&task)
	}
}

func BenchmarkSettlement(b *testing.B) {
	q, err := NewQueue(WithWorker(NewRing()), WithLogger(emptyLogger{}))
	if err != nil {
		b.Fatal(err)
	}
	delivery := job.NewTask(nil)
	delivery.SetAcknowledgement(func() error { return nil }, func() error { return nil })
	handlerErr := errors.New("handler failed")

	b.Run("ack", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			if err := q.settle(&delivery, nil); err != nil {
				b.Fatal(err)
			}
		}
	})
	b.Run("nack", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			if err := q.settle(&delivery, handlerErr); !errors.Is(err, handlerErr) {
				b.Fatal(err)
			}
		}
	})
}
