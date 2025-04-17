package main

import (
	"fmt"
	"fs/pkg/cli"
)

func main() {

	CLI := cli.NewCLI()
	CLI.RegisterPlugin(&cli.GreetCommand{})
	if err := CLI.RunCLI(); err != nil {
		fmt.Printf("Error: %v\n", err)
	}
}
