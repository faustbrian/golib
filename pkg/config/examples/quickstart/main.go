package main

import (
	"context"
	"errors"
	"fmt"

	config "github.com/faustbrian/golib/pkg/config"
	"github.com/faustbrian/golib/pkg/config/defaults"
	"github.com/faustbrian/golib/pkg/config/environment"
	jsonsource "github.com/faustbrian/golib/pkg/config/json"
	"github.com/faustbrian/golib/pkg/config/programmatic"
	"github.com/faustbrian/golib/pkg/config/validation"
)

type settings struct {
	Host  string        `config:"host" env:"HOST" default:"127.0.0.1"`
	Port  int           `config:"port" env:"PORT" default:"8080"`
	Token config.Secret `config:"token,secret" env:"TOKEN"`
}

func main() {
	base := must(defaults.For[settings]("defaults"))
	file := must(jsonsource.Bytes(
		[]byte(`{"host":"service.internal","port":9000}`),
		jsonsource.Options{Name: "config.json"},
	))
	env := must(environment.EnvironFor[settings](
		[]string{"PORT=9443", "TOKEN=not-printed"},
		environment.Options{Name: "environment"},
	))
	override := must(programmatic.Overrides(
		"command-line", map[string]any{"host": "localhost"},
	))
	plan := must(config.NewDefaultPlan(config.DefaultSources{
		Defaults:       []config.Source{base},
		DiscoveredBase: []config.Source{file},
		Environment:    []config.Source{env},
		Overrides:      []config.Source{override},
	}))

	snapshot, err := config.LoadWithValidators(
		context.Background(),
		plan,
		func(_ context.Context, value settings) error {
			if value.Port < 1 || value.Port > 65535 {
				return validation.At("port", errors.New("port outside range"))
			}
			return nil
		},
	)
	if err != nil {
		panic(err)
	}
	value := snapshot.Value()
	origin, _ := snapshot.Origin("port")
	fmt.Printf("%s:%d token=%s port-source=%s\n", value.Host, value.Port, value.Token, origin.Source)
}

func must[T any](value T, err error) T {
	if err != nil {
		panic(err)
	}
	return value
}
