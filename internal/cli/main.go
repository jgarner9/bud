// Package cli provides CLI functions to run commands and invoke/process the CLI
package cli

import "fmt"

type App struct{}

func NewApp() *App {
	return &App{}
}

func (a *App) Run() {
	fmt.Println("Running bud...")
	fmt.Println("Ctrl + C to stop process...")
}
