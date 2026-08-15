package storage

import (
	"encoding/json"

	"coderun-agent/internal/coderun"
)

func marshalProblem(p *coderun.Problem) ([]byte, error) {
	return json.Marshal(p)
}
