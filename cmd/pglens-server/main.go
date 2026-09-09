package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/manprint/pglens/internal/advisor"
	"github.com/manprint/pglens/internal/alert"
	"github.com/manprint/pglens/internal/alert/notify"
	"github.com/manprint/pglens/internal/server"
	"github.com/manprint/pglens/internal/store"
	"github.com/manprint/pglens/internal/webui"
)

var version = "dev" // overridden at build time with -ldflags

func main() {
	showVersion := flag.Bool("version", false, "print version and exit")
	healthcheck := flag.Bool("healthcheck", false, "GET /readyz on the configured listen address, exit 0 if it returns 200 else 1 (for Docker HEALTHCHECK — the distroless image has no shell or curl to do this any other way)")
	flag.Parse()
	if *showVersion {
		fmt.Println(version)
		return
	}
	if *healthcheck {
		os.Exit(runHealthcheck())
	}
	token, err := server.LoadBootstrapToken(nil, nil)
	if err != nil {
		log.Fatalf("bootstrap token: %v", err)
	}
	listen := os.Getenv("PGLENS_LISTEN")
	if listen == "" {
		listen = ":8080"
	}

	startupCtx := context.Background()
	var pool *pgxpool.Pool
	if dsn := os.Getenv("PGLENS_DSN"); dsn != "" {
		pool, err = pgxpool.New(startupCtx, dsn)
		if err != nil {
			log.Fatalf("connect db: %v", err)
		}
		defer pool.Close()
		if err := store.Migrate(startupCtx, pool); err != nil {
			log.Fatalf("migrate: %v", err)
		}
	} else {
		log.Println("PGLENS_DSN not set; running without a database (ingest and API calls are no-ops)")
	}

	auth := server.NewAuth(token)
	inv := server.NewInventory(pool)
	pipeline := server.NewPipeline(pool, nil)
	api := server.NewAPI(pool)
	topoAPI := server.NewTopologyAPI(pool)
	ashAPI := server.NewAshAPI(pool)
	staleness := server.NewStaleness(pool, nil)
	staleness.Start(startupCtx)
	defer staleness.Stop()

	alertCfg, err := server.LoadAlertConfig(nil, nil)
	if err != nil {
		log.Fatalf("alert configuration: %v", err)
	}
	uiCfg, err := server.LoadUIConfig(nil, nil)
	if err != nil {
		log.Fatalf("UI configuration: %v", err)
	}
	switch {
	case !uiCfg.Enabled:
		log.Println("PGLENS_UI_ENABLED=false; UI disabled")
	case uiCfg.Password == "":
		log.Println("PGLENS_UI_ENABLED=true; UI enabled but no password configured; API requests will be rejected")
	default:
		log.Println("PGLENS_UI_ENABLED=true; UI enabled")
	}
	sessionStore := server.NewSessionStore(uiCfg.SessionTTL)
	var alertStore alert.Store
	var alertSources []alert.Source
	var alertNotifier alert.Notifier
	if pool != nil {
		alertStore = alert.NewPgStore(pool)
		alertSources = []alert.Source{alert.NewMetricSource(pool, alertCfg.Interval), alert.NewEventSource(pool, alertCfg.Interval)}
		channels := []notify.Channel{}
		if alertCfg.SlackURL != "" {
			channels = append(channels, notify.NewSlack(alertCfg.SlackURL, nil))
		}
		if alertCfg.WebhookURL != "" {
			channels = append(channels, notify.NewWebhook(alertCfg.WebhookURL, nil))
		}
		if len(channels) == 0 {
			log.Println("no alert notification channel configured; alerts will be persisted but not delivered")
		} else {
			alertNotifier = alert.NewChannelNotifier(alertStore, channels...)
		}
	}
	alertEngine := alert.NewEngineWithInterval(pool, nil, alertCfg.Interval, alertStore, alertNotifier, alertSources)
	alertEngine.Start(startupCtx)
	defer alertEngine.Stop()
	advisorEngine := advisor.NewEngine(pool, nil, 0)
	advisorEngine.Start(startupCtx)
	defer advisorEngine.Stop()
	alertAPI := server.NewAlertAPI(pool, alertStore)
	assets, err := webui.Assets()
	if err != nil {
		log.Fatalf("web UI assets: %v", err)
	}
	router := server.NewRouter(uiCfg, assets, sessionStore, auth, inv, pipeline, api, topoAPI, ashAPI, alertAPI)
	srv := &http.Server{Addr: listen, Handler: router}
	go func() {
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("listen: %v", err)
		}
	}()
	// Wait for signal
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, syscall.SIGTERM, syscall.SIGINT)
	<-ch
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	_ = srv.Shutdown(ctx)
}

// runHealthcheck is invoked as `pglens-server --healthcheck` from the
// container's own HEALTHCHECK directive. It reads PGLENS_LISTEN the same way
// main() does, so it always checks the port the running server actually
// bound, even if PGLENS_LISTEN is overridden from the default.
func runHealthcheck() int {
	listen := os.Getenv("PGLENS_LISTEN")
	if listen == "" {
		listen = ":8080"
	}
	port := listen
	if i := lastColon(listen); i >= 0 {
		port = listen[i+1:]
	}
	client := &http.Client{Timeout: 3 * time.Second}
	resp, err := client.Get(fmt.Sprintf("http://127.0.0.1:%s/readyz", port))
	if err != nil {
		fmt.Fprintln(os.Stderr, "healthcheck:", err)
		return 1
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		fmt.Fprintln(os.Stderr, "healthcheck: /readyz returned", resp.StatusCode)
		return 1
	}
	return 0
}

func lastColon(s string) int {
	for i := len(s) - 1; i >= 0; i-- {
		if s[i] == ':' {
			return i
		}
	}
	return -1
}
