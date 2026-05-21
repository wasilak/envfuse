package main

import (
	"context"
	"flag"
	"fmt"
	"os"

	launcher "envfuse/internal/exec"
	"envfuse/internal/state"
	synccycle "envfuse/internal/sync"
)

func main() {
	var configPath string
	var once bool

	flag.StringVar(&configPath, "config", "./seon.json", "path to seon config file")
	flag.BoolVar(&once, "once", false, "run exactly one sync cycle and exit")
	flag.Parse()

	if !once {
		fmt.Fprintln(os.Stderr, "only -once mode is supported in phase 01")
		os.Exit(1)
	}

	store := state.NewStore()
	result := synccycle.RunSingleCycleWithStoreFromConfig(context.Background(), configPath, store)
	fmt.Println(string(result.Status))

	if result.Status != synccycle.CycleStatusSuccess {
		os.Exit(1)
	}

	if len(flag.Args()) > 0 {
		if err := launcher.LaunchWithEnv(flag.Args(), os.Environ(), store.LastAppliedEnv()); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	}
}
