// Package coderun holds the domain model and the client interface.
package coderun

import "time"

// Status is a problem's solve state, parsed from the status icon's class token.
// Exactly three states exist on the site; see docs/coderun-research.md.
type Status string

const (
	StatusUnknown   Status = "UNKNOWN"
	StatusNotSolved Status = "NOT_SOLVED"
	StatusWrong     Status = "WRONG"
	StatusSolved    Status = "SOLVED"
)

type DifficultyLevel string

const (
	DifficultyUnknown DifficultyLevel = "UNKNOWN"
	DifficultyEasy    DifficultyLevel = "EASY"
	DifficultyMedium  DifficultyLevel = "MEDIUM"
	DifficultyHard    DifficultyLevel = "HARD"
)

// Difficulty keeps the original label alongside the parsed level. Only
// "Средняя" has been observed, so the raw string is the trustworthy field.
type Difficulty struct {
	Level DifficultyLevel
	Raw   string
}

// ProblemRef identifies a problem. The slug pair is the primary key; ContextID
// is discovered from the page's solution-template request and is required by
// the submission API.
type ProblemRef struct {
	SelectionSlug string
	ProblemSlug   string
	ContextID     int
}

type Selection struct {
	Slug         string
	Title        string
	Group        string
	ProblemCount int
}

type ProblemSummary struct {
	Ref        ProblemRef
	Number     int
	Title      string
	Difficulty Difficulty
	Status     Status
	URL        string
}

type Example struct {
	Input  string
	Output string
}

type Compiler struct {
	Slug    string // "python_make" — never derived from Title
	Title   string // "Python"
	Version string // "3.12.3"
}

type Problem struct {
	Ref          ProblemRef
	Number       int
	Title        string
	Difficulty   Difficulty
	Statement    string
	InputFormat  string
	OutputFormat string
	Constraints  string
	Notes        string
	Sections     map[string]string // unrecognised <h2> sections, kept verbatim
	Examples     []Example
	Languages    []Compiler
	URL          string
	FetchedAt    time.Time
}

// TestResult is one judged test. Only "open" (sample) tests carry data; hidden
// tests are reported as a count only.
type TestResult struct {
	Number      int
	Verdict     string
	IsSample    bool
	TimeMillis  int
	MemoryBytes int64
	Input       string // fetched from a presigned link, never the link itself
	Output      string // what the submission actually printed
	Answer      string // what was expected
}

// Submission is one judged attempt.
//
// Verdict is deliberately a plain string, not an enum: only WRONG_ANSWER has
// ever been observed, and treating an unrecognised verdict as success would be
// the worst failure this system could have. Use IsAccepted, never a switch.
type Submission struct {
	GlobalID         string
	Ref              ProblemRef
	CompilerSlug     string
	Status           string // PENDING -> FINISHED
	Verdict          string
	MaxTimeMillis    int
	MaxMemoryBytes   int64
	FirstFailedTest  int
	CompileLog       string
	TimeLimitMillis  int
	MemoryLimitBytes int64
	OpenTests        []TestResult
	HiddenTestCount  int
	SubmittedAt      time.Time
}

// IsAccepted reports success. Verdicts are an open set, so anything not
// explicitly known to mean success counts as failure.
func IsAccepted(verdict string) bool {
	return verdict == "OK" || verdict == "ACCEPTED"
}

// IsFinished reports whether judging has completed.
func IsFinished(status string) bool { return status == "FINISHED" }
