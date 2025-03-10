package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"
	"time"

	"log/slog"

	"fs/internal/config"
	discoverymanager "fs/internal/discovery_manager"
	discoverymodels "fs/internal/discovery_manager/models"
	"fs/internal/listener"
	"fs/internal/node"
	"fs/internal/util/logger/handlers/slogpretty"
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
	signalChannel := make(chan os.Signal, 1)
	signal.Notify(signalChannel, os.Interrupt, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		sig := <-signalChannel
		log.Info("Shutdown signal received", slog.Any("signal", sig))
		cancel()
	}()

	// Создаем узел
	n := node.NewNode(cfg.Name, cfg.Port, log)

	// Конфигурация для обнаружения пиров
	discoveryConfig := &discoverymodels.PeerDiscoveryConfig{
		MulticastAddress:   "239.0.0.1:9999", // Пример multicast-адреса
		DiscoveryInterval:  5 * time.Second,
		ConnectionTimeout:  3 * time.Second,
		AutoconnectEnabled: false,
	}

	// Создаем менеджер обнаружения пиров
	discoveryManager := discoverymanager.NewPeerDiscoveryManager(ctx, n, discoveryConfig, log)

	// Запускаем механизмы обнаружения
	discoveryManager.Start()

	log.Info("Peer discovery manager started")

	go listener.StartListener(ctx, n, cfg.Port, log)

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
