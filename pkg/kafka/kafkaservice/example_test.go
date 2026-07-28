package kafkaservice_test

import (
	"context"
	"fmt"

	"github.com/faustbrian/golib/pkg/correlation"
	"github.com/faustbrian/golib/pkg/kafka"
	"github.com/faustbrian/golib/pkg/kafka/kafkaservice"
)

type exampleGenerator struct {
	values []string
}

func (generator *exampleGenerator) New() (string, error) {
	value := generator.values[0]
	generator.values = generator.values[1:]

	return value, nil
}

type exampleProducer struct {
	record kafka.ProducerRecord
}

func ExampleNewProducer() {
	factory, _ := correlation.NewFactory(correlation.FactoryOptions{
		Generator: &exampleGenerator{
			values: []string{"workflow", "command-request", "record-request"},
		},
	})
	resource := &exampleProducer{}
	producer, err := kafkaservice.NewProducer(
		kafkaservice.ProducerOptions[*exampleProducer]{
			Name: "events-producer", Resource: resource, Correlation: factory,
			Publish: func(
				_ context.Context,
				resource *exampleProducer,
				record kafka.ProducerRecord,
			) (kafka.DeliveryResult, error) {
				resource.record = record

				return kafka.DeliveryResult{Topic: record.Topic}, nil
			},
		},
	)
	if err != nil {
		return
	}
	component := producer.Component()
	if err = component.Start(context.Background()); err != nil {
		return
	}
	parent, _ := factory.Start()
	values, _, err := producer.Publish(
		correlation.WithValues(context.Background(), parent),
		kafka.ProducerRecord{
			Topic: "tracking-events", Key: []byte("tracked-item"),
		},
	)
	if err != nil {
		return
	}
	fmt.Println(values.CorrelationID, values.RequestID, values.CausationID)
	if err = component.Stop(context.Background()); err != nil {
		return
	}

	// Output: workflow record-request command-request
}
