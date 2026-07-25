# Package-author guide

Reusable packages should define only the configuration they own. A PostgreSQL
package may own pool and TLS settings; a telemetry package may own exporter
settings. Core `config` intentionally does not define vendor, database,
queue, cloud, or authentication credential catalogs.

Use `config` tags for format-independent field names. Add `env` tags only when a
stable environment contract belongs to the type. Mark required/deprecated/
secret metadata in the config tag and provide conservative typed defaults only
when they are valid for every caller.

```go
type ClientSettings struct {
	Endpoint *url.URL    `config:"endpoint,required" env:"ENDPOINT"`
	Timeout  time.Duration `config:"timeout" env:"TIMEOUT" default:"5s"`
	Token    config.Secret `config:"token,required,secret" env:"TOKEN"`
}

func (s ClientSettings) Validate() error {
	if s.Timeout <= 0 {
		return validation.At("timeout", errors.New("must be positive"))
	}
	return nil
}
```

Custom decoder and validation cause text is redacted throughout the library
error chain, and sentinel identity remains available through `errors.Is`.
Still return value-free errors because application code may inspect them before
returning them to this library. Prefer `encoding.TextUnmarshaler` for scalar
domain types and a dedicated enum type for closed sets. Scalar types with
private mutable reference fields cannot be copied safely and are rejected
before snapshot publication. Hooks must be deterministic,
cancellation-independent, panic-free, and must not perform I/O.

Do not load configuration inside a reusable package, call `os.Getenv`, register
global state in `init`, or import application root settings. Applications own
source selection and layering, then pass `ClientSettings` to the package.

Test the type with `defaults.For`, `environment.EnvironFor`, strict decode,
validation, malformed text, redaction, and snapshot copying. A package adding a
remote source should implement `config.Source` in a separate optional module and
document limits, retries, cancellation, caching, staleness, and shutdown.
