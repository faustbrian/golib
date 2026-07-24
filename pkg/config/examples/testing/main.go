package main

import (
	"context"
	"fmt"

	config "github.com/faustbrian/golib/pkg/config"
	"github.com/faustbrian/golib/pkg/config/configtest"
	"github.com/faustbrian/golib/pkg/config/environment"
)

type settings struct {
	Port int `config:"port" env:"PORT"`
}

func main() {
	source, err := environment.EnvironFor[settings](
		configtest.Environment(map[string]string{"PORT": "8080"}),
		environment.Options{Name: "test-environment"},
	)
	if err != nil {
		panic(err)
	}
	plan, err := config.NewPlan(source)
	if err != nil {
		panic(err)
	}
	snapshot, err := config.Load[settings](context.Background(), plan)
	if err != nil {
		panic(err)
	}
	fmt.Println(snapshot.Value().Port)
}
