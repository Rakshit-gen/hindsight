// Command hindsight scores a routing policy on payments another policy
// routed, before anyone turns it on.
package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/Rakshit-gen/hindsight/logs"
	"github.com/Rakshit-gen/hindsight/ope"
	"github.com/Rakshit-gen/hindsight/plan"
	"github.com/Rakshit-gen/hindsight/policy"
	"github.com/Rakshit-gen/hindsight/sim"
)

var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

const usage = `hindsight scores a routing policy on payments another policy routed.

  hindsight eval   --logs logs.jsonl --policy new.json [--baseline old.json]
  hindsight check  --logs logs.jsonl
  hindsight plan   --per-day 80000 --share 0.04 --lift 0.15
  hindsight sim    [--logger static|window|thompson] [--epsilon 0.05] > logs.jsonl
  hindsight bench  [accuracy|loggers|budget|paired|horizon ...]
  hindsight version

Run a command with -h for its flags.
`

func main() {
	if len(os.Args) < 2 {
		fmt.Fprint(os.Stderr, usage)
		os.Exit(2)
	}
	cmds := map[string]func([]string) error{
		"eval": evalCmd, "check": checkCmd, "plan": planCmd, "sim": simCmd, "bench": benchCmd,
	}
	name, args := os.Args[1], os.Args[2:]
	switch name {
	case "version", "--version", "-v":
		fmt.Printf("hindsight %s (%s, %s)\n", version, commit, date)
		return
	case "help", "-h", "--help":
		fmt.Print(usage)
		return
	}
	cmd, ok := cmds[name]
	if !ok {
		fmt.Fprintf(os.Stderr, "hindsight: no command %q\n\n%s", name, usage)
		os.Exit(2)
	}
	if err := cmd(args); err != nil {
		if !errors.Is(err, flag.ErrHelp) {
			fmt.Fprintln(os.Stderr, "hindsight:", err)
		}
		os.Exit(1)
	}
}

func readLogs(path string) ([]logs.Payment, error) {
	var r io.Reader = os.Stdin
	if path != "-" {
		f, err := os.Open(path)
		if err != nil {
			return nil, err
		}
		defer f.Close()
		r = f
	}
	as, err := logs.Read(r)
	if err != nil {
		return nil, err
	}
	return logs.Group(as)
}
