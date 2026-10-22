//go:build js && wasm

// Command wasm runs hindsight in a browser, so logs are scored without
// leaving the machine they're on.
package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"syscall/js"
	"time"

	"github.com/Rakshit-gen/hindsight/examples"
	"github.com/Rakshit-gen/hindsight/logs"
	"github.com/Rakshit-gen/hindsight/ope"
	"github.com/Rakshit-gen/hindsight/plan"
	"github.com/Rakshit-gen/hindsight/policy"
	"github.com/Rakshit-gen/hindsight/sim"
)

func main() {
	js.Global().Set("hindsight", js.ValueOf(map[string]any{
		"evaluate": js.FuncOf(evaluate),
		"sample":   js.FuncOf(sample),
		"plan":     js.FuncOf(planTable),
	}))
	select {}
}

func reply(v any, err error) any {
	if err != nil {
		return js.ValueOf(map[string]any{"error": err.Error()})
	}
	b, _ := json.Marshal(v)
	return js.Global().Get("JSON").Call("parse", string(b))
}

// evaluate(logs, policy, baseline, horizonMinutes)
func evaluate(_ js.Value, args []js.Value) any {
	as, err := logs.Read(strings.NewReader(args[0].String()))
	if err != nil {
		return reply(nil, err)
	}
	ps, err := logs.Group(as)
	if err != nil {
		return reply(nil, err)
	}
	target, err := policy.Parse([]byte(args[1].String()))
	if err != nil {
		return reply(nil, err)
	}
	var baseline policy.Policy
	if b := args[2].String(); strings.TrimSpace(b) != "" {
		if baseline, err = policy.Parse([]byte(b)); err != nil {
			return reply(nil, err)
		}
	}
	ps, dropped := ope.Settled(ps, time.Duration(args[3].Float()*float64(time.Minute)))
	r, err := ope.Evaluate(ps, target, baseline, ope.Options{})
	if err != nil {
		return reply(nil, err)
	}
	return reply(struct {
		Dropped int `json:"dropped"`
		*ope.Report
	}{dropped, r}, nil)
}

// sample(hours, epsilon, seed) gives a simulated log and the example
// policies, to try the page without real data.
func sample(_ js.Value, args []js.Value) any {
	days, eps, seed := args[0].Float()/24, args[1].Float(), uint64(args[2].Int())
	lg, _ := sim.NewLogger("static", eps, nil, seed)
	w := &sim.World{Seed: seed, Incidents: sim.Incidents()}
	ps := sim.Traffic(seed, days, 60)
	var buf bytes.Buffer
	if err := logs.Write(&buf, sim.Play(w, ps, lg, seed, sim.Epoch+int64(days*86_400_000))); err != nil {
		return reply(nil, err)
	}
	pols := map[string]string{}
	for _, n := range []string{"today", "sbi-cards-to-cedar", "upi-to-alder", "retry-same-gateway"} {
		pols[n] = examples.Raw(n)
	}
	return reply(map[string]any{"logs": buf.String(), "policies": pols}, nil)
}

// plan(inputs, epsilons)
func planTable(_ js.Value, args []js.Value) any {
	var in plan.Inputs
	if err := json.Unmarshal([]byte(args[0].String()), &in); err != nil {
		return reply(nil, err)
	}
	var eps []float64
	if err := json.Unmarshal([]byte(args[1].String()), &eps); err != nil {
		return reply(nil, err)
	}
	return reply(plan.Table(in, eps), nil)
}
