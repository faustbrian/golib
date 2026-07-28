# config

`config` loads explicit, layered configuration sources into caller-owned Go
structs. It provides deterministic precedence, strict decoding, immutable
snapshots, safe provenance, validation orchestration, and redacted secrets
without introducing global state or implicit filesystem discovery.

Requires Go 1.25 or newer.

## Install

```console
go get github.com/faustbrian/golib/pkg/config
```

## Five-minute quickstart

```go
package main

import (
	"context"
	"fmt"

	config "github.com/faustbrian/golib/pkg/config"
	"github.com/faustbrian/golib/pkg/config/defaults"
	"github.com/faustbrian/golib/pkg/config/environment"
	jsonsource "github.com/faustbrian/golib/pkg/config/json"
	"github.com/faustbrian/golib/pkg/config/programmatic"
)

type Settings struct {
	Host  string        `config:"host" env:"HOST" default:"127.0.0.1"`
	Port  int           `config:"port" env:"PORT" default:"8080"`
	Token config.Secret `config:"token,secret" env:"TOKEN"`
}

func main() {
	base, _ := defaults.For[Settings]("defaults")
	file, _ := jsonsource.Bytes(
		[]byte(`{"host":"service.internal","port":9000}`),
		jsonsource.Options{Name: "config.json"},
	)
	env, _ := environment.EnvironFor[Settings](
		[]string{"PORT=9443", "TOKEN=not-printed"},
		environment.Options{Name: "environment"},
	)
	override, _ := programmatic.Overrides(
		"command-line", map[string]any{"host": "localhost"},
	)

	plan, _ := config.NewDefaultPlan(config.DefaultSources{
		Defaults: []config.Source{base},
		DiscoveredBase: []config.Source{file},
		Environment: []config.Source{env},
		Overrides: []config.Source{override},
	})
	snapshot, err := config.Load[Settings](context.Background(), plan)
	if err != nil {
		panic(err)
	}

	settings := snapshot.Value()
	fmt.Printf("%s:%d token=%s\n", settings.Host, settings.Port, settings.Token)
	// localhost:9443 token=[REDACTED]
}
```

All constructors return errors; production code should handle them. The
omitted checks above keep the precedence example compact. A complete version is
in [examples/quickstart](examples/quickstart/main.go).

## Service command loading

`configservice.New` adapts a typed plan to `service.CommandSpec.Load`. It loads
only after command selection and before component construction. Local dotenv
files require the explicit `Local` option; process environment and caller
overrides retain the standard precedence.

```go
loader, err := configservice.New(configservice.Options[Settings]{
    Local: true,
    Dotenv: &configservice.Dotenv{
        FS: os.DirFS("."),
        Path: ".env",
        Options: dotenv.Options{Name: "local-dotenv", Prefix: "APP_"},
    },
    Environment: &environment.Options{
        Name: "process-environment",
        Prefix: "APP_",
    },
})
if err != nil {
    return err
}

command := service.CommandFor(service.CommandSpec[Settings]{
    Name: "serve",
    Kind: service.CommandKindLongRunning,
    Load: loader,
    Build: build,
})
```

The adapter owns no resource, performs no retries, and does not reload
configuration. The caller owns every source and decides whether repeated loads
are safe.

## What is included

- Strict JSON, YAML, TOML, dotenv, environment, map, byte, reader, `fs.FS`, and
  explicit-file sources.
- Explicit bounded discovery with search-first/search-all, roots, stop
  directories, symlink policy, and file-permission policy.
- Object merge, scalar/slice replacement, explicit null and deletion, and type
  conflict rejection.
- `Optional[T]`, `Secret`, `ByteSize`, durations, URLs, timestamps, enums, and
  text-unmarshaling types.
- Struct-tag defaults, bounded dotenv interpolation, self-validation, typed
  validators, and deterministic error aggregation.
- Immutable typed snapshots and field-level provenance that never stores field
  values.
- A typed `configservice` adapter for command-scoped service configuration,
  explicit local dotenv loading, and process-environment orchestration.

## Documentation

- [API reference](docs/api.md)
- [Sources and formats](docs/sources.md)
- [Structured-format conformance matrix](docs/conformance.md)
- [Layering, defaults, merging, interpolation, and validation](docs/layering.md)
- [Discovery](docs/discovery.md)
- [Security and threat model](docs/security.md)
- [Kubernetes and Infisical recipes](docs/kubernetes.md)
- [Operations, compatibility, performance, troubleshooting, and FAQ](docs/operations.md)
- [Migration from Laravel configuration and `os.Getenv`](docs/migration.md)
- [Reusable configuration types for package authors](docs/package-authors.md)
- [Runnable examples](docs/examples.md)
- [Hardening evidence and findings](docs/hardening.md)
- [Hardening audit traceability](docs/audit-evidence.md)

## Design boundaries

The package does not load automatically, mutate process environment, traverse
parents by default, execute configuration code, hot-reload snapshots, manage
secrets, or define vendor credential structs. Kubernetes applications should
normally receive Infisical values through the Operator, CSI, or Agent and load
the resulting environment variables or files normally.

## Development

```console
make check
```

The release gate enforces race tests, exact 100% production statement coverage,
all fuzz targets, benchmarks, docs/examples, API compatibility, vetting,
low-level Go safety, and vulnerability scanning. See
[CONTRIBUTING.md](CONTRIBUTING.md).

## License

MIT. See [LICENSE](LICENSE).
