package wrapper

type Wrapper struct{}

func (*Wrapper) Wrap(err error) error { return err }

func Fixed(first, _ error) error { return first }

func All(err error) error { return err }
