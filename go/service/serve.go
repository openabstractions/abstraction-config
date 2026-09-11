package service

import (
	"context"
	"flag"
	"fmt"
	"github.com/openabstractions/abstraction-config/go/client"
	"os"
	"os/signal"
)

// Serve is registered as openabstractions serve config. One process per user;
// it is not a privileged broker for other users' configuration.
func Serve(args []string) error {
	flags := flag.NewFlagSet("config", flag.ContinueOnError)
	endpoint := flags.String("endpoint", client.DefaultEndpoint(), "framed per-user config endpoint")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("config: unexpected arguments")
	}
	host, err := Listen(*endpoint)
	if err != nil {
		return err
	}
	defer host.Close()
	host.OnError = func(err error) { fmt.Fprintln(os.Stderr, "config:", err) }
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()
	fmt.Fprintln(os.Stdout, "config: listening", *endpoint)
	return host.Serve(ctx)
}
