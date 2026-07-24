package password_test

import (
	"context"
	"fmt"

	"github.com/faustbrian/golib/pkg/keyphrase/password"
)

func ExampleGenerator_Generate() {
	policy := password.Policy{
		Length:   20,
		Alphabet: "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789",
		Required: []password.Class{
			{Name: "lower", Characters: "abcdefghijklmnopqrstuvwxyz"},
			{Name: "upper", Characters: "ABCDEFGHIJKLMNOPQRSTUVWXYZ"},
			{Name: "digits", Characters: "0123456789"},
		},
		MinimumEntropyBits: 100,
	}
	secret, err := password.DefaultGenerator().Generate(context.Background(), policy)
	if err != nil {
		panic(err)
	}
	defer clear(secret)

	fmt.Println(len(secret) == 20)
	// Output: true
}
