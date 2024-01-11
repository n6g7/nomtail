package main

import (
	"context"
	"os"
	"os/signal"
	"runtime"
	"sync"
	"syscall"

	"github.com/n6g7/nomtail/internal/log"
	"github.com/n6g7/nomtail/internal/loki"
	"github.com/n6g7/nomtail/internal/nomad"
	"github.com/n6g7/nomtail/internal/version"
)

func main() {
	logger := log.SetupLogger()
	logger.Info("Nomtail starting", "version", version.Display(), "go_runtime", runtime.Version())

	nomad, err := nomad.NewClient(logger)
	if err != nil {
		logger.Error("failed to create Nomad client", "err", err)
		os.Exit(1)
	}

	loki := loki.NewClient(logger, os.Getenv("PROMTAIL_ADDR"))

	ctx, cancel := context.WithCancel(context.Background())

	var wg sync.WaitGroup
	wg.Add(2)
	go nomad.Run(ctx, &wg)
	go loki.Run(ctx, &wg)

	exit := func() {
		close(loki.Chan())
		cancel()
		wg.Wait()
	}

	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGTERM)

	for {
		select {
		case entry, ok := <-nomad.Chan():
			if !ok {
				exit()
				return
			}
			loki.Chan() <- entry
		case sig := <-sigs:
			logger.Warn("received signal", "signal", sig)
			exit()
			return
		}
	}
}
