// Package logs reads a payment router's decision logs: one record per
// attempt, written when the router picked the gateway.
//
// Off-policy evaluation only works if the router wrote down, at the moment
// it chose, the chance it had of choosing what it chose. A propensity
// rebuilt later from counts is a guess about a policy that kept changing,
// and the estimates inherit the guess.
package logs

import (
	"bufio"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Attempt is one try at charging a payment through one gateway.
type Attempt struct {
	Payment string `json:"payment"`
	Attempt int    `json:"attempt"` // 1 for the first try
	At      int64  `json:"at"`      // unix milliseconds
	// What the router knew about the payment: method, bank, card network,
	// amount band. Policies match on these.
	Context  map[string]string `json:"context,omitempty"`
	Eligible []string          `json:"eligible"`
	Gateway  string            `json:"gateway"`
	// The chance the router had of picking Gateway, at the time.
	Propensity float64 `json:"propensity"`
	// The router's whole distribution over Eligible, if it logged one. It
	// shows where the router could never have gone.
	Probs map[string]float64 `json:"probs,omitempty"`
	// "success", "pending" while the outcome isn't known, anything else
	// for a failure.
	Outcome   string `json:"outcome"`
	SettledAt int64  `json:"settled_at,omitempty"`
}

func (a Attempt) Paid() bool    { return a.Outcome == "success" }
func (a Attempt) Pending() bool { return a.Outcome == "pending" }

// Payment is every attempt at one payment, in order.
type Payment struct {
	ID       string
	Attempts []Attempt
}

func (p Payment) Paid() bool   { return p.Attempts[len(p.Attempts)-1].Paid() }
func (p Payment) Start() int64 { return p.Attempts[0].At }
