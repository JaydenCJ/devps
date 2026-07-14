package render

import (
	"encoding/json"
	"io"

	"github.com/JaydenCJ/devps/internal/scan"
)

// jsonListener is the stable machine-readable row. Field order and names
// are part of the schema_version 1 contract.
type jsonListener struct {
	Port       int      `json:"port"`
	Addresses  []string `json:"addresses"`
	PID        int      `json:"pid"`
	UID        int      `json:"uid"`
	User       string   `json:"user"`
	Command    string   `json:"command"`
	Argv       string   `json:"argv"`
	Kind       string   `json:"kind"`
	Dir        string   `json:"dir,omitempty"`
	Project    string   `json:"project,omitempty"`
	GitRoot    string   `json:"git_root,omitempty"`
	Branch     string   `json:"branch,omitempty"`
	Detached   bool     `json:"detached,omitempty"`
	AgeSeconds int64    `json:"age_seconds"`
	Age        string   `json:"age"`
}

type envelope struct {
	Tool          string         `json:"tool"`
	SchemaVersion int            `json:"schema_version"`
	Listeners     []jsonListener `json:"listeners"`
	Hidden        int            `json:"hidden"`
	UnownedPorts  int            `json:"unowned_ports"`
}

// JSON writes the machine-readable report. Listeners is always an array
// (never null) so downstream `| jq '.listeners[]'` pipelines cannot break
// on an idle machine.
func JSON(w io.Writer, res scan.Result) error {
	env := envelope{
		Tool:          "devps",
		SchemaVersion: 1,
		Listeners:     make([]jsonListener, 0, len(res.Listeners)),
		Hidden:        res.Hidden,
		UnownedPorts:  res.Unowned,
	}
	for i := range res.Listeners {
		l := &res.Listeners[i]
		env.Listeners = append(env.Listeners, jsonListener{
			Port:       l.Port,
			Addresses:  l.Addresses,
			PID:        l.PID,
			UID:        l.UID,
			User:       l.User,
			Command:    l.Command,
			Argv:       l.Argv,
			Kind:       l.Kind.String(),
			Dir:        l.Dir,
			Project:    l.Project,
			GitRoot:    l.GitRoot,
			Branch:     l.Branch,
			Detached:   l.Detached,
			AgeSeconds: int64(l.Age.Seconds()),
			Age:        Age(l.Age),
		})
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(env)
}
