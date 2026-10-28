package main

import (
	"context"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/opentool-hub/opentool-go"
)

//go:embed mock_tool.json
var assets embed.FS

type EchoTool struct{}

func (EchoTool) Call(_ context.Context, name string, params json.RawMessage) (any, error) {
	switch name {
	case "echo":
		var input map[string]any
		if err := json.Unmarshal(params, &input); err != nil {
			return nil, opentool.InvalidParams(err.Error())
		}
		return map[string]any{"echo": input["message"]}, nil
	default:
		return nil, opentool.MethodNotFound(name)
	}
}

func (EchoTool) StreamCall(ctx context.Context, name string, params json.RawMessage, emit func(any) error) error {
	if name != "countdown" {
		return opentool.MethodNotFound(name)
	}
	var input struct {
		From int `json:"from"`
	}
	if err := json.Unmarshal(params, &input); err != nil {
		return opentool.InvalidParams(err.Error())
	}
	for count := input.From; count >= 0; count-- {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		if err := emit(map[string]int{"count": count}); err != nil {
			return err
		}
	}
	return nil
}

func (EchoTool) InitArgs(_ context.Context, args *opentool.ParsedArgs) error {
	if message, ok := args.String("message"); ok {
		fmt.Printf("custom message: %s\n", message)
	}
	return nil
}

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	spec, err := assets.ReadFile("mock_tool.json")
	if err != nil {
		return err
	}
	cli := opentool.NewCLIArguments(os.Args[1:]...)
	cli.AddCustomOption("message")
	config := opentool.DefaultConfig()
	if host := os.Getenv("HOST"); host != "" {
		config.Host = host
	}
	server, err := opentool.NewServer(
		EchoTool{},
		opentool.WithConfig(config),
		opentool.WithSpec(spec),
		opentool.WithCLIArguments(cli),
	)
	if err != nil {
		return err
	}
	if err := server.Init(context.Background()); err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	resolved := server.Config()
	listener, err := net.Listen("tcp", net.JoinHostPort(resolved.Host, strconv.Itoa(resolved.Port)))
	if err != nil {
		_ = server.Shutdown(context.Background())
		return err
	}
	log.Printf("OpenTool server listening on %s%s", listener.Addr(), resolved.Prefix)
	serveErr := make(chan error, 1)
	go func() { serveErr <- server.Serve(listener) }()

	select {
	case err := <-serveErr:
		return err
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		return errors.Join(server.Shutdown(shutdownCtx), <-serveErr)
	}
}
