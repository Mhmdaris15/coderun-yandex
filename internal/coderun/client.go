package coderun

import "context"

// CodeRunClient is the boundary between the rest of the agent and however we
// happen to talk to CodeRun today. The current implementation drives a real
// browser; if stable internal endpoints are ever adopted, only the
// implementation behind this interface changes.
type CodeRunClient interface {
	ListSelections(ctx context.Context, group string) ([]Selection, error)
	ListProblems(ctx context.Context, selectionSlug string) ([]ProblemSummary, error)
	GetProblem(ctx context.Context, ref ProblemRef) (*Problem, int, error)
	GetTemplate(ctx context.Context, ref ProblemRef, compilerSlug string) (string, error)
	Submit(ctx context.Context, ref ProblemRef, compilerSlug string, sourcePath string) (*Submission, []byte, error)
	GetSubmission(ctx context.Context, globalID string) (*Submission, error)
}
