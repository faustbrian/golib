package kafka

import (
	"context"
	"net"

	"github.com/twmb/franz-go/pkg/kgo"
)

// NewProducerWithDialerForTest constructs the production producer policy with
// a test-owned network dialer for broker fault injection.
func NewProducerWithDialerForTest(
	config ProducerConfig,
	dialer func(context.Context, string, string) (net.Conn, error),
) (*Producer, error) {
	return newProducer(config, func(options ...kgo.Opt) (*kgo.Client, error) {
		options = append(options, kgo.Dialer(dialer))

		return kgo.NewClient(options...)
	})
}
