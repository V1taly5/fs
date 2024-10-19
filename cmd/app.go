package main

import (
	"flag"
	"fs/discover"
	"fs/internal/cli"
	"fs/internal/util/logger/handlers/slogpretty"
	"fs/listener"
	"fs/node"
	"log/slog"
	"os"
	"os/signal"
	"os/user"
	"syscall"
)

const (
	envLocal = "local"
	envDev   = "dev"
	envProd  = "prod"
)

type InitParams struct {
	Name      *string
	Port      *int
	PeersFile *string
}

var initParams InitParams

func init() {
	currentUser, _ := user.Current()
	hostName, _ := os.Hostname()

	initParams = InitParams{
		Name:      flag.String("name", currentUser.Username+"@"+hostName, "name"),
		Port:      flag.Int("port", 35034, "port that have to listen"),
		PeersFile: flag.String("peers", "peers.txt", "Path to file with addresses on each line"),
	}

	flag.Parse()
}

func main() {

	log := setupLogger(envLocal)

	log.Info("starting application",
		slog.String("name", *initParams.Name),
		slog.Int("port", *initParams.Port),
	)

	signalChenal := make(chan os.Signal, 2)
	signal.Notify(signalChenal, os.Interrupt, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		sig := <-signalChenal
		log.Info("Exit by ",
			slog.Any("signal", sig))
		// log.Default().Printf("Exit by signal: %s", sig)
		os.Exit(1)
	}()

	n := node.NewNode(*initParams.Name, *initParams.Port, log)
	cmdContext := cli.NewAppContext(n)
	Start(n, log)
	remainingArgs := flag.Args()
	// log.Default().Println(remainingArgs)
	cli.CliStart(remainingArgs, cmdContext)
	// CommandInput(n)
}

func Start(n *node.Node, log *slog.Logger) {

	//var wg sync.WaitGroup
	//wg.Add(1)
	go listener.StartListener(n, *initParams.Port, log)
	go discover.StartDiscover(n, *initParams.PeersFile, log)
	//wg.Wait()
}

// func CommandInput(n *node.Node) {
// 	reader := bufio.NewReader(os.Stdin)

// 	for {
// 		fmt.Print("Start command listener")
// 		command, _ := reader.ReadString('\n')
// 		command = strings.TrimSpace(command)
// 		parts := strings.SplitN(command, "", 1)
// 		if len(parts) != 1 || parts[0] != "send" {
// 			fmt.Print("Неправильная команда")
// 			continue
// 		}
// 		conn, err := net.Dial("tcp", "0.0.0.0:10002")
// 		if err != nil {
// 			// handle error
// 		}
// 		peer := node.NewPeer(conn)
// 		n.SendName(peer)
// 	}
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
