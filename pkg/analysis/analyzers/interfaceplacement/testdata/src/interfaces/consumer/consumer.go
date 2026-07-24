package consumer

// Provider is consumer-owned and accepted outside configured provider trees.
type Provider interface {
	Call()
}
