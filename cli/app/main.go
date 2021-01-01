package main

import (
	"context"
	"crypto/tls"
	"flag"
	"io/ioutil"
	golog "log"
	"net"
	"net/http"
	"os"
	"syscall"
	"time"

	"git.sr.ht/~mariusor/wrapper"
	"github.com/go-ap/errors"
	"github.com/go-chi/chi"
	"github.com/go-chi/chi/middleware"
	"github.com/mariusor/go-littr/app"
	"github.com/mariusor/go-littr/internal/config"
	"github.com/mariusor/go-littr/internal/log"
)

var version = "HEAD"

const defaultPort = config.DefaultListenPort
const defaultTimeout = time.Second * 5

// SetupHttpServer creates a new http server and returns the start and stop functions for it
func SetupHttpServer(ctx context.Context, conf config.Configuration, m http.Handler) (func() error, func() error) {
	var serveFn func() error
	var srv *http.Server
	fileExists := func(dir string) bool {
		if _, err := os.Stat(dir); os.IsNotExist(err) {
			return false
		}
		return true
	}

	srv = &http.Server{
		Addr:              conf.Listen(),
		Handler:           m,
		ErrorLog:          golog.New(ioutil.Discard, "", 0),
		ReadHeaderTimeout: conf.TimeOut/20,
		WriteTimeout:      conf.TimeOut,
		ConnContext:       func(ctx context.Context, c net.Conn) context.Context {
			ctx, _ = context.WithCancel(ctx)
			return ctx
		},
	}
	if conf.Secure && fileExists(conf.CertPath) && fileExists(conf.KeyPath) {
		srv.TLSConfig = &tls.Config{
			MinVersion:               tls.VersionTLS12,
			CurvePreferences:         []tls.CurveID{tls.CurveP521, tls.CurveP384, tls.CurveP256},
			PreferServerCipherSuites: true,
			CipherSuites: []uint16{
				tls.TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256,
				tls.TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256,
				tls.TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384,
				tls.TLS_ECDHE_RSA_WITH_AES_256_CBC_SHA,
				tls.TLS_RSA_WITH_AES_256_GCM_SHA384,
				tls.TLS_RSA_WITH_AES_256_CBC_SHA,
			},
		}
		serveFn = func() error {
			return srv.ListenAndServeTLS(conf.CertPath, conf.KeyPath)
		}
	} else {
		serveFn = srv.ListenAndServe
	}
	shutdown := func() error {
		select {
		case <-ctx.Done():
			if err := ctx.Err(); err != http.ErrServerClosed {
				return err
			}
		}
		err := srv.Shutdown(ctx)
		if err != nil {
			return err
		}
		return nil
	}

	// Run our server in a goroutine so that it doesn't block.
	return serveFn, shutdown
}

// Run is the wrapper for starting the web-server and handling signals
func Run(a app.Application) {
	a.Logger.WithContext(log.Ctx{
		"listen":  a.Conf.Listen(),
		"host":    a.Conf.HostName,
		"env":     a.Conf.Env,
		"https":   a.Conf.Secure,
		"timeout": a.Conf.TimeOut,
	}).Info("Started")

	ctx, cancelFn := context.WithCancel(context.TODO())
	srvStart, srvShutdown := SetupHttpServer(ctx, *a.Conf, a.Mux)
	defer func() {
		cancelFn()
		srvShutdown()
	}()

	runFn := func() {
		// Run our server in a goroutine so that it doesn't block.
		if err := srvStart(); err != nil {
			a.Logger.Errorf("Error: %s", err)
			os.Exit(1)
		}
	}

	// Set up the signal handlers functions so the OS can tell us if the it requires us to stop
	sigHandlerFns := wrapper.SignalHandlers {
		syscall.SIGHUP: func(_ chan int) {
			a.Logger.Info("SIGHUP received, reloading configuration")
			a.Conf = config.Load(a.Conf.Env, a.Conf.TimeOut)
		},
		syscall.SIGUSR1: func(_ chan int) {
			a.Logger.Info("SIGUSR1 received, switching to maintenance mode")
			a.Conf.MaintenanceMode = !a.Conf.MaintenanceMode
		},
		syscall.SIGTERM: func(status chan int) {
			// kill -SIGTERM XXXX
			a.Logger.Info("SIGTERM received, stopping")
			status <- 0
		},
		syscall.SIGINT: func(status chan int) {
			// kill -SIGINT XXXX or Ctrl+c
			a.Logger.Info("SIGINT received, stopping")
			status <- 0
		},
		syscall.SIGQUIT: func(status chan int) {
			a.Logger.Error("SIGQUIT received, force stopping")
			status <- -1
		},
	}

	// Wait for OS signals asynchronously
	code := wrapper.RegisterSignalHandlers(sigHandlerFns).Exec(runFn)
	if code == 0 {
		a.Logger.Info("Shutting down")
	}
	os.Exit(code)
}
func main() {
	var wait time.Duration
	var port int
	var host string
	var env string

	flag.DurationVar(&wait, "graceful-timeout", defaultTimeout, "the duration for which the server gracefully wait for existing connections to finish - e.g. 15s or 1m")
	flag.IntVar(&port, "port", defaultPort, "the port on which we should listen on")
	flag.StringVar(&host, "host", "", "the host on which we should listen on")
	flag.StringVar(&env, "env", "unknown", "the environment type")
	flag.Parse()

	c := config.Load(config.EnvType(env), wait)
	errors.IncludeBacktrace = c.Env.IsDev()

	// Routes
	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	if !c.Env.IsProd() {
		r.Use(middleware.Recoverer)
		http.DefaultTransport.(*http.Transport).TLSClientConfig = &tls.Config{InsecureSkipVerify: true}
	}

	a := app.New(c, host, port, version, r)
	Run(a)
}
