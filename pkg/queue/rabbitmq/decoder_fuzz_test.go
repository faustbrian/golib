package rabbitmq

import (
	"testing"

	"github.com/faustbrian/golib/pkg/queue/job"
	amqp "github.com/rabbitmq/amqp091-go"
)

func FuzzRequestDelivery(f *testing.F) {
	valid := job.NewTask(nil)
	f.Add(valid.Bytes())
	f.Add([]byte("not-json"))

	f.Fuzz(func(t *testing.T, data []byte) {
		acknowledger := &recordingAcknowledger{}
		deliveries := make(chan amqp.Delivery, 1)
		deliveries <- amqp.Delivery{
			Acknowledger: acknowledger,
			DeliveryTag:  1,
			Body:         data,
		}
		worker := &Worker{tasks: deliveries, opts: newOptions()}
		worker.startOnce.Do(func() {})
		_, _ = worker.Request()
	})
}
