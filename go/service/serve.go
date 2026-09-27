package service

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"github.com/openabstractions/abstraction-config/go/client"
	"io"
	"os"
	"os/signal"
)

const configUsage = `Usage: openabstractions serve config [options]

Answers configuration reads and edits for this account through the service.

  --endpoint EP   framed per-user config endpoint (default: the installed
                  runtime's pipe for this service)

Exit codes: 0 a clean stop, 1 startup failure, 2 usage error.
`

func isHelp(arg string) bool { return arg == "--help" || arg == "-h" || arg == "help" }

// ErrUsage wraps an error Serve returns for a bad flag or argument, after it
// has already printed configUsage. A caller that hosts several capabilities
// tells this apart from a startup failure with errors.Is(err, ErrUsage).
var ErrUsage = errors.New("config: usage error")

// Serve is registered as openabstractions serve config. One process per user;
// it is not a privileged broker for other users' configuration.
func Serve(args []string) error {
	if len(args) > 0 && isHelp(args[0]) {
		_, err := io.WriteString(os.Stdout, configUsage)
		return err
	}
	flags := flag.NewFlagSet("config", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	// configUsage already documents every option; the fallback on a
	// genuine parse error prints it once, not a second time as
	// flag.PrintDefaults' own single-dash listing.
	var usageErr error
	flags.Usage = func() { _, usageErr = fmt.Fprint(flags.Output(), configUsage) }
	endpoint := flags.String("endpoint", client.DefaultEndpoint(), "framed per-user config endpoint")
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return usageErr
		}
		return errors.Join(fmt.Errorf("%w: %v", ErrUsage, err), usageErr)
	}
	if flags.NArg() != 0 {
		//unchecked: stderr is diagnostic only; ErrUsage below preserves the failed invocation
		fmt.Fprint(os.Stderr, configUsage)
		return fmt.Errorf("%w: config: unexpected arguments", ErrUsage)
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
