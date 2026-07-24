package boundary

import (
	"backend"
	"errors"
	"fmt"
	"wrapper"
)

var errPublic = errors.New("public")

type Service struct{}
type hiddenService struct{}

func Direct() (string, error) {
	return backend.Load() // want `api/backend-error-boundary: backend.Load result 2 crosses exported boundary boundary.Direct`
}

func Assigned() (string, error) {
	value, err := backend.Load()
	if err != nil {
		return "", err // want `api/backend-error-boundary: backend.Load result 2 crosses exported boundary boundary.Assigned`
	}
	return value, nil
}

func Wrapped() (string, error) {
	_, err := backend.Load()
	return "", fmt.Errorf("load: %w", err) // want `api/backend-error-boundary: backend.Load result 2 crosses exported boundary boundary.Wrapped`
}

func Branch(flag bool) (string, error) {
	var err error
	if flag {
		_, err = backend.Load()
	} else {
		err = errPublic
	}
	return "", err // want `api/backend-error-boundary: backend.Load result 2 crosses exported boundary boundary.Branch`
}

func Multiple(flag bool) error {
	var err error
	if flag {
		_, err = backend.Load()
	} else {
		err = backend.Save()
	}
	return err // want `api/backend-error-boundary: backend.Load result 2 crosses exported boundary boundary.Multiple` `api/backend-error-boundary: backend.Save result 1 crosses exported boundary boundary.Multiple`
}

func Concrete() error {
	return backend.Concrete() // want `api/backend-error-boundary: backend.Concrete result 1 crosses exported boundary boundary.Concrete`
}

func InterfaceConversion() error {
	var detailed interface {
		error
		Public()
	} = backend.Concrete()
	return detailed // want `api/backend-error-boundary: backend.Concrete result 1 crosses exported boundary boundary.InterfaceConversion`
}

func Assertion() error {
	_, err := backend.Load()
	return err.(interface { // want `api/backend-error-boundary: backend.Load result 2 crosses exported boundary boundary.Assertion`
		error
		Temporary() bool
	})
}

func MethodWrapper() error {
	_, err := backend.Load()
	return (&wrapper.Wrapper{}).Wrap(err) // want `api/backend-error-boundary: backend.Load result 2 crosses exported boundary boundary.MethodWrapper`
}

func FixedWrapper() error {
	_, err := backend.Load()
	return wrapper.Fixed(errPublic, err)
}

func AllWrapper() error {
	_, err := backend.Load()
	return wrapper.All(err) // want `api/backend-error-boundary: backend.Load result 2 crosses exported boundary boundary.AllWrapper`
}

func (*Service) Load() error {
	return backend.Save() // want `api/backend-error-boundary: backend.Save result 1 crosses exported boundary boundary.Service.Load`
}

func (hiddenService) Load() error { return backend.Save() }

func Dynamic(loader interface{ Load() error }) error { return loader.Load() }

func Literal() error { return func() error { return errPublic }() }

func Method(client *backend.Client) (string, error) {
	return client.Load() // want `api/backend-error-boundary: backend.Client.Load result 2 crosses exported boundary boundary.Method`
}

func Generic() (int, error) {
	return backend.Generic[int]() // want `api/backend-error-boundary: backend.Generic result 2 crosses exported boundary boundary.Generic`
}

func Translated() (string, error) {
	value, err := backend.Load()
	if err != nil {
		return "", translate(err)
	}
	return value, nil
}

func Handled() (string, error) {
	value, err := backend.Load()
	if err != nil {
		return "", errPublic
	}
	return value, nil
}

func Overwritten() (string, error) {
	_, err := backend.Load()
	err = errPublic
	return "", err
}

func Unrelated() (string, error) { return "", errPublic }

func internal() (string, error) { return backend.Load() }

func translate(error) error { return errPublic }
