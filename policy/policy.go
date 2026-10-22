// Package policy describes routing policies as rule tables, the form most
// teams already keep their routing in, so a proposed change can be scored
// against logs before anyone turns it on.
package policy

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"slices"
	"strconv"
	"strings"
)

// State is what a router knows when it picks a gateway for one attempt.
type State struct {
	Context  map[string]string
	Attempt  int
	Tried    []string // gateways of earlier attempts, in order
	Eligible []string
}

// Policy gives the chance of each eligible gateway. The chances sum to 1.
type Policy interface {
	Probs(s State) map[string]float64
}

// Rule places a payment if When matches. Order sends it to the first
// candidate on the list; Split spreads it by weight.
type Rule struct {
	// Context key to the value it must have. "a|b" matches either, and
	// the key "attempt" matches the attempt number, with "2+" for retries.
	When  map[string]string  `json:"when,omitempty"`
	Order []string           `json:"order,omitempty"`
	Split map[string]float64 `json:"split,omitempty"`
	// Overrides the table's epsilon for payments this rule places.
	Epsilon *float64 `json:"epsilon,omitempty"`
}

// Table is a list of rules; the first one that can place a payment does.
type Table struct {
	Name string `json:"name,omitempty"`
	// Share of payments sent to a uniformly random eligible gateway
	// instead.
	Epsilon float64 `json:"epsilon,omitempty"`
	// "next" (the default) leaves out gateways a payment already tried,
	// unless that leaves none. "same" doesn't.
	Retry string `json:"retry,omitempty"`
	Rules []Rule `json:"rules"`
}

func Parse(data []byte) (*Table, error) {
	var t Table
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&t); err != nil {
		return nil, fmt.Errorf("policy: %w", err)
	}
	if err := t.validate(); err != nil {
		return nil, fmt.Errorf("policy: %w", err)
	}
	return &t, nil
}

func Load(path string) (*Table, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	t, err := Parse(data)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	if t.Name == "" {
		t.Name = strings.TrimSuffix(path[strings.LastIndex(path, "/")+1:], ".json")
	}
	return t, nil
}

func (t *Table) validate() error {
	if t.Epsilon < 0 || t.Epsilon > 1 {
		return fmt.Errorf("epsilon %v should be between 0 and 1", t.Epsilon)
	}
	if t.Retry != "" && t.Retry != "next" && t.Retry != "same" {
		return fmt.Errorf(`retry %q should be "next" or "same"`, t.Retry)
	}
	if len(t.Rules) == 0 {
		return errors.New("no rules")
	}
	for i, r := range t.Rules {
		if (len(r.Order) == 0) == (len(r.Split) == 0) {
			return fmt.Errorf("rule %d: needs an order or a split, not both", i+1)
		}
		for g, w := range r.Split {
			if w < 0 {
				return fmt.Errorf("rule %d: weight for %s is negative", i+1, g)
			}
		}
		if r.Epsilon != nil && (*r.Epsilon < 0 || *r.Epsilon > 1) {
			return fmt.Errorf("rule %d: epsilon %v should be between 0 and 1", i+1, *r.Epsilon)
		}
	}
	return nil
}

func (t *Table) Probs(s State) map[string]float64 {
	cands := t.candidates(s)
	if len(cands) == 0 {
		return nil
	}
	probs, eps := map[string]float64{}, t.Epsilon
	placed := false
	for _, r := range t.Rules {
		if !r.matches(s) {
			continue
		}
		if placed = r.place(cands, probs); placed {
			if r.Epsilon != nil {
				eps = *r.Epsilon
			}
			break
		}
	}
	if !placed {
		for _, g := range cands {
			probs[g] = 1 / float64(len(cands))
		}
	}
	// Exploring covers every eligible gateway, tried or not, so the logs
	// can speak to retry rules too.
	for g := range probs {
		probs[g] *= 1 - eps
	}
	for _, g := range s.Eligible {
		probs[g] += eps / float64(len(s.Eligible))
	}
	return probs
}
