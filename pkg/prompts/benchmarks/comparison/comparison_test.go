//go:build !windows

package comparison_test

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"testing"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/huh/v2"
	"github.com/AlecAivazis/survey/v2"
	expect "github.com/Netflix/go-expect"
	"github.com/creack/pty"
	prompts "github.com/faustbrian/golib/pkg/prompts"
	terminaladapter "github.com/faustbrian/golib/pkg/prompts/terminal"
	"github.com/hinshun/vt10x"
	"github.com/manifoldco/promptui"
)

const benchmarkAnswer = "Ada"

func BenchmarkInteractiveTextPTY(benchmark *testing.B) {
	engines := map[string]func() (string, error){
		"GoPrompts": func() (string, error) { return runWithPTY(runGoPrompts) },
		"Huh":       func() (string, error) { return runWithPTY(runHuh) },
		"Survey":    runSurveyWithConsole,
		"PromptUI":  func() (string, error) { return runWithPTY(runPromptUI) },
		"Bubbles":   func() (string, error) { return runWithPTY(runBubbles) },
	}
	for name, run := range engines {
		benchmark.Run(name, func(benchmark *testing.B) {
			benchmark.ReportAllocs()
			for benchmark.Loop() {
				answer, err := run()
				if err != nil {
					benchmark.Fatal(err)
				}
				if answer != benchmarkAnswer {
					benchmark.Fatalf("answer = %q", answer)
				}
			}
		})
	}
}

func runWithPTY(run func(*os.File) (string, error)) (string, error) {
	primary, replica, err := pty.Open()
	if err != nil {
		return "", err
	}
	done := startTerminalReactor(primary, benchmarkAnswer+"\r")
	defer func() {
		_ = replica.Close()
		_ = primary.Close()
		<-done
	}()
	if err := pty.Setsize(replica, &pty.Winsize{Rows: 24, Cols: 80}); err != nil {
		return "", err
	}

	return run(replica)
}

func startTerminalReactor(primary *os.File, input string) <-chan struct{} {
	done := make(chan struct{})
	go func() {
		defer close(done)
		buffer := make([]byte, 4096)
		var tail []byte
		answered := false
		prompt := []byte("Name")
		for {
			count, err := primary.Read(buffer)
			if count > 0 {
				chunk := make([]byte, 0, len(tail)+count)
				chunk = append(chunk, tail...)
				chunk = append(chunk, buffer[:count]...)
				if bytes.Contains(chunk, []byte("\x1b[6n")) {
					_, _ = primary.Write([]byte("\x1b[1;1R"))
				}
				if !answered && bytes.Contains(chunk, prompt) {
					_, _ = primary.WriteString(input)
					answered = true
				}
				if len(chunk) >= len(prompt) {
					tail = append(tail[:0], chunk[len(chunk)-len(prompt)+1:]...)
				} else {
					tail = append(tail[:0], chunk...)
				}
			}
			if err != nil {
				return
			}
		}
	}()

	return done
}

func runGoPrompts(file *os.File) (string, error) {
	prompt, err := prompts.NewText(prompts.TextConfig{ID: "name", Label: "Name"})
	if err != nil {
		return "", err
	}
	adapter, err := terminaladapter.New(file, file, terminaladapter.Config{})
	if err != nil {
		return "", err
	}

	return prompts.Run(context.Background(), prompt, prompts.Execution{
		Output: file, Error: file, Events: adapter, Terminal: adapter,
		Capabilities: adapter.Capabilities(),
		Policy: prompts.InteractionPolicy{
			Mode: prompts.InteractiveRequired, PermitInteraction: true,
		},
	})
}

func runHuh(file *os.File) (string, error) {
	var answer string
	form := huh.NewForm(huh.NewGroup(huh.NewInput().Title("Name").Value(&answer))).
		WithInput(file).WithOutput(file).WithWidth(80)
	if err := form.Run(); err != nil {
		return "", err
	}

	return answer, nil
}

func runSurvey(file *os.File) (string, error) {
	var answer string
	err := survey.AskOne(
		&survey.Input{Message: "Name"},
		&answer,
		survey.WithStdio(file, file, file),
	)

	return answer, err
}

func runSurveyWithConsole() (answer string, resultErr error) {
	primary, replica, err := pty.Open()
	if err != nil {
		return "", err
	}
	emulator := vt10x.New(vt10x.WithWriter(replica))
	console, err := expect.NewConsole(
		expect.WithStdin(primary), expect.WithStdout(emulator),
		expect.WithCloser(primary, replica),
	)
	if err != nil {
		return "", err
	}
	defer func() {
		if err := console.Close(); resultErr == nil && err != nil {
			resultErr = err
		}
	}()
	if err := pty.Setsize(console.Tty(), &pty.Winsize{Rows: 24, Cols: 80}); err != nil {
		return "", err
	}
	interaction := make(chan error, 1)
	go func() {
		_, expectErr := console.ExpectString("Name")
		if expectErr == nil {
			_, expectErr = console.SendLine(benchmarkAnswer)
		}
		if expectErr == nil {
			_, expectErr = console.ExpectEOF()
		}
		interaction <- expectErr
	}()
	answer, resultErr = runSurvey(console.Tty())
	if closeErr := console.Tty().Close(); resultErr == nil {
		resultErr = closeErr
	}
	if interactionErr := <-interaction; resultErr == nil {
		resultErr = interactionErr
	}

	return answer, resultErr
}

func runPromptUI(file *os.File) (string, error) {
	prompt := promptui.Prompt{Label: "Name", Stdin: file, Stdout: file}

	return prompt.Run()
}

type bubblesTextModel struct {
	input textinput.Model
}

func newBubblesTextModel() bubblesTextModel {
	input := textinput.New()
	input.Prompt = "Name: "
	_ = input.Focus()

	return bubblesTextModel{input: input}
}

func (model bubblesTextModel) Init() tea.Cmd {
	return nil
}

func (model bubblesTextModel) Update(message tea.Msg) (tea.Model, tea.Cmd) {
	if key, ok := message.(tea.KeyPressMsg); ok && key.Code == tea.KeyEnter {
		return model, tea.Quit
	}
	model.input, _ = model.input.Update(message)

	return model, nil
}

func (model bubblesTextModel) View() tea.View {
	return tea.NewView(model.input.View())
}

func runBubbles(file *os.File) (string, error) {
	program := tea.NewProgram(
		newBubblesTextModel(), tea.WithInput(file), tea.WithOutput(file),
		tea.WithWindowSize(80, 24), tea.WithoutSignals(),
	)
	model, err := program.Run()
	if err != nil {
		return "", err
	}
	result, ok := model.(bubblesTextModel)
	if !ok {
		return "", fmt.Errorf("unexpected model %T", model)
	}

	return result.input.Value(), nil
}
