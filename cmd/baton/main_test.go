package main

import (
	"runtime/debug"
	"testing"
)

func TestResolvedVersionUsesBuildInfoForDevBuildVariable(t *testing.T) {
	buildInfo := &debug.BuildInfo{
		Main: debug.Module{Version: "v0.4.1"},
	}

	if got := resolvedVersion("dev", buildInfo, true); got != "v0.4.1" {
		t.Fatalf("resolvedVersion = %q, want %q", got, "v0.4.1")
	}
}

func TestResolvedVersionKeepsInjectedVersionAuthoritative(t *testing.T) {
	if got := resolvedVersion("v9.9.9", nil, false); got != "v9.9.9" {
		t.Fatalf("resolvedVersion = %q, want injected version", got)
	}
}

func TestResolvedVersionKeepsDevForSourceBuilds(t *testing.T) {
	buildInfo := &debug.BuildInfo{
		Main: debug.Module{Version: "(devel)"},
	}

	if got := resolvedVersion("dev", buildInfo, true); got != "dev" {
		t.Fatalf("resolvedVersion = %q, want dev", got)
	}
}
