package main

import (
	"fmt"
	"fs/pkg/cli"
)

func main() {
	CLI := cli.NewCLI()
	CLI.RegisterPlugin(&cli.GreetCommand{})
	if err := CLI.Run(); err != nil {
		fmt.Printf("Error: %v\n", err)
	}
}
