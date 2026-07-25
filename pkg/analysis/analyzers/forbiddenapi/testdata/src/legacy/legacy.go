package legacy

func Old() {}

func Generic[T any](T) {}

func GenericPair[T, U any](T, U) {}

func Current() {}

type Client struct{}

func (*Client) Call() {}

func (Client) Current() {}
