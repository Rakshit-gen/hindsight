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

func evalCmd(args []string) error {
	fs := flag.NewFlagSet("eval", flag.ContinueOnError)
	logPath := fs.String("logs", "-", "decision logs, JSON lines or CSV; - for stdin")
	polPath := fs.String("policy", "", "the policy to score (required)")
	basePath := fs.String("baseline", "", "compare with this policy instead of with what the logs did")
	horizon := fs.Duration("horizon", 0, "leave out payments that started this close to the end of the logs, so their retries and late outcomes are in")
	clip := fs.Float64("clip", 0, "cap each payment's weight at this; 0 for no cap")
	asJSON := fs.Bool("json", false, "print the report as JSON")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *polPath == "" {
		return errors.New("eval needs --policy")
	}
	target, err := policy.Load(*polPath)
	if err != nil {
		return err
	}
	var baseline policy.Policy
	baseName := "what the logs did"
	if *basePath != "" {
		b, err := policy.Load(*basePath)
		if err != nil {
			return err
		}
		baseline, baseName = b, b.Name
	}
	ps, err := readLogs(*logPath)
	if err != nil {
		return err
	}
	ps, dropped := ope.Settled(ps, *horizon)
	r, err := ope.Evaluate(ps, target, baseline, ope.Options{Clip: *clip})
	if err != nil {
		return err
	}
	if *asJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(struct {
			Target   string `json:"target"`
			Baseline string `json:"baseline"`
			Dropped  int    `json:"dropped"`
			*ope.Report
		}{target.Name, baseName, dropped, r})
	}
	printReport(os.Stdout, target.Name, baseName, dropped, r)
	return nil
}

func pct(x float64) string { return strconv.FormatFloat(100*x, 'f', 2, 64) }

func printReport(w io.Writer, target, base string, dropped int, r *ope.Report) {
	fmt.Fprintf(w, "%s against %s\n", target, base)
	fmt.Fprintf(w, "%s payments", commas(r.Payments))
	if dropped > 0 {
		fmt.Fprintf(w, " (%s too recent, left out)", commas(dropped))
	}
	fmt.Fprintf(w, "\n\n%-8s %16s %18s\n", "", "paid %", "difference, pts")
	fmt.Fprintf(w, "%-8s %7s ± %-6s\n", "logged", pct(r.Logged.Value), pct(1.96*r.Logged.SE))
	for _, row := range r.Rows {
		fmt.Fprintf(w, "%-8s %7s ± %-6s %+8.2f ± %-6s\n", row.Name, pct(row.Value), pct(1.96*row.SE), 100*row.Diff.Value, pct(1.96*row.Diff.SE))
	}
	fmt.Fprintln(w, "\n± is a 95% interval. Read dr and snips: first ignores retries, and dm's")
	fmt.Fprintln(w, "interval leaves out the error in its own model.")
	printDiag(w, "", r.Diag, r.Payments)
	if r.BaselineDiag != nil {
		printDiag(w, "baseline ", *r.BaselineDiag, r.Payments)
	}
}

func printDiag(w io.Writer, who string, d ope.Diagnostics, n int) {
	fmt.Fprintf(w, "\n%sweights\n", who)
	fmt.Fprintf(w, "  effective sample size  %s of %s\n", commas(int(d.ESS)), commas(n))
	fmt.Fprintf(w, "  mean weight            %.3f ± %.3f (should be 1)\n", d.MeanWeight, 1.96*d.MeanWeightSE)
	fmt.Fprintf(w, "  largest weight         %.1f, %.2f%% of all weight\n", d.MaxWeight, 100*d.MaxShare)
	fmt.Fprintf(w, "  first attempts moved   %.2f%%\n", 100*d.Changed)
	fmt.Fprintf(w, "  out of the logs' reach %.2f%% of choices\n", 100*d.Deficit)
	for _, s := range d.Warnings {
		fmt.Fprintf(w, "  ! %s\n", s)
	}
}

func commas(n int) string {
	s := strconv.Itoa(n)
	for i := len(s) - 3; i > 0; i -= 3 {
		s = s[:i] + "," + s[i:]
	}
	return s
}
