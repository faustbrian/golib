package configservice_test

import (
	"context"
	"fmt"

	"github.com/faustbrian/golib/pkg/config"
	"github.com/faustbrian/golib/pkg/config/configservice"
	"github.com/faustbrian/golib/pkg/config/programmatic"
	"github.com/faustbrian/golib/pkg/service"
)

func ExampleNew() {
	defaults, _ := programmatic.Defaults(
		"defaults",
		map[string]any{"port": int64(8080)},
	)
	loader, _ := configservice.New(configservice.Options[settings]{
		Sources: config.DefaultSources{
			Defaults: []config.Source{defaults},
		},
	})
	command := service.CommandFor(service.CommandSpec[settings]{
		Name: "serve",
		Kind: service.CommandKindLongRunning,
		Load: loader,
		Build: func(context.Context, service.BuildContext, settings) (service.Plan, error) {
			return service.Plan{}, nil
		},
	})
	_ = command

	loaded, _ := loader(context.Background(), service.Invocation{})
	fmt.Println(loaded.Port)
	// Output: 8080
}
