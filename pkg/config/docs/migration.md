# Migration guide

## From direct `os.Getenv`

Define one root struct at the application composition root. Add `config` tags
for document names and explicit `env` tags for environment names. Replace
scattered reads with one `environment.ProcessFor[T]`, one plan, and one boot
load. Pass the resulting typed value or owned component sub-structs to
constructors.

Before:

```go
port, _ := strconv.Atoi(os.Getenv("PORT"))
token := os.Getenv("TOKEN")
```

After:

```go
type Settings struct {
	Port  int           `config:"port,required" env:"PORT"`
	Token config.Secret `config:"token,required,secret" env:"TOKEN"`
}

source, err := environment.ProcessFor[Settings](environment.Options{
	Name: "environment",
})
```

Handle constructor and load errors at startup. Use explicit test environment
slices instead of `t.Setenv` where possible. Do not retain a global mutable
configuration singleton.

## From Laravel configuration

Laravel commonly combines PHP config files, `.env`, `env()`, service-container
lookups, and runtime `config()` mutation. Translate the final public contract,
not executable PHP behavior:

1. Define caller-owned Go structs for application settings and package-owned
   sub-structs for reusable integrations.
2. Move static PHP defaults to typed `default` tags or a programmatic defaults
   source.
3. Translate deployable config files to strict JSON, YAML, or TOML.
4. Map `.env` only when explicitly requested; keep process environment above it
   in `NewDefaultPlan`.
5. Replace `config('path')` reads with typed field access.
6. Replace `config([...])` runtime writes with explicit immutable overrides
   before loading or with application state outside configuration.
7. Replace service-container resolution with ordinary constructor arguments.
8. Add validators for cross-field rules that Laravel config previously assumed.

PHP config files can execute code; this library deliberately cannot. Compute
dynamic values in the composition root and pass them through a programmatic
source. Preserve secret ownership in deployment tooling rather than committing
plaintext translations.

During migration, compare a sanitized field inventory and provenance, never a
dump containing values. Roll out with the old and new loaders reading the same
inputs only if the old path can be observed safely; publish one authoritative
configuration to consumers.
