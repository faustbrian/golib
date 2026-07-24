package policy_test

import (
	"encoding/json"
	"testing"

	"github.com/faustbrian/golib/pkg/authorization/abac"
	"github.com/faustbrian/golib/pkg/authorization/acl"
	"github.com/faustbrian/golib/pkg/authorization/policy"
	"github.com/faustbrian/golib/pkg/authorization/rbac"
)

func BenchmarkCompilerCompile(b *testing.B) {
	compiler, err := policy.NewCompiler(map[policy.Model]policy.Decoder{
		policy.ModelACL: acl.Decoder{}, policy.ModelRBAC: rbac.Decoder{},
		policy.ModelABAC: abac.Decoder{},
	})
	if err != nil {
		b.Fatal(err)
	}
	manifest := policy.Manifest{
		Format: policy.FormatV1, Revision: 1,
		Algorithm: policy.AlgorithmDenyOverrides,
		Policies: []policy.Record{{
			ID: "acl", Revision: 1, Model: policy.ModelACL,
			Document: json.RawMessage(`{"version":1,"entries":[]}`),
		}},
	}
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		if _, err := compiler.Compile(manifest); err != nil {
			b.Fatal(err)
		}
	}
}
