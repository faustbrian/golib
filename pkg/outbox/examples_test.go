package outbox_test

import (
	"fmt"

	"github.com/faustbrian/golib/pkg/outbox"
)

func Example_duplicateDeliveryRequiresIdempotentConsumer() {
	consumer := idempotentConsumer{seen: make(map[string]struct{})}
	envelope := outbox.Envelope{ID: "evt-42", Topic: "orders.created"}

	consumer.Consume(envelope)
	consumer.Consume(envelope) // relay redelivery after an ambiguous result

	fmt.Println(consumer.effects)
	// Output: 1
}

type idempotentConsumer struct {
	seen    map[string]struct{}
	effects int
}

func (consumer *idempotentConsumer) Consume(envelope outbox.Envelope) {
	if _, duplicate := consumer.seen[envelope.ID]; duplicate {
		return
	}
	consumer.seen[envelope.ID] = struct{}{}
	consumer.effects++
}
