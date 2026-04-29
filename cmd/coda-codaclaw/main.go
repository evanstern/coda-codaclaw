package main

import (
	"fmt"
	"os"
)

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	var code int
	switch os.Args[1] {
	case "start":
		code = handleStart(os.Args[2:])
	case "stop":
		code = handleStop(os.Args[2:])
	case "deliver":
		code = handleDeliver(os.Args[2:])
	case "health":
		code = handleHealth(os.Args[2:])
	case "output":
		code = handleOutput(os.Args[2:])
	case "attach":
		fmt.Fprintln(os.Stderr, "coda-codaclaw: attach not implemented yet (Card E #6)")
		code = 1
	case "help", "--help", "-h":
		printUsage()
	default:
		fmt.Fprintf(os.Stderr, "coda-codaclaw: unknown subcommand %q\n", os.Args[1])
		printUsage()
		code = 1
	}
	os.Exit(code)
}

func printUsage() {
	fmt.Fprintln(os.Stderr, `coda-codaclaw — CodaClaw provider plugin for coda

Usage: coda-codaclaw <subcommand> [args...]

Subcommands (provider exec contract):
  start --agent=<name>           start a session; reads ProviderConfig JSON from stdin
  stop <sessionID>               stop a session
  deliver <sessionID>            deliver a message; reads session.Message JSON from stdin
  health <sessionID>             report session health
  output <sessionID> [--since=]  drain pending messages
  attach <sessionID>             attach to session (interactive)

See https://github.com/evanstern/coda/blob/main/docs/plugin-contracts/providers.md`)
}
