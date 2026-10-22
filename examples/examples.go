// Package examples holds the policies the README, the bench and the web
// page use, all written for the simulated world.
package examples

import (
	"embed"

	"github.com/Rakshit-gen/hindsight/policy"
)

//go:embed *.json
var files embed.FS

// Load parses one of the example policies by name.
func Load(name string) *policy.Table {
	data, err := files.ReadFile(name + ".json")
	if err != nil {
		panic(err)
	}
	t, err := policy.Parse(data)
	if err != nil {
		panic(err)
	}
	return t
}

// Raw returns an example policy's JSON.
func Raw(name string) string {
	data, err := files.ReadFile(name + ".json")
	if err != nil {
		panic(err)
	}
	return string(data)
}
