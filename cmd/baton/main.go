package main

import (
	"context"
	"os"
	"os/signal"
	"runtime/debug"
	"strings"
	"syscall"

	"github.com/sjunepark/baton/internal/cli"
)

var version = "dev"

func main() {
	buildInfo, ok := debug.ReadBuildInfo()
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	os.Exit(cli.RunContext(ctx, os.Args[1:], os.Stdout, os.Stderr, resolvedVersion(version, buildInfo, ok)))
}

func resolvedVersion(injected string, buildInfo *debug.BuildInfo, ok bool) string {
	injected = strings.TrimSpace(injected)
	if injected != "" && injected != "dev" {
		return injected
	}
	if ok {
		if buildVersion := releaseBuildInfoVersion(buildInfo); buildVersion != "" {
			return buildVersion
		}
	}
	if injected != "" {
		return injected
	}
	return "dev"
}

func releaseBuildInfoVersion(info *debug.BuildInfo) string {
	if info == nil {
		return ""
	}
	version := strings.TrimSpace(info.Main.Version)
	switch version {
	case "", "(devel)", "dev":
		return ""
	default:
		return version
	}
}
