package plugins

import "testing"

func TestGitHubCommands(t *testing.T) {
	plugin := &GitHub{}
	if got := plugin.Name(); got != "github" {
		t.Fatalf("Name() = %q", got)
	}
	if got := plugin.Commands(); len(got) != 2 || got[0] != "github" || got[1] != "gh" {
		t.Fatalf("unexpected commands: %v", got)
	}
}

func TestParseGitHubTarget(t *testing.T) {
	tests := []struct {
		name string
		want githubTarget
	}{
		{name: "octo/example", want: githubTarget{owner: "octo", repo: "example", kind: "repo"}},
		{name: "https://github.com/octo/example/", want: githubTarget{owner: "octo", repo: "example", kind: "repo"}},
		{name: "octo/example#42", want: githubTarget{owner: "octo", repo: "example", kind: "issue", number: 42}},
		{name: "https://github.com/octo/example/issues/42", want: githubTarget{owner: "octo", repo: "example", kind: "issue", number: 42}},
		{name: "https://github.com/octo/example/pull/7", want: githubTarget{owner: "octo", repo: "example", kind: "pr", number: 7}},
		{name: "https://github.com/octo/example/releases/tag/v1.2.3", want: githubTarget{owner: "octo", repo: "example", kind: "release", tag: "v1.2.3"}},
		{name: "https://github.com/octo/example/commit/0123456789abcdef", want: githubTarget{owner: "octo", repo: "example", kind: "commit", sha: "0123456789abcdef"}},
	}
	for _, test := range tests {
		got, err := parseGitHubTarget(test.name)
		if err != nil {
			t.Fatalf("parseGitHubTarget(%q): %v", test.name, err)
		}
		if got != test.want {
			t.Errorf("parseGitHubTarget(%q) = %#v, want %#v", test.name, got, test.want)
		}
	}
}

func TestParseGitHubTargetRejectsNonGitHubURL(t *testing.T) {
	for _, value := range []string{"https://example.com/octo/example", "octo", "octo/example/issues/nope", "octo/example#0"} {
		if _, err := parseGitHubTarget(value); err == nil {
			t.Errorf("parseGitHubTarget(%q) unexpectedly succeeded", value)
		}
	}
}
