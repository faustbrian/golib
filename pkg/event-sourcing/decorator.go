package eventsourcing

// MessageDecoratorFunc returns an immutable pending-message copy.
//
// Decorators may replace application metadata only. Stable envelope identity,
// event data, recording time, correlation, causation, tenant, and partition
// fields must remain unchanged.
type MessageDecoratorFunc func(PendingMessage) (PendingMessage, error)

// MessageDecoratorChain applies validated decorators in registration order.
type MessageDecoratorChain struct {
	decorators []MessageDecoratorFunc
}

// NewMessageDecoratorChain validates and owns an ordered decorator chain.
func NewMessageDecoratorChain(
	decorators ...MessageDecoratorFunc,
) (*MessageDecoratorChain, error) {
	owned := make([]MessageDecoratorFunc, len(decorators))
	for index, decorator := range decorators {
		if decorator == nil {
			return nil, invalid("decorator", "must be assigned")
		}
		owned[index] = decorator
	}

	return &MessageDecoratorChain{decorators: owned}, nil
}

// NewMetadataDecorator constructs a collision-rejecting static metadata
// decorator and defensively owns the supplied entries.
func NewMetadataDecorator(
	metadata map[string]string,
) (MessageDecoratorFunc, error) {
	owned, err := copyMetadata(metadata)
	if err != nil {
		return nil, err
	}

	return func(message PendingMessage) (PendingMessage, error) {
		combined := message.Metadata()
		for key, value := range owned {
			if _, exists := combined[key]; exists {
				return PendingMessage{}, ErrMetadataCollision
			}
			combined[key] = value
		}

		return message.WithMetadata(combined)
	}, nil
}

// Decorate applies every decorator or stops at the first contained failure.
func (chain *MessageDecoratorChain) Decorate(
	message PendingMessage,
) (PendingMessage, error) {
	if chain == nil || !validPendingMessage(message) {
		return PendingMessage{}, ErrInvalidArgument
	}

	decorated := clonePendingMessage(message)
	for index, decorator := range chain.decorators {
		next, err := callMessageDecorator(index, decorator, decorated)
		if err != nil {
			return PendingMessage{}, err
		}
		decorated = next
	}

	return clonePendingMessage(decorated), nil
}

func callMessageDecorator(
	index int,
	decorator MessageDecoratorFunc,
	message PendingMessage,
) (decorated PendingMessage, err error) {
	defer func() {
		if recover() != nil {
			decorated = PendingMessage{}
			err = &DecoratorError{Index: index, Cause: ErrDecoratorPanic}
		}
	}()

	decorated, err = decorator(clonePendingMessage(message))
	if err != nil {
		return PendingMessage{}, &DecoratorError{Index: index, Cause: err}
	}
	if !validPendingMessage(decorated) {
		return PendingMessage{}, &DecoratorError{
			Index: index,
			Cause: ErrInvalidArgument,
		}
	}

	expected := clonePendingMessage(message)
	expected.metadata = cloneMetadata(decorated.metadata)
	if !pendingMessagesEqual(expected, decorated) {
		return PendingMessage{}, &DecoratorError{
			Index: index,
			Cause: ErrDecoratorChangedMessage,
		}
	}

	return clonePendingMessage(decorated), nil
}

func validPendingMessage(message PendingMessage) bool {
	return !message.id.IsZero() &&
		!message.stream.IsZero() &&
		!message.event.IsZero() &&
		!message.recordedAt.IsZero()
}

// DecoratorError identifies the ordered decorator that failed without printing
// application metadata, payload, panic values, or wrapped diagnostics.
type DecoratorError struct {
	Index int
	Cause error
}

// Error implements error with redacted diagnostics.
func (*DecoratorError) Error() string {
	return "event message decoration failed"
}

// Unwrap preserves the underlying cause for errors.Is and errors.As.
func (err *DecoratorError) Unwrap() error {
	return err.Cause
}

var _ error = (*DecoratorError)(nil)
