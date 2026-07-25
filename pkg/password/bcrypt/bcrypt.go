package bcrypt

import password "github.com/faustbrian/golib/pkg/password"

// New validates cost and limits and constructs a bcrypt-target service. New
// systems should use Argon2id; bcrypt targets exist for controlled compatibility.
func New(cost int, limits password.Limits, options ...password.Option) (*password.Service, error) {
	policy, err := password.NewPolicy(password.PolicyConfig{Algorithm: password.Bcrypt, BcryptCost: cost, Limits: limits})
	if err != nil {
		return nil, err
	}
	return password.New(policy, options...)
}
