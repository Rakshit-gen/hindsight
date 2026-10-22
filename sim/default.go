package sim

import (
	"fmt"
	"time"

	"github.com/Rakshit-gen/hindsight/examples"
	"github.com/Rakshit-gen/hindsight/policy"
)

// Incidents is the outage every simulated log has unless asked otherwise:
// birch's UPI at 40% of normal for 90 minutes on the first afternoon.
func Incidents() []Incident {
	return []Incident{{Gateway: "birch", Method: "upi", Start: 14 * time.Hour, Length: 90 * time.Minute, Factor: 0.4}}
}

// NewLogger builds one of the simulated live routers. Static uses the
// example "today" rules unless given others; epsilon is its exploration
// share, or the floor for Thompson.
func NewLogger(kind string, epsilon float64, static *policy.Table, seed uint64) (Logger, error) {
	switch kind {
	case "static":
		t := examples.Load("today")
		if static != nil {
			t = static
		}
		c := *t
		c.Epsilon = epsilon
		return Static{&c}, nil
	case "window":
		return &Window{Size: 300, Epsilon: epsilon}, nil
	case "thompson":
		return &Thompson{Floor: epsilon, HalfLife: 2 * time.Hour, Draws: 1000, Seed: seed}, nil
	}
	return nil, fmt.Errorf("no logger %q: static, window or thompson", kind)
}
