package configservice_test

import (
	"context"
	"errors"
	"testing"
	"testing/fstest"

	"github.com/faustbrian/golib/pkg/config"
	"github.com/faustbrian/golib/pkg/config/configservice"
	"github.com/faustbrian/golib/pkg/config/dotenv"
	"github.com/faustbrian/golib/pkg/config/environment"
	"github.com/faustbrian/golib/pkg/config/programmatic"
	"github.com/faustbrian/golib/pkg/config/validation"
	"github.com/faustbrian/golib/pkg/service"
)

type settings struct {
	Port int `config:"port"`
}

func TestLoaderAppliesLocalDotenvEnvironmentAndOverridePrecedence(t *testing.T) {
	t.Setenv("APP_PORT", "3000")
	defaults, err := programmatic.Defaults("defaults", map[string]any{"port": int64(1000)})
	if err != nil {
		t.Fatal(err)
	}
	override, err := programmatic.Overrides("override", map[string]any{"port": int64(4000)})
	if err != nil {
		t.Fatal(err)
	}
	loader, err := configservice.New(configservice.Options[settings]{
		Sources: config.DefaultSources{
			Defaults:  []config.Source{defaults},
			Overrides: []config.Source{override},
		},
		Local: true,
		Dotenv: &configservice.Dotenv{
			FS:   fstest.MapFS{".env": {Data: []byte("APP_PORT=2000\n")}},
			Path: ".env",
			Options: dotenv.Options{
				Name: "local-dotenv", Prefix: "APP_",
			},
		},
		Environment: &environment.Options{
			Name: "process-environment", Prefix: "APP_",
		},
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	loaded, err := loader(context.Background(), service.Invocation{})
	if err != nil {
		t.Fatalf("loader() error = %v", err)
	}
	if loaded.Port != 4000 {
		t.Fatalf("Port = %d, want 4000", loaded.Port)
	}
}

func TestLoaderUsesEnvironmentOverLocalDotenv(t *testing.T) {
	t.Setenv("APP_PORT", "3000")
	loader, err := configservice.New(configservice.Options[settings]{
		Local: true,
		Dotenv: &configservice.Dotenv{
			FS:   fstest.MapFS{".env": {Data: []byte("APP_PORT=2000\n")}},
			Path: ".env",
			Options: dotenv.Options{
				Name: "local-dotenv", Prefix: "APP_",
			},
		},
		Environment: &environment.Options{
			Name: "process-environment", Prefix: "APP_",
		},
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	loaded, err := loader(context.Background(), service.Invocation{})
	if err != nil {
		t.Fatalf("loader() error = %v", err)
	}
	if loaded.Port != 3000 {
		t.Fatalf("Port = %d, want 3000", loaded.Port)
	}
}

func TestNewRejectsDotenvOutsideExplicitLocalMode(t *testing.T) {
	_, err := configservice.New(configservice.Options[settings]{
		Dotenv: &configservice.Dotenv{
			FS:      fstest.MapFS{},
			Path:    ".env",
			Options: dotenv.Options{Name: "local-dotenv"},
		},
	})
	if !errors.Is(err, configservice.ErrInvalidOptions) {
		t.Fatalf("New() error = %v, want ErrInvalidOptions", err)
	}
}

func TestNewRejectsInvalidSourceConstruction(t *testing.T) {
	tests := []struct {
		name       string
		options    configservice.Options[settings]
		wantReason string
	}{
		{
			name: "dotenv",
			options: configservice.Options[settings]{
				Local: true,
				Dotenv: &configservice.Dotenv{
					FS: fstest.MapFS{}, Path: "", Options: dotenv.Options{Name: "local-dotenv"},
				},
			},
			wantReason: "requires a filesystem and path",
		},
		{
			name: "dotenv filesystem",
			options: configservice.Options[settings]{
				Local: true,
				Dotenv: &configservice.Dotenv{
					Path: ".env", Options: dotenv.Options{Name: "local-dotenv"},
				},
			},
			wantReason: "requires a filesystem and path",
		},
		{
			name: "dotenv options",
			options: configservice.Options[settings]{
				Local: true,
				Dotenv: &configservice.Dotenv{
					FS: fstest.MapFS{".env": {Data: nil}}, Path: ".env",
				},
			},
		},
		{
			name: "environment",
			options: configservice.Options[settings]{
				Environment: &environment.Options{},
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := configservice.New(test.options)
			if !errors.Is(err, configservice.ErrInvalidOptions) {
				t.Fatalf("New() error = %v, want ErrInvalidOptions", err)
			}
			var optionsError *configservice.OptionsError
			if !errors.As(err, &optionsError) || optionsError.Error() == "" {
				t.Fatalf("New() error = %v, want safe OptionsError", err)
			}
			if test.wantReason != "" && optionsError.Reason != test.wantReason {
				t.Fatalf("OptionsError.Reason = %q, want %q", optionsError.Reason, test.wantReason)
			}
		})
	}
}

func TestOptionsErrorUnwrapIncludesOnlyPresentCause(t *testing.T) {
	t.Parallel()

	withoutCause := (&configservice.OptionsError{}).Unwrap()
	if len(withoutCause) != 1 || !errors.Is(withoutCause[0], configservice.ErrInvalidOptions) {
		t.Fatalf("Unwrap(without cause) = %#v", withoutCause)
	}

	cause := errors.New("construction failure")
	withCause := (&configservice.OptionsError{Cause: cause}).Unwrap()
	if len(withCause) != 2 || !errors.Is(withCause[1], cause) {
		t.Fatalf("Unwrap(with cause) = %#v", withCause)
	}
}

func TestNewRejectsInvalidSourcePlan(t *testing.T) {
	source, err := programmatic.Defaults("duplicate", map[string]any{"port": int64(1000)})
	if err != nil {
		t.Fatal(err)
	}
	_, err = configservice.New(configservice.Options[settings]{
		Sources: config.DefaultSources{
			Defaults: []config.Source{source, source},
		},
	})
	if !errors.Is(err, configservice.ErrInvalidOptions) {
		t.Fatalf("New() error = %v, want ErrInvalidOptions", err)
	}
	var optionsError *configservice.OptionsError
	if !errors.As(err, &optionsError) || optionsError.Field != "Sources" {
		t.Fatalf("New() error = %#v, want Sources OptionsError", err)
	}
}

func TestLoaderPreservesCancellationAndValidationCauses(t *testing.T) {
	validationCause := errors.New("invalid port")
	loader, err := configservice.New(configservice.Options[settings]{
		Validators: []validation.Validator[settings]{
			func(context.Context, settings) error {
				return validation.At("port", validationCause)
			},
		},
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if _, err := loader(context.Background(), service.Invocation{}); !errors.Is(err, validationCause) {
		t.Fatalf("loader() error = %v, want validation cause", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := loader(ctx, service.Invocation{}); !errors.Is(err, context.Canceled) {
		t.Fatalf("loader() error = %v, want context.Canceled", err)
	}
}
