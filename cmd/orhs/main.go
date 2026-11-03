// Command orhs runs a simulation from a TOML settings file and CSV inputs.
package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/realjonathanhester/open-ride-hailing-simulator/runner"
)

func main() {
	settings := flag.String("settings", "", "path to the TOML settings file (required)")
	requests := flag.String("requests", "", "ride requests CSV (overrides [io] requests_path)")
	supply := flag.String("supply", "", "supply CSV (overrides [io] supply_path)")
	output := flag.String("output", "", "output directory (overrides [io] output_directory)")
	flag.Parse()
	if *settings == "" {
		flag.Usage()
		os.Exit(2)
	}
	if _, err := runner.RunFiles(*settings, runner.Paths{Requests: *requests, Supply: *supply, OutputDirectory: *output}); err != nil {
		fmt.Fprintln(os.Stderr, "orhs:", err)
		os.Exit(1)
	}
}
