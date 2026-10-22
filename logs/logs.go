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

// CSV puts lists in one cell with | between items: eligible is
// "alder|birch", probs is "alder:0.95|birch:0.05". Columns it doesn't know
// are context.
func readCSV(r io.Reader) ([]Attempt, error) {
	cr := csv.NewReader(r)
	cr.FieldsPerRecord = -1
	head, err := cr.Read()
	if err != nil {
		return nil, fmt.Errorf("csv header: %w", err)
	}
	for i := range head {
		head[i] = strings.ToLower(strings.TrimSpace(head[i]))
	}
	for _, need := range []string{"payment", "attempt", "at", "eligible", "gateway", "propensity", "outcome"} {
		if !slices.Contains(head, need) {
			return nil, fmt.Errorf("csv header has no %q column", need)
		}
	}
	var out []Attempt
	for line := 2; ; line++ {
		rec, err := cr.Read()
		if errors.Is(err, io.EOF) {
			return out, nil
		}
		if err != nil {
			return nil, err
		}
		a := Attempt{Context: map[string]string{}}
		for i, v := range rec {
			if i >= len(head) {
				break
			}
			v = strings.TrimSpace(v)
			if err := a.set(head[i], v); err != nil {
				return nil, fmt.Errorf("line %d, %s: %w", line, head[i], err)
			}
		}
		if err := check(a); err != nil {
			return nil, fmt.Errorf("line %d: %w", line, err)
		}
		out = append(out, a)
	}
}

func (a *Attempt) set(col, v string) error {
	var err error
	switch col {
	case "payment":
		a.Payment = v
	case "attempt":
		a.Attempt, err = strconv.Atoi(v)
	case "at":
		a.At, err = parseTime(v)
	case "settled_at":
		if v != "" {
			a.SettledAt, err = parseTime(v)
		}
	case "eligible":
		a.Eligible = strings.Split(v, "|")
	case "gateway":
		a.Gateway = v
	case "propensity":
		a.Propensity, err = strconv.ParseFloat(v, 64)
	case "probs":
		if v == "" {
			return nil
		}
		a.Probs = map[string]float64{}
		for kv := range strings.SplitSeq(v, "|") {
			k, p, ok := strings.Cut(kv, ":")
			if !ok {
				return fmt.Errorf("%q should look like gateway:probability", kv)
			}
			if a.Probs[k], err = strconv.ParseFloat(p, 64); err != nil {
				return err
			}
		}
	case "outcome":
		a.Outcome = v
	default:
		if v != "" {
			a.Context[col] = v
		}
	}
	return err
}

// parseTime takes unix milliseconds or RFC 3339.
func parseTime(v string) (int64, error) {
	if n, err := strconv.ParseInt(v, 10, 64); err == nil {
		return n, nil
	}
	t, err := time.Parse(time.RFC3339Nano, v)
	if err != nil {
		return 0, fmt.Errorf("%q is neither unix milliseconds nor RFC 3339", v)
	}
	return t.UnixMilli(), nil
}

func check(a Attempt) error {
	switch {
	case a.Payment == "":
		return errors.New("no payment id")
	case a.Attempt < 1:
		return fmt.Errorf("payment %s: attempt is %d, should start at 1", a.Payment, a.Attempt)
	case a.Gateway == "":
		return fmt.Errorf("payment %s: no gateway", a.Payment)
	case len(a.Eligible) == 0:
		return fmt.Errorf("payment %s: no eligible gateways; a policy can't be scored without them", a.Payment)
	case !slices.Contains(a.Eligible, a.Gateway):
		return fmt.Errorf("payment %s: chose %s, which isn't in eligible %v", a.Payment, a.Gateway, a.Eligible)
	case !(a.Propensity > 0 && a.Propensity <= 1):
		return fmt.Errorf("payment %s: propensity %v should be above 0 and at most 1", a.Payment, a.Propensity)
	case a.Outcome == "":
		return fmt.Errorf("payment %s: no outcome", a.Payment)
	}
	return nil
}
