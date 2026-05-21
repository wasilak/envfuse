package main

import (
	"context"
	"flag"
	"fmt"
	"os"

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

	result := synccycle.RunSingleCycleFromConfig(context.Background(), configPath)
	fmt.Println(string(result.Status))

	if result.Status != synccycle.CycleStatusSuccess {
		os.Exit(1)
	}
}
