package resourceapi

type Resource struct{}

type Manager struct{}

func Open() (*Resource, func(), error) {
	return &Resource{}, func() {}, nil
}

func OpenFor[T any]() (*Resource, func(), error) {
	return Open()
}

func OpenPair[T, U any]() (*Resource, func(), error) {
	return Open()
}

func (Manager) Open() (*Resource, func(), error) {
	return Open()
}

func Other() (*Resource, func(), error) {
	return Open()
}
