// Command sammati runs the Sammati consent records server.
//
//	sammati serve              start the server
//	sammati key create NAME    make an admin API key (shown once)
//	sammati key revoke ID      switch a key off
//	sammati verify             check the consent record chain
//	sammati health             exit 0 when the server is answering
//	sammati admin reset-password
//	                           set a new admin password (read from standard
//	                           input) and switch two-step sign-in off
//
// Settings come from the environment:
//
//	SAMMATI_DB            database file (default ./sammati.db)
//	SAMMATI_ADDR          listen address (default 127.0.0.1:8787)
//	SAMMATI_ALLOW_ORIGIN  CORS origin for the public API (default *)
//	SAMMATI_TRUST_PROXY   "1" when behind your own reverse proxy
//	SAMMATI_FRAME_ANCESTORS  sites allowed to embed the form (default *)
package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/vishrantkhanna/sammati/internal/api"
	"github.com/vishrantkhanna/sammati/internal/hooks"
	"github.com/vishrantkhanna/sammati/internal/store"
)

const usage = "usage: sammati serve | key create NAME | key revoke ID | verify | admin reset-password | health"

func env(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if len(args) == 0 {
		return errors.New(usage)
	}
	if args[0] == "health" {
		return health(ctx)
	}
	s, err := store.Open(ctx, env("SAMMATI_DB", "sammati.db"))
	if err != nil {
		return err
	}
	defer s.Close()

	switch {
	case args[0] == "serve":
		return serve(ctx, s)
	case len(args) == 3 && args[0] == "key" && args[1] == "create":
		id, key, err := s.CreateAPIKey(ctx, args[2])
		if err != nil {
			return err
		}
		fmt.Fprintf(os.Stderr, "Key %s created. It is shown once; store it safely.\n", id)
		fmt.Println(key)
		return nil
	case len(args) == 3 && args[0] == "key" && args[1] == "revoke":
		return s.RevokeAPIKey(ctx, args[2])
	case len(args) == 2 && args[0] == "admin" && args[1] == "reset-password":
		fmt.Fprint(os.Stderr, "New password (at least 12 characters): ")
		line, err := bufio.NewReader(os.Stdin).ReadString('\n')
		if err != nil && line == "" {
			return errors.New("no password given")
		}
		if err := s.ResetPassword(ctx, strings.TrimRight(line, "\r\n")); err != nil {
			return err
		}
		fmt.Fprintln(os.Stderr, "Password changed. Two-step sign-in is off and everyone is signed out.")
		return nil
	case args[0] == "verify":
		rep, err := s.Verify(ctx)
		if err != nil {
			return err
		}
		if !rep.OK {
			return fmt.Errorf("chain broken at record %d: %s", rep.BrokenAt, rep.Problem)
		}
		fmt.Printf("Chain intact: %d records. Head: %s\n", rep.Records, rep.Head)
		return nil
	}
	return errors.New(usage)
}

// health asks the running server whether it is up. Containers use it as
// their health check, because the image has no shell or curl.
func health(ctx context.Context) error {
	_, port, err := net.SplitHostPort(env("SAMMATI_ADDR", "127.0.0.1:8787"))
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://127.0.0.1:"+port+"/healthz", nil)
	if err != nil {
		return err
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	_ = res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("server answered %d", res.StatusCode)
	}
	return nil
}

func serve(ctx context.Context, s *store.Store) error {
	log := slog.New(slog.NewTextHandler(os.Stderr, nil))
	fmt.Fprintln(os.Stderr, api.Responsibility)
	go hooks.New(s, log).Run(ctx)
	srv := &http.Server{
		Addr: env("SAMMATI_ADDR", "127.0.0.1:8787"),
		Handler: api.New(s, log, api.Options{AllowOrigin: os.Getenv("SAMMATI_ALLOW_ORIGIN"),
			TrustProxy: os.Getenv("SAMMATI_TRUST_PROXY") == "1", FrameAncestors: os.Getenv("SAMMATI_FRAME_ANCESTORS")}),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	done := make(chan error, 1)
	go func() { done <- srv.ListenAndServe() }()
	log.Info("listening", "addr", srv.Addr)
	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		shut, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		return srv.Shutdown(shut)
	}
}
