package password_test

import (
	"context"
	"testing"

	password "github.com/faustbrian/golib/pkg/password"
)

func TestMaintainedImplementationVectors(t *testing.T) {
	tests := []struct{ name, password, encoded string }{
		{"argon2id PHC reference CLI", "password", "$argon2id$v=19$m=64,t=1,p=1$c29tZXNhbHQ$ZVrRXqxlLcWfcXCnMyv0m4Rpvh/bnCi7"},
		{"bcrypt Go maintained vector", "allmine", "$2a$10$XajjQvNhvvRt5GSeFk1xFeyqRrsxkhBkUiQeg0dt.wU1qD4aFDcga"},
	}
	limits := password.DefaultPolicy().Limits()
	policy, err := password.NewPolicy(password.PolicyConfig{Algorithm: password.Argon2id, Argon2id: password.Argon2idParameters{Version: 19, Time: 1, MemoryKiB: 64, Parallelism: 1, SaltLength: 8, OutputLength: 24}, Limits: limits})
	if err != nil {
		t.Fatal(err)
	}
	svc, err := password.New(policy)
	if err != nil {
		t.Fatal(err)
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := svc.Verify(context.Background(), []byte(tt.password), tt.encoded)
			if err != nil || !result.Match() {
				t.Fatalf("result=%+v error=%v", result, err)
			}
		})
	}
}
