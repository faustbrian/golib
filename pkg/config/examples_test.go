package config_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"testing/fstest"

	config "github.com/faustbrian/golib/pkg/config"
	"github.com/faustbrian/golib/pkg/config/configtest"
	"github.com/faustbrian/golib/pkg/config/defaults"
	"github.com/faustbrian/golib/pkg/config/discover"
	"github.com/faustbrian/golib/pkg/config/dotenv"
	"github.com/faustbrian/golib/pkg/config/environment"
	"github.com/faustbrian/golib/pkg/config/filesystem"
	jsonsource "github.com/faustbrian/golib/pkg/config/json"
	"github.com/faustbrian/golib/pkg/config/programmatic"
	tomlsource "github.com/faustbrian/golib/pkg/config/toml"
	"github.com/faustbrian/golib/pkg/config/validation"
	yamlsource "github.com/faustbrian/golib/pkg/config/yaml"
)

func Example_structuredSources() {
	files := fstest.MapFS{
		"settings.json": &fstest.MapFile{Data: []byte(`{"json_fs":"loaded"}`)},
		"settings.yaml": &fstest.MapFile{Data: []byte("yaml_fs: loaded\n")},
		"settings.toml": &fstest.MapFile{Data: []byte(`toml_fs = "loaded"`)},
	}
	jsonBytes, _ := jsonsource.Bytes(
		[]byte(`{"json_bytes":"loaded"}`),
		jsonsource.Options{Name: "json-bytes"},
	)
	jsonFS, _ := jsonsource.FromFS(
		files,
		"settings.json",
		jsonsource.Options{Name: "json-fs"},
	)
	yamlBytes, _ := yamlsource.Bytes(
		[]byte("yaml_bytes: loaded\n"),
		yamlsource.Options{Name: "yaml-bytes"},
	)
	yamlFS, _ := yamlsource.FromFS(
		files,
		"settings.yaml",
		yamlsource.Options{Name: "yaml-fs"},
	)
	tomlBytes, _ := tomlsource.Bytes(
		[]byte(`toml_bytes = "loaded"`),
		tomlsource.Options{Name: "toml-bytes"},
	)
	tomlFS, _ := tomlsource.FromFS(
		files,
		"settings.toml",
		tomlsource.Options{Name: "toml-fs"},
	)
	plan, _ := config.NewPlan(jsonBytes, jsonFS, yamlBytes, yamlFS, tomlBytes, tomlFS)
	snapshot, _ := config.LoadTree(context.Background(), plan)
	value := snapshot.Value()
	fmt.Println(
		value["json_bytes"], value["json_fs"],
		value["yaml_bytes"], value["yaml_fs"],
		value["toml_bytes"], value["toml_fs"],
	)
	// Output: loaded loaded loaded loaded loaded loaded
}

func Example_dotenvSources() {
	type settings struct {
		FromBytes string `config:"from_bytes" env:"FROM_BYTES"`
		FromFS    string `config:"from_fs" env:"FROM_FS"`
	}
	files := fstest.MapFS{
		"settings.env": &fstest.MapFile{Data: []byte("APP_FROM_FS=${VALUE:-filesystem}\n")},
	}
	fromBytes, _ := dotenv.BytesFor[settings](
		[]byte("APP_FROM_BYTES=${VALUE:-bytes}\n"),
		dotenv.Options{
			Name: "dotenv-bytes", Prefix: "APP_",
			Interpolation: &dotenv.Interpolation{IncludeFile: true},
		},
	)
	fromFS, _ := dotenv.FromFSFor[settings](
		files,
		"settings.env",
		dotenv.Options{
			Name: "dotenv-fs", Prefix: "APP_",
			Interpolation: &dotenv.Interpolation{IncludeFile: true},
		},
	)
	plan, _ := config.NewPlan(fromBytes, fromFS)
	snapshot, _ := config.Load[settings](context.Background(), plan)
	fmt.Println(snapshot.Value().FromBytes, snapshot.Value().FromFS)
	// Output: bytes filesystem
}

func Example_environmentSources() {
	type settings struct {
		Explicit string `config:"explicit" env:"EXPLICIT"`
		Process  string `config:"process" env:"PROCESS"`
	}
	const processName = "GO_CONFIG_EXAMPLE_PROCESS"
	previous, existed := os.LookupEnv(processName)
	_ = os.Setenv(processName, "process")
	defer func() {
		if existed {
			_ = os.Setenv(processName, previous)
		} else {
			_ = os.Unsetenv(processName)
		}
	}()

	explicit, _ := environment.EnvironFor[settings](
		[]string{"GO_CONFIG_EXAMPLE_EXPLICIT=explicit"},
		environment.Options{Name: "explicit", Prefix: "GO_CONFIG_EXAMPLE_"},
	)
	process, _ := environment.ProcessFor[settings](environment.Options{
		Name: "process", Prefix: "GO_CONFIG_EXAMPLE_",
	})
	plan, _ := config.NewPlan(explicit, process)
	snapshot, _ := config.Load[settings](context.Background(), plan)
	fmt.Println(snapshot.Value().Explicit, snapshot.Value().Process)
	// Output: explicit process
}

func Example_defaultsAndProgrammaticSources() {
	type settings struct {
		Name   string `config:"name" default:"default"`
		Region string `config:"region"`
		Port   int    `config:"port"`
	}
	typedDefaults, _ := defaults.For[settings]("typed-defaults")
	mapDefaults, _ := programmatic.Defaults(
		"map-defaults",
		map[string]any{"region": "eu"},
	)
	middle, _ := programmatic.Map(
		"runtime",
		config.PriorityExplicitFiles,
		map[string]any{"port": int64(8080)},
	)
	overrides, _ := programmatic.Overrides(
		"overrides",
		map[string]any{"name": "worker"},
	)
	plan, _ := config.NewPlan(overrides, middle, mapDefaults, typedDefaults)
	snapshot, _ := config.Load[settings](context.Background(), plan)
	value := snapshot.Value()
	fmt.Println(value.Name, value.Region, value.Port)
	// Output: worker eu 8080
}

func Example_filesystemSources() {
	files := fstest.MapFS{
		"embedded.json": &fstest.MapFile{Data: []byte(`{"embedded":"loaded"}`)},
	}
	fromFS, _ := filesystem.FromFS(
		files,
		"embedded.json",
		filesystem.Options{Name: "embedded"},
	)
	reader, _ := filesystem.Reader(
		func(context.Context) (io.ReadCloser, error) {
			return io.NopCloser(bytes.NewBufferString("reader: loaded\n")), nil
		},
		filesystem.Options{Name: "reader", Format: filesystem.FormatYAML},
	)

	directory, _ := os.MkdirTemp("", "config-example-")
	defer func() { _ = os.RemoveAll(directory) }()
	path := filepath.Join(directory, "settings.toml")
	_ = os.WriteFile(path, []byte(`path = "loaded"`), 0o600)
	fromPath, _ := filesystem.FromPath(path, filesystem.Options{Name: "path"})
	fromDiscovery, _ := filesystem.FromDiscovered(
		discover.Result{Path: path, ResolvedPath: path},
		filesystem.Options{Name: "discovered"},
	)

	plan, _ := config.NewPlan(fromFS, reader, fromPath, fromDiscovery)
	snapshot, _ := config.LoadTree(context.Background(), plan)
	value := snapshot.Value()
	fmt.Println(value["embedded"], value["reader"], value["path"])
	// Output: loaded loaded loaded
}

func Example_discoveryAndValidation() {
	directory, _ := os.MkdirTemp("", "config-discovery-")
	defer func() { _ = os.RemoveAll(directory) }()
	path := filepath.Join(directory, "app.yaml")
	_ = os.WriteFile(path, []byte("port: 8080\n"), 0o600)
	permissions := discover.OwnerOnly
	if runtime.GOOS == "windows" {
		permissions = discover.IgnorePermissions
	}
	results, _ := discover.Search(context.Background(), discover.Options{
		Root: directory, Directories: []string{directory},
		SearchPlaces: []string{"app.yaml"}, Mode: discover.SearchFirst,
		Symlinks: discover.RejectSymlinks, Permissions: permissions,
	})
	source, _ := filesystem.FromDiscovered(
		results[0],
		filesystem.Options{Name: "discovered", Priority: config.PriorityDiscoveredBase},
	)
	type settings struct {
		Port int `config:"port,required"`
	}
	plan, _ := config.NewPlan(source)
	snapshot, _ := config.LoadWithValidators(
		context.Background(),
		plan,
		func(_ context.Context, value settings) error {
			if value.Port < 1 || value.Port > 65535 {
				return validation.At("port", errors.New("port outside supported range"))
			}
			return nil
		},
	)
	fmt.Println(snapshot.Value().Port, results[0].SearchPlace)
	// Output: 8080 app.yaml
}

func Example_secretsProvenanceAndConfigtest() {
	type settings struct {
		Token config.Secret `config:"token,secret"`
	}
	fixture := configtest.NewSource(
		config.SourceInfo{Name: "fixture", Sensitive: true},
		config.Document{Tree: map[string]any{"token": "secret-value"}},
	)
	plan, _ := config.NewPlan(fixture)
	snapshot, _ := config.Load[settings](context.Background(), plan)
	origin, _ := snapshot.Origin("token")

	environmentFixture := configtest.Environment(map[string]string{"B": "2", "A": "1"})
	filesystemFixture := configtest.Filesystem(map[string]string{"config.json": `{}`})
	_, openErr := filesystemFixture.Open("config.json")
	failure := errors.New("fixture failure")
	failingPlan, _ := config.NewPlan(configtest.FailingSource(
		config.SourceInfo{Name: "failure"},
		failure,
	))
	_, loadErr := config.LoadTree(context.Background(), failingPlan)

	fmt.Println(snapshot.Value().Token, origin.Sensitive)
	fmt.Println(environmentFixture, openErr == nil, errors.Is(loadErr, failure))
	// Output:
	// [REDACTED] true
	// [A=1 B=2] true true
}
