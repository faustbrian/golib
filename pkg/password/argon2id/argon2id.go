package argon2id

import password "github.com/faustbrian/golib/pkg/password"

// New validates parameters and limits and constructs an Argon2id service.
func New(parameters password.Argon2idParameters, limits password.Limits, options ...password.Option) (*password.Service, error) {
	policy, err := password.NewPolicy(password.PolicyConfig{Algorithm: password.Argon2id, Argon2id: parameters, Limits: limits})
	if err != nil {
		return nil, err
	}
	return password.New(policy, options...)
}

// NewDefault constructs a service using password.DefaultPolicy.
func NewDefault(options ...password.Option) (*password.Service, error) {
	return password.New(password.DefaultPolicy(), options...)
}
