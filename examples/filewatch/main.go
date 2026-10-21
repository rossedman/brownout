// Command filewatch loads brownout rules from a JSON file and reloads them
// whenever the file changes. This is the pattern to use with a Kubernetes
// ConfigMap mounted as a volume: edit the ConfigMap and every replica picks
// up the new rules within a few seconds, without a restart.
//
//	go run ./examples/filewatch -rules examples/filewatch/rules.json
//
//	curl -i localhost:8080/reports/q3   # 503, from rules.json
//	# edit rules.json (e.g. set "rules": []) and save
//	curl -i localhost:8080/reports/q3   # 200 within ~2s
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"html"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/rossedman/brownout"
)

// shutdownGrace is how long in-flight requests get to finish on shutdown.
var shutdownGrace = 5 * time.Second

// run serves the app on ln with rules loaded from rulesPath, reloading them
// every interval, until ctx is canceled. It returns once the server and the
// file watcher have both stopped.
func run(ctx context.Context, ln net.Listener, rulesPath string, interval time.Duration) error {
	c := brownout.New()

	// Load once up front so a bad file fails startup loudly, then watch.
	if err := c.LoadFile(rulesPath); err != nil {
		return err
	}

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	watchDone := make(chan struct{})
	go func() {
		defer close(watchDone)
		err := c.WatchFile(ctx, rulesPath, interval, func(err error) {
			log.Printf("brownout: keeping previous rules: %v", err)
		})
		if err != nil && !errors.Is(err, context.Canceled) {
			log.Printf("brownout: watcher stopped: %v", err)
		}
	}()

	app := http.NewServeMux()
	app.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, "hello from %s\n", html.EscapeString(r.URL.Path))
	})
	srv := &http.Server{Handler: c.Middleware(app), ReadHeaderTimeout: 10 * time.Second}

	serveErr := make(chan error, 1)
	go func() { serveErr <- srv.Serve(ln) }()

	select {
	case err := <-serveErr:
		cancel()
		<-watchDone
		return err
	case <-ctx.Done():
	}

	shutdownCtx, cancelShutdown := context.WithTimeout(context.Background(), shutdownGrace)
	defer cancelShutdown()
	err := srv.Shutdown(shutdownCtx)
	if errors.Is(err, context.DeadlineExceeded) {
		// Connections still open after the grace period, including ones a
		// client opened but never sent a request on, are closed forcibly.
		err = srv.Close()
	}
	if serr := <-serveErr; err == nil && !errors.Is(serr, http.ErrServerClosed) {
		err = serr
	}
	<-watchDone
	return err
}

// cli parses args, starts listening and runs until ctx is canceled.
func cli(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("filewatch", flag.ContinueOnError)
	addr := fs.String("addr", ":8080", "address to listen on")
	rulesPath := fs.String("rules", "rules.json", "path to the brownout rules file")
	interval := fs.Duration("interval", 2*time.Second, "how often to check the rules file")
	if err := fs.Parse(args); err != nil {
		return err
	}

	ln, err := net.Listen("tcp", *addr)
	if err != nil {
		return err
	}
	log.Printf("app listening on %s, watching %s", ln.Addr(), *rulesPath)
	return run(ctx, ln, *rulesPath, *interval)
}

// main runs the CLI until SIGINT or SIGTERM.
func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	err := cli(ctx, os.Args[1:])
	stop()
	if err != nil && !errors.Is(err, flag.ErrHelp) {
		log.Fatal(err)
	}
}
