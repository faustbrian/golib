package main

import "charm.land/huh/v2"

func main() {
	var answer string
	_ = huh.NewForm(huh.NewGroup(huh.NewInput().Title("Name").Value(&answer)))
}
