package main

import (
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/anomalyco/dnsdist-ha-agent/internal/agent"
	"github.com/anomalyco/dnsdist-ha-agent/internal/config"
	"github.com/anomalyco/dnsdist-ha-agent/internal/logger"
)

func main() {
	configPath := flag.String("config", "/usr/local/etc/dnsdist-ha-agent.yaml", "path to config file")
	checkOnly := flag.Bool("t", false, "test config and exit")
	flag.Parse()

	if *checkOnly {
		errs := config.CheckConfig(*configPath)
		if len(errs) > 0 {
			fmt.Fprintf(os.Stderr, "\n")
			fmt.Fprintf(os.Stderr, "  ╔══════════════════════════════════════════════════╗\n")
			fmt.Fprintf(os.Stderr, "  ║       DNSDIST-HA-AGENT — CONFIG ERROR          ║\n")
			fmt.Fprintf(os.Stderr, "  ╚══════════════════════════════════════════════════╝\n")
			fmt.Fprintf(os.Stderr, "\n")
			for _, e := range errs {
				fmt.Fprintf(os.Stderr, "  • %s\n", e)
			}
			fmt.Fprintf(os.Stderr, "\n")
			fmt.Fprintf(os.Stderr, "  Fix the config and try again.\n")
			fmt.Fprintf(os.Stderr, "\n")
			os.Exit(1)
		}
		fmt.Fprintf(os.Stdout, "  Config OK: %s\n", *configPath)
		os.Exit(0)
	}

	cfg, err := config.Load(*configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "\n")
		fmt.Fprintf(os.Stderr, "  ╔══════════════════════════════════════════════════╗\n")
		fmt.Fprintf(os.Stderr, "  ║       DNSDIST-HA-AGENT — CONFIG ERROR          ║\n")
		fmt.Fprintf(os.Stderr, "  ╚══════════════════════════════════════════════════╝\n")
		fmt.Fprintf(os.Stderr, "\n")
		fmt.Fprintf(os.Stderr, "  File: %s\n", *configPath)
		fmt.Fprintf(os.Stderr, "  %s\n", err)
		fmt.Fprintf(os.Stderr, "\n")
		fmt.Fprintf(os.Stderr, "  Fix the config and try again.\n")
		fmt.Fprintf(os.Stderr, "\n")
		os.Exit(1)
	}

	log := logger.New("info", cfg.LogFile)
	log.Info("[AGENT] starting dnsdist-ha-agent", "config", *configPath)

	runner := agent.NewRunner(cfg, log.Logger)

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGTERM, syscall.SIGINT)

	go func() {
		sig := <-sigCh
		log.Info("shutting down", "signal", sig)
		runner.Stop()
	}()

	if err := runner.Run(); err != nil {
		log.Fatal("runner exited with error", "error", err)
	}
}
