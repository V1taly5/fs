package main

import (
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"log/slog"

	cliplugins "fs/internal/cli_plugins"
	"fs/internal/config"
	connectionmanager "fs/internal/connection_manager"
	discoverymanager "fs/internal/discovery_manager"
	discoverymodels "fs/internal/discovery_manager/models"
	messagerouter "fs/internal/message_router"
	"fs/internal/node"
	nodestorage "fs/internal/storage/node_storage"
	"fs/internal/util/logger/handlers/slogpretty"
	"fs/internal/util/logger/sl"
	"fs/internal/watcher"
	"fs/pkg/cli"
)

const (
	envLocal = "local"
	envDev   = "dev"
	envProd  = "prod"
)

func main() {
	file, err := os.OpenFile("log/app.log", os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0666)
	if err != nil {
		log.Fatalf("Failed to open log file: %v", err)
	}
	defer file.Close()
	// Загружаем конфигурацию
	cfg := config.MustLoad()

	// Настраиваем логгер
	log := setupLogger(cfg.Env, file)

	log.Info("starting application",
		slog.String("name", cfg.Name),
		slog.Int("port", cfg.Port),
	)

	// Создаем контекст с отменой для graceful shutdown
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Создаем канал для перехвата сигналов ОС
	signalChannel := make(chan os.Signal, 1)
	signal.Notify(signalChannel, os.Interrupt, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		sig := <-signalChannel
		log.Info("Shutdown signal received", slog.Any("signal", sig))
		cancel()
	}()

	// Создаем узел
	n := node.NewNode(cfg.Name, cfg.Port, log)

	watcherConfig := watcher.Config{
		DebounceDuration: 500 * time.Millisecond,
		IgnorePatterns:   []string{":Zone.Identifier", ".tmp", "~", ".swp", ".swo", ".swx", "#", "#", "/tmp/"},
		Logger:           nil,
	}

	dir, err := os.Getwd()
	if err != nil {
		return
	}

	watchPath := dir + "/testFolder"
	fmt.Println(watchPath)

	watcher, err := watcher.NewFileWatcher(n.Indexer, watcherConfig)
	if err != nil {
		return
	}

	err = watcher.Watch(watchPath)
	if err != nil {
		return
	}
	n.Watcher = watcher

	// Конфигурация для обнаружения пиров
	discoveryConfig := &discoverymodels.PeerDiscoveryConfig{
		MulticastAddress:   "239.0.0.1:9999", // Пример multicast-адреса
		DiscoveryInterval:  5 * time.Second,
		ConnectionTimeout:  3 * time.Second,
		AutoconnectEnabled: false,
	}

	storageConfig := nodestorage.DefaultConfig()
	storage, err := nodestorage.New(storageConfig, log)
	if err != nil {
		log.Error("failed init node storage")
		return
	}

	// Создаем менеджер обнаружения пиров
	discoveryManager := discoverymanager.NewPeerDiscoveryManager(ctx, n, discoveryConfig, storage, log)

	// Запускаем механизмы обнаружения
	discoveryManager.Start()

	log.Info("Peer discovery manager started")

	connectionConfig := &connectionmanager.ConnectionConfig{
		ConnectionTimeout: time.Second * 30,
		BaseRetryInterval: time.Second * 1,
		MaxRetryInterval:  time.Second * 60,
		MaxRetryCount:     5,
		AutoStartProcess:  true,
	}

	connectionManager := connectionmanager.NewConnectionManager(ctx, n, storage, connectionConfig, log, cfg.Port)

	time.Sleep(9 * time.Second)

	message_router := messagerouter.NewMessageRouter(n, n.Indexer, connectionManager, log)

	// handler := handlers.NewHadler(connectionManager, n, log)
	// handler.RegisterStandardHandlers()

	retryOptions := &connectionmanager.RetryOptions{
		MaxAttempts:    3,
		UseExponential: true,
		InitialDelay:   time.Second,
		MaxDelay:       time.Second * 10,
	}
	CLI := cli.NewCLI(ctx)
	connCmd := cliplugins.NewConnectionCommand(connectionManager)
	sendCmd := cliplugins.NewSenderCommand(message_router)
	CLI.RegisterPlugin(&cli.GreetCommand{})
	CLI.RegisterPlugin(sendCmd)
	CLI.RegisterPlugin(connCmd)
	if err := CLI.RunCLI(); err != nil {
		fmt.Printf("Error: %v\n", err)
	}

	node, err := storage.GetNode(ctx, "d9c8327530a1527c5eb755f0c92bfa693012b8f7a7737594f92f647e4dd28427")
	if err != nil {
		log.Error("failed to get node for ID", sl.Err(err))
		return
	}

	_, err = connectionManager.ConnectToEndpoint(ctx, node.Endpoints[0], retryOptions)
	if err != nil {
		log.Error("failed to create Connection to Node endpoint", sl.Err(err))
		return
	}

	// go listener.StartListener(ctx, n, cfg.Port, log)

	// Graceful shutdown
	defer func() {
		log.Info("Shutting down peer discovery manager...")
		discoveryManager.Shutdown()
		log.Info("Peer discovery manager stopped")
	}()

	// Основной цикл приложения
	<-ctx.Done()
	log.Info("Application shutdown complete")
}

func setupLogger(env string, writer io.Writer) *slog.Logger {

	var log *slog.Logger

	switch env {
	case envLocal:
		log = setupPrettySlog(writer)
	case envDev:
		log = slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug}))
	case envProd:
		log = slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	}
	return log
}

func setupPrettySlog(writer io.Writer) *slog.Logger {
	opts := slogpretty.PrettyHandlerOptions{
		SlogOpts: &slog.HandlerOptions{
			Level: slog.LevelDebug,
		},
	}

	handler := opts.NewPrettyHandler(writer)

	return slog.New(handler)
}
