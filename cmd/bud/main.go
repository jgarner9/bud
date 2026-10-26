package main

import (
	"log"

	"jgarner.dev/bud/internal/cli"
)

func main() {
	app := cli.NewApp()
	if err := app.Run(); err != nil {
		// TODO: look into better stderr printing (maybe os.Stderr directly)
		log.Fatal(err.Error())
	}
}
