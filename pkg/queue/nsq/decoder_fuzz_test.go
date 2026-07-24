package nsq

import (
	"testing"

	"github.com/faustbrian/golib/pkg/queue/job"
	"github.com/faustbrian/golib/pkg/queue/management"
	nsqgo "github.com/nsqio/go-nsq"
)

func FuzzRequestDelivery(f *testing.F) {
	valid := job.NewTask(nil)
	f.Add(valid.Bytes())
	f.Add([]byte("not-json"))

	f.Fuzz(func(t *testing.T, data []byte) {
		delegate := &messageDelegate{}
		message := nsqgo.NewMessage(nsqgo.MessageID{}, data)
		message.Delegate = delegate
		tasks := make(chan *nsqgo.Message, 1)
		tasks <- message
		worker := &Worker{tasks: tasks, opts: newOptions()}
		worker.startOnce.Do(func() {})
		_, _ = worker.Request()
	})
}

func FuzzDeadLetterRecord(f *testing.F) {
	worker := &Worker{opts: newOptions()}
	message := nsqgo.NewMessage(nsqgo.MessageID{1}, []byte("payload"))
	message.Attempts = 5
	valid, err := worker.encodeNSQDeadLetter(
		message, management.ClassificationRetryable, "attempts_exhausted", 5,
	)
	if err != nil {
		f.Fatal(err)
	}
	f.Add(valid)
	f.Add([]byte("not-json"))

	f.Fuzz(func(t *testing.T, data []byte) {
		_, _ = decodeNSQDeadLetter(data)
	})
}
