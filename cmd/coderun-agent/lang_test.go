package main

import (
	"strings"
	"testing"

	"coderun-agent/internal/coderun"
)

var testCompilers = []coderun.Compiler{
	{Slug: "python_make", Title: "Python", Version: "3.12.3"},
	{Slug: "nodejs_20_make", Title: "JavaScript", Version: "20.14.0"},
	{Slug: "cpp_make", Title: "C++", Version: "14.1.0"},
}

func TestResolveCompilerBySlug(t *testing.T) {
	got, err := resolveCompiler("python_make", testCompilers)
	if err != nil {
		t.Fatal(err)
	}
	if got != "python_make" {
		t.Errorf("got %q", got)
	}
}

func TestResolveCompilerByFriendlyName(t *testing.T) {
	got, err := resolveCompiler("python", testCompilers)
	if err != nil {
		t.Fatal(err)
	}
	if got != "python_make" {
		t.Errorf("got %q, want python_make", got)
	}
}

func TestResolveCompilerJavaScriptMapsToNodeSlug(t *testing.T) {
	// The slug is nodejs_20_make. Deriving it from the name is impossible,
	// which is exactly why resolution goes through the scraped list.
	got, err := resolveCompiler("javascript", testCompilers)
	if err != nil {
		t.Fatal(err)
	}
	if got != "nodejs_20_make" {
		t.Errorf("got %q, want nodejs_20_make", got)
	}
}

func TestResolveCompilerRejectsAmbiguousPrefix(t *testing.T) {
	// "jav" prefixes both Java and JavaScript. Returning the first match would
	// submit Java source as JavaScript or vice versa — a wasted submission on
	// a live contest platform, with nothing in the output to say a fuzzy match
	// occurred.
	ambiguous := []coderun.Compiler{
		{Slug: "java_make", Title: "Java"},
		{Slug: "nodejs_20_make", Title: "JavaScript"},
	}
	_, err := resolveCompiler("jav", ambiguous)
	if err == nil {
		t.Fatal("expected an error for an ambiguous prefix, not a silent pick")
	}
	for _, want := range []string{"java_make", "nodejs_20_make"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q should list candidate %q", err, want)
		}
	}
}

func TestResolveCompilerExactTitleBeatsPrefix(t *testing.T) {
	// "C" is an exact title and also a prefix of "C#" and "C++". The exact
	// match must win, or asking for C would be reported as ambiguous.
	cLike := []coderun.Compiler{
		{Slug: "c_make", Title: "C"},
		{Slug: "csharp_make", Title: "C#"},
		{Slug: "cpp_make", Title: "C++"},
	}
	got, err := resolveCompiler("c", cLike)
	if err != nil {
		t.Fatalf("exact title match should win over prefix ambiguity: %v", err)
	}
	if got != "c_make" {
		t.Errorf("got %q, want c_make", got)
	}
}

func TestResolveCompilerUnknownListsOptions(t *testing.T) {
	_, err := resolveCompiler("brainfuck", testCompilers)
	if err == nil {
		t.Fatal("expected an error for an unknown language")
	}
	// The error must be actionable rather than merely negative.
	if !strings.Contains(err.Error(), "python_make") {
		t.Errorf("error should list the available compilers, got %v", err)
	}
}
