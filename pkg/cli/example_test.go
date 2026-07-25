package cli_test

import (
	"bytes"
	"context"
	"fmt"

	cli "github.com/faustbrian/golib/pkg/cli"
)

func ExampleApplication_Run() {
	name := cli.StringArgument("name")
	application, err := cli.Compile(cli.NewCommand(
		"hello",
		cli.WithArguments(name),
		cli.WithHandler(func(_ context.Context, invocation cli.Invocation) error {
			return invocation.Output().SetData("hello " + name.Get(invocation.Input()))
		}),
	))
	if err != nil {
		panic(err)
	}
	stdout := new(bytes.Buffer)
	result := application.Run(context.Background(), cli.Request{
		Args: []string{"Brian"}, Stdout: stdout,
	})
	fmt.Print(stdout.String())
	fmt.Println(result.ExitCode)
	// Output:
	// hello Brian
	// 0
}

func ExampleApplication_Run_json() {
	application, err := cli.Compile(cli.NewCommand(
		"status",
		cli.WithInteraction(cli.InteractionForbidden),
		cli.WithHandler(func(_ context.Context, invocation cli.Invocation) error {
			return invocation.Output().SetData(map[string]string{"status": "ok"})
		}),
	))
	if err != nil {
		panic(err)
	}
	stdout := new(bytes.Buffer)
	application.Run(context.Background(), cli.Request{
		Stdout:         stdout,
		Output:         cli.OutputPolicy{Mode: cli.OutputJSON, NoColor: true},
		NonInteractive: true,
	})
	fmt.Print(stdout.String())
	// Output:
	// {"schema":"go-cli/v1","ok":true,"data":{"status":"ok"}}
}
