# validation

`validation` is a typed, transport-neutral validation package for Go 1.26
and later. Ordinary functions and `Validator[T]` are the primary API. Reports
retain stable paths and rule codes without retaining rejected values.

## Five-minute quickstart

```go
package main

import (
	"fmt"

	validation "github.com/faustbrian/golib/pkg/validation"
	"github.com/faustbrian/golib/pkg/validation/rules"
)

func main() {
	ctx, _ := validation.NewContext(validation.DefaultLimits())
	validator := validation.All(validation.CollectAll,
		rules.RuneLength(3, 40),
		rules.Prefix("usr_"),
	)
	report := validator.Validate(ctx.WithPath(validation.Field("username")), "x")
	for _, violation := range report.Violations() {
		fmt.Println(violation.Path(), violation.Code())
	}
	// Output:
	// username rune_length
	// username prefix
}
```

Use `validation.Value[T]` when input presence matters:

```go
missing := validation.Missing[string]()
null := validation.Null[string]()
empty := validation.Present("")
_ = []validation.Value[string]{missing, null, empty}
```

Core validators never perform I/O. Use `AsyncValidator[T]` and `AsyncAll` for
context-aware external checks. Reflection is optional and isolated in
`structplan`; typed plans require no tags or registry.

## Documentation

- [Documentation index](docs/README.md)
- [API and packages](docs/api.md)
- [Rule catalog](docs/rules.md)
- [Normative semantics](docs/semantics.md)
- [Guides](docs/guides.md)
- [Security model](docs/security.md)
- [Hardening evidence](docs/hardening-report.md)
- [Laravel and cline/struct adoption](docs/adoption.md)
- [Compatibility](docs/compatibility.md)

## Local verification

```sh
make check
```

Every blocking CI command has a local Make target. `make nilaway` is advisory.
Hosted CI is a release integrator's final external verification step, not a
prerequisite for local development.

## License

MIT. See [LICENSE](LICENSE).
