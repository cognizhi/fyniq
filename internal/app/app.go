// Package app starts a Fyniq service: it reads the environment, refuses to run where this build
// does not belong (DEV-07, SEC-16), builds the clock (DEV-05) and serves the HTTP shell until
// SIGTERM.
package app

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"os/signal"
	"syscall"

	"github.com/cognizhi/fyniq/internal/clock"
	"github.com/cognizhi/fyniq/internal/httpserver"
	"github.com/cognizhi/fyniq/internal/ports"
	"github.com/cognizhi/fyniq/internal/runtime"
	"github.com/cognizhi/fyniq/internal/version"
)

// Deps are what every service is given.
type Deps struct {
	Env   runtime.Env
	Clock ports.Clock
}

// Info returns this binary's identity.
func Info(service string) version.Info {
	return version.Info{Service: service, Version: version.Version, Commit: version.Commit, BuildTime: version.BuildTime, Flavour: runtime.Flavour}
}

// Prepare checks the environment and builds the dependencies; every binary calls it first.
func Prepare() (Deps, error) {
	env, err := runtime.EnvFromOS()
	if err != nil {
		return Deps{}, err
	}
	if err := runtime.CheckThisBinary(env); err != nil {
		return Deps{}, err
	}
	c, err := clock.New(env, os.Getenv("FYNIQ_BUSINESS_DATE"))
	if err != nil {
		return Deps{}, err
	}
	return Deps{Env: env, Clock: c}, nil
}

// Serve runs an HTTP service. routes adds the service's own handlers and readiness checks.
func Serve(service string, routes func(*httpserver.Server, Deps) error) int {
	if len(os.Args) > 1 && os.Args[1] == "--version" {
		fmt.Printf("%s %s (%s, %s build)\n", service, version.Version, version.Commit, runtime.Flavour)
		return 0
	}
	deps, err := Prepare()
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s: %v\n", service, err)
		return 2
	}
	s := httpserver.New(Info(service))
	if routes != nil {
		if err := routes(s, deps); err != nil {
			fmt.Fprintf(os.Stderr, "%s: %v\n", service, err)
			return 1
		}
	}
	port := os.Getenv("PORT") // Cloud Run sets PORT; the local stack sets one per service
	if port == "" {
		port = "8080"
	}
	l, err := net.Listen("tcp", ":"+port)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s: listen on port %s: %v (set PORT to a free port)\n", service, port, err)
		return 1
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	fmt.Fprintf(os.Stderr, "%s %s listening on :%s (FYNIQ_ENV=%s, %s build)\n", service, version.Version, port, deps.Env, runtime.Flavour)
	if err := s.Serve(ctx, l); err != nil {
		fmt.Fprintf(os.Stderr, "%s: %v\n", service, err)
		return 1
	}
	return 0
}

// Run runs a command-line tool or job after the same start-up checks.
func Run(tool string, main func(ctx context.Context, deps Deps, args []string) error) int {
	if len(os.Args) > 1 && os.Args[1] == "--version" {
		fmt.Printf("%s %s (%s, %s build)\n", tool, version.Version, version.Commit, runtime.Flavour)
		return 0
	}
	deps, err := Prepare()
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s: %v\n", tool, err)
		return 2
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := main(ctx, deps, os.Args[1:]); err != nil {
		var re *runtime.Error
		if errors.As(err, &re) {
			fmt.Fprintf(os.Stderr, "%s: %v\n", tool, re)
			return 2
		}
		fmt.Fprintf(os.Stderr, "%s: %v\n", tool, err)
		return 1
	}
	return 0
}
