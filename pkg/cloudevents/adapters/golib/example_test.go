package golib_test

import (
	"fmt"
	"time"

	golib "github.com/faustbrian/golib/pkg/cloudevents/adapters/golib"
	"github.com/faustbrian/golib/pkg/queue/job"
)

func ExampleQueueToCloudEvent() {
	message := job.Message{
		Timeout: time.Minute,
		Body:    []byte(`{"order":"A-123"}`),
		Metadata: &job.Metadata{
			OriginalID: "job-1", JobType: "order.notify", ContentType: "application/json",
		},
	}
	event, state, report, err := golib.QueueToCloudEvent(message, golib.QueueOptions{Source: "/queue/orders"})
	if err != nil {
		panic(err)
	}
	fmt.Println(event.ID(), event.Type(), state.RetryCount, len(report.Losses))
	// Output: job-1 order.notify 0 0
}
