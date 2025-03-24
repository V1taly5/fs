package main

import (
	"context"
	"flag"
	"fmt"
	"fs/internal/cli"
	"os"
	"os/signal"
	"syscall"
)

func main() {

	ctx, cancel := context.WithCancel(context.Background())

	signalChan := make(chan os.Signal, 1)
	signal.Notify(signalChan, os.Interrupt, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		<-signalChan
		fmt.Print("\n Shutdown signal received")
		cancel()
	}()

	cmdContext := cli.NewEmptyAppCtx(cancel)
	remainingArgs := flag.Args()

	cli.CliStart(ctx, remainingArgs, cmdContext)

	<-ctx.Done()
}
