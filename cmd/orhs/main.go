// Command orhs runs simulations.
//
//	orhs run -settings settings.toml -supply supply.csv -requests requests.csv -output out/
//
// For backward compatibility, flags without a subcommand mean "run".
package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/realjonathanhester/open-ride-hailing-simulator/runner"
)

func main() {
	args := os.Args[1:]
	if len(args) > 0 && args[0] == "run" {
		args = args[1:]
	} else if len(args) > 0 && args[0] != "" && args[0][0] != '-' {
		fmt.Fprintf(os.Stderr, "orhs: unknown subcommand %q (available: run)\n", args[0])
		os.Exit(2)
	}
	os.Exit(run(args))
}

func run(args []string) int {
	flags := flag.NewFlagSet("orhs run", flag.ExitOnError)
	settings := flags.String("settings", "", "path to the TOML settings file (required)")
	requests := flags.String("requests", "", "ride requests CSV (overrides [io] requests_path)")
	supply := flags.String("supply", "", "supply CSV (overrides [io] supply_path)")
	output := flags.String("output", "", "output directory (overrides [io] output_directory)")
	flags.Parse(args)
	if *settings == "" {
		flags.Usage()
		return 2
	}
	if _, err := runner.RunFiles(*settings, runner.Paths{Requests: *requests, Supply: *supply, OutputDirectory: *output}); err != nil {
		fmt.Fprintln(os.Stderr, "orhs:", err)
		return 1
	}
	return 0
}
