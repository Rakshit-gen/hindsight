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
