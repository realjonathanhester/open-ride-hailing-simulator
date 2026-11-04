// Command rhs runs simulations.
//
//	rhs run -settings settings.toml -supply supply.csv -requests requests.csv -output out/
//	rhs view -output out/ [-compare out2/] [-port 8080] [-settings viewer.toml]
//
// For backward compatibility, flags without a subcommand mean "run".
package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/realjonathanhester/ride-hailing-simulator/runner"
	"github.com/realjonathanhester/ride-hailing-simulator/viewer"
	"net/http"
)

func main() {
	args := os.Args[1:]
	if len(args) > 0 && args[0] == "view" {
		os.Exit(view(args[1:]))
	}
	if len(args) > 0 && args[0] == "run" {
		args = args[1:]
	} else if len(args) > 0 && args[0] != "" && args[0][0] != '-' {
		fmt.Fprintf(os.Stderr, "rhs: unknown subcommand %q (available: run, view)\n", args[0])
		os.Exit(2)
	}
	os.Exit(run(args))
}

func run(args []string) int {
	flags := flag.NewFlagSet("rhs run", flag.ExitOnError)
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
		fmt.Fprintln(os.Stderr, "rhs:", err)
		return 1
	}
	return 0
}

func view(args []string) int {
	flags := flag.NewFlagSet("rhs view", flag.ExitOnError)
	output := flags.String("output", "", "output directory of the run to view (required)")
	compare := flags.String("compare", "", "a second output directory to compare against")
	port := flags.Int("port", 8080, "port to listen on (127.0.0.1 only)")
	settings := flags.String("settings", "viewer.toml", "local TOML file with [viewer] google_maps_api_key and google_maps_map_id")
	flags.Parse(args)
	if *output == "" {
		flags.Usage()
		return 2
	}
	run, err := viewer.LoadRun(viewer.CSVTableReader{Directory: *output})
	if err != nil {
		fmt.Fprintln(os.Stderr, "rhs view:", err)
		return 1
	}
	config := viewer.Config{Run: run}
	if *compare != "" {
		if config.Compare, err = viewer.LoadRun(viewer.CSVTableReader{Directory: *compare}); err != nil {
			fmt.Fprintln(os.Stderr, "rhs view:", err)
			return 1
		}
	}
	maps, err := viewer.LoadMapsSettings(*settings)
	if err != nil {
		fmt.Fprintln(os.Stderr, "rhs view:", err)
		return 1
	}
	config.GoogleMapsAPIKey, config.GoogleMapsMapID = maps.GoogleMapsAPIKey, maps.GoogleMapsMapID
	address := fmt.Sprintf("127.0.0.1:%d", *port)
	if maps.GoogleMapsAPIKey == "" {
		fmt.Println("rhs view: no Google Maps API key (set GOOGLE_MAPS_API_KEY); maps are disabled")
	}
	fmt.Printf("rhs view: serving http://%s/\n", address)
	if err := http.ListenAndServe(address, viewer.NewServer(config)); err != nil {
		fmt.Fprintln(os.Stderr, "rhs view:", err)
		return 1
	}
	return 0
}
