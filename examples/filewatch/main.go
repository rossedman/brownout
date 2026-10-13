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
	"log"
	"net/http"
	"os/signal"
	"syscall"
	"time"

	"github.com/rossedman/brownout"
)

func main() {
	rulesPath := flag.String("rules", "rules.json", "path to the brownout rules file")
	interval := flag.Duration("interval", 2*time.Second, "how often to check the rules file")
	flag.Parse()

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	c := brownout.New()

	// Load once up front so a bad file fails startup loudly, then watch.
	if err := c.LoadFile(*rulesPath); err != nil {
		log.Fatal(err)
	}
	go func() {
		err := c.WatchFile(ctx, *rulesPath, *interval, func(err error) {
			log.Printf("brownout: keeping previous rules: %v", err)
		})
		if err != nil && !errors.Is(err, context.Canceled) {
			log.Printf("brownout: watcher stopped: %v", err)
		}
	}()

	app := http.NewServeMux()
	app.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, "hello from %s\n", r.URL.Path)
	})

	srv := &http.Server{Addr: ":8080", Handler: c.Middleware(app)}
	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdown)
	}()

	log.Printf("app listening on :8080, watching %s", *rulesPath)
	if err := srv.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
		log.Fatal(err)
	}
}
