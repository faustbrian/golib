package nats

import (
	"testing"

	"github.com/faustbrian/golib/pkg/queue/job"
	natsgo "github.com/nats-io/nats.go"
)

func FuzzRequestDelivery(f *testing.F) {
	valid := job.NewTask(nil)
	f.Add(valid.Bytes())
	f.Add([]byte("not-json"))

	f.Fuzz(func(t *testing.T, data []byte) {
		tasks := make(chan *natsgo.Msg, 1)
		tasks <- &natsgo.Msg{Data: data}
		worker := &Worker{tasks: tasks, opts: newOptions()}
		worker.startOnce.Do(func() {})
		_, _ = worker.Request()
	})
}
