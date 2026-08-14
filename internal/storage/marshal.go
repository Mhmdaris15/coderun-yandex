package storage

import (
	"encoding/json"

	"coderun-agent/internal/coderun"
)

func marshalProblem(p *coderun.Problem) ([]byte, error) {
	return json.Marshal(p)
}

// UnmarshalProblem is used by the export command.
func UnmarshalProblem(blob []byte) (*coderun.Problem, error) {
	var p coderun.Problem
	if err := json.Unmarshal(blob, &p); err != nil {
		return nil, err
	}
	return &p, nil
}
