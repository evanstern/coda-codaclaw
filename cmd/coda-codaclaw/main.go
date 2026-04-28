// coda-codaclaw is the provider plugin executable that implements
// coda's session.Provider interface against the CodaClaw runtime.
//
// The host (coda CLI) spawns this binary once per provider method
// with subcommand argv: start | stop | deliver | health | output |
// attach. Input arrives on stdin where applicable; output is JSON on
// stdout. See docs/plugin-contracts/providers.md in evanstern/coda
// for the full contract, and docs/specs/173-codaclaw-provider.md in
// this repo for the CodaClaw-specific design.
//
// Implementation lands under card #173. v0 is a stub that prints a
// usage error for every subcommand.
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

	switch os.Args[1] {
	case "start", "stop", "deliver", "health", "output", "attach":
		fmt.Fprintf(os.Stderr, "coda-codaclaw: %s not implemented yet (see card #173)\n", os.Args[1])
		os.Exit(1)
	case "help", "--help", "-h":
		printUsage()
	default:
		fmt.Fprintf(os.Stderr, "coda-codaclaw: unknown subcommand %q\n", os.Args[1])
		printUsage()
		os.Exit(1)
	}
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

See https://github.com/evanstern/coda/blob/main/docs/plugin-contracts/providers.md
for the contract; see docs/specs/173-codaclaw-provider.md for design.`)
}
