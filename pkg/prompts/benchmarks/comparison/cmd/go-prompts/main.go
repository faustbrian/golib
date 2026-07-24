package main

import prompts "github.com/faustbrian/golib/pkg/prompts"

func main() {
	_, _ = prompts.NewText(prompts.TextConfig{ID: "name", Label: "Name"})
}
