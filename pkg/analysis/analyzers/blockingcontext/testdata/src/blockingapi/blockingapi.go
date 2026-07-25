package blockingapi

import contextalias "context"

type Context = contextalias.Context

func Fetch[T any](value T) error { // want `context/blocking-api-context: configured blocking API Fetch requires context.Context`
	return nil
}

type Client struct{}

func (*Client) Load(key string) error { // want `context/blocking-api-context: configured blocking API Client.Load requires context.Context`
	return nil
}

func Store(ctx Context, value string) error {
	return nil
}

func Helper(value string) error {
	return nil
}
