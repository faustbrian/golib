package eventsourcing_test

import eventsourcing "github.com/faustbrian/golib/pkg/event-sourcing"

type messageCodecContract struct{}

func (messageCodecContract) Encode(eventsourcing.Message) ([]byte, error) {
	return nil, nil
}

func (messageCodecContract) Decode([]byte) (eventsourcing.Message, error) {
	return eventsourcing.Message{}, nil
}

var _ eventsourcing.MessageCodec = messageCodecContract{}
