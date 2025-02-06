package main

import (
	"context"
	"flag"
	"fmt"
	"fs/internal/cli"
	"fs/internal/config"
	"fs/internal/discover"
	"fs/internal/listener"
	"fs/internal/node"
	"fs/internal/util/logger/handlers/slogpretty"
	"fs/internal/util/logger/sl"
	"fs/internal/watcher"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"
)

const (
	envLocal = "local"
	envDev   = "dev"
	envProd  = "prod"
)

func main() {
	// Загружаем конфигурацию
	cfg := config.MustLoad()

	// Настраиваем логгер
	log := setupLogger(cfg.Env)

	log.Info("starting application",
		slog.String("name", cfg.Name),
		slog.Int("port", cfg.Port),
	)

	// Создаем контекст с отменой для graceful shutdown
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Создаем канал для перехвата сигналов ОС
	signalChanel := make(chan os.Signal, 1)
	signal.Notify(signalChanel, os.Interrupt, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		sig := <-signalChanel
		log.Info("Shutdown signal received", slog.Any("signal", sig))
		cancel()
	}()

	n := node.NewNode(cfg.Name, cfg.Port, log)

	watcherConfig := watcher.Config{
		DebounceDuration: 500 * time.Millisecond,
		IgnorePatterns:   []string{":Zone.Identifier", ".tmp", "~", ".swp", ".swo", ".swx", "#", "#", "/tmp/"},
		Logger:           nil,
	}

	watchPath := "/home/vito/Source/projects/fs/testFolder"
	// watcher, err := watcher.StartWatching("/home/vito/Source/projects/fs/testFolder", n.IndexDB)
	watcher, err := watcher.NewFileWatcher(n.Indexer, watcherConfig)
	if err != nil {
		log.Error("Watcher err", sl.Err(err))
	}
	err = watcher.Watch(watchPath)
	if err != nil {
		watcher.Close()
		fmt.Errorf("failed to watch directory: %w", err)
	}
	n.Watcher = watcher

	go listener.StartListener(ctx, n, cfg.Port, log)
	discover := discover.StartDiscover(ctx, n, cfg.Peers, log)

	cmdContext := cli.NewAppContext(n, discover)

	remainingArgs := flag.Args()
	cli.CliStart(ctx, remainingArgs, cmdContext)

	// Ждем завершения контекста, чтобы программа завершилась корректно
	<-ctx.Done()
	log.Info("Application shutting down gracefully")
}

// func Start(n *node.Node, log *slog.Logger, cfg *config.Config) {

// 	//var wg sync.WaitGroup
// 	//wg.Add(1)
// 	go listener.StartListener(n, cfg.Port, log)
// 	go discover.StartDiscover(n, cfg.Peers, log)
// 	//wg.Wait()
// }

func setupLogger(env string) *slog.Logger {

	var log *slog.Logger

	switch env {
	case envLocal:
		log = setupPrettySlog()
	case envDev:
		log = slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug}))
	case envProd:
		log = slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	}
	return log
}

func setupPrettySlog() *slog.Logger {
	opts := slogpretty.PrettyHandlerOptions{
		SlogOpts: &slog.HandlerOptions{
			Level: slog.LevelDebug,
		},
	}

	handler := opts.NewPrettyHandler(os.Stdout)

	return slog.New(handler)
}
