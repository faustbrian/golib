package backend

import "errors"

type Client struct{}

type Failure struct{}

func (Failure) Error() string   { return "backend" }
func (Failure) Public()         {}
func (Failure) Temporary() bool { return true }

func Load() (string, error) { return "", errors.New("backend") }

func Save() error { return errors.New("backend") }

func Concrete() Failure { return Failure{} }

func (*Client) Load() (string, error) { return "", errors.New("backend") }

func Generic[T any]() (T, error) {
	var zero T
	return zero, errors.New("backend")
}
