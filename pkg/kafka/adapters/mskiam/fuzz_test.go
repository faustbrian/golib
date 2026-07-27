package mskiam

import (
	"context"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
)

func FuzzConfigValidate(f *testing.F) {
	f.Add("eu-north-1", int64(0))
	f.Add("us-gov-east-1", int64(time.Second))
	f.Add("invalid", int64(-1))

	f.Fuzz(func(t *testing.T, region string, timeoutNanoseconds int64) {
		config := Config{
			Region:       region,
			TokenTimeout: time.Duration(timeoutNanoseconds),
		}
		_ = config.Validate()
	})
}

func FuzzTokenResultValidation(f *testing.F) {
	f.Add("YWJj", int64((15*time.Minute)/time.Millisecond))
	f.Add("", int64(0))
	f.Add("not valid!", int64(-1))

	f.Fuzz(func(t *testing.T, value string, expiryDeltaMilliseconds int64) {
		now := time.Unix(1_700_000_000, 0)
		provider := testProvider(now, generatorFunc(func(
			context.Context,
			string,
			aws.Credentials,
		) (string, int64, error) {
			return value,
				now.UnixMilli() + expiryDeltaMilliseconds,
				nil
		}))
		_, _ = provider.Token(context.Background())
	})
}
