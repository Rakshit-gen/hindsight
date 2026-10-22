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

// Read takes JSON lines or CSV with a header row, whichever the input is.
func Read(r io.Reader) ([]Attempt, error) {
	br := bufio.NewReaderSize(r, 1<<16)
	for {
		b, err := br.Peek(1)
		if errors.Is(err, io.EOF) {
			return nil, nil
		}
		if err != nil {
			return nil, err
		}
		switch b[0] {
		case ' ', '\t', '\r', '\n':
			_, _ = br.ReadByte()
			continue
		case 0xEF: // byte order mark
			_, _ = br.Discard(3)
			continue
		case '{':
			return readJSONL(br)
		}
		return readCSV(br)
	}
}

func readJSONL(r io.Reader) ([]Attempt, error) {
	var out []Attempt
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 1<<16), 1<<24)
	for line := 1; sc.Scan(); line++ {
		text := strings.TrimSpace(sc.Text())
		if text == "" {
			continue
		}
		var a Attempt
		if err := json.Unmarshal([]byte(text), &a); err != nil {
			return nil, fmt.Errorf("line %d: %w", line, err)
		}
		if err := check(a); err != nil {
			return nil, fmt.Errorf("line %d: %w", line, err)
		}
		out = append(out, a)
	}
	return out, sc.Err()
}
