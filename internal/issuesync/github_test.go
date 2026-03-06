package issuesync

import "testing"

func TestIsTransientGitHubError(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{name: "eof", err: assertErr("gh api: EOF"), want: true},
		{name: "timeout", err: assertErr("request timeout"), want: true},
		{name: "validation", err: assertErr("422 Validation failed"), want: false},
	}

	for _, tc := range cases {
		if got := isTransientGitHubError(tc.err); got != tc.want {
			t.Fatalf("%s: got %v want %v", tc.name, got, tc.want)
		}
	}
}

func TestIsAlreadyExistsError(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{name: "validation", err: assertErr("422 Validation failed"), want: true},
		{name: "already exists", err: assertErr("relationship already exists"), want: true},
		{name: "other", err: assertErr("404 not found"), want: false},
	}

	for _, tc := range cases {
		if got := isAlreadyExistsError(tc.err); got != tc.want {
			t.Fatalf("%s: got %v want %v", tc.name, got, tc.want)
		}
	}
}

type assertErr string

func (e assertErr) Error() string { return string(e) }
