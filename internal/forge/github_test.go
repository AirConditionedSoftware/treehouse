package forge

import (
	"strings"
	"testing"
)

func TestPullHeadRef(t *testing.T) {
	if got := (gitHub{}).PullHeadRef(123); got != "refs/pull/123/head" {
		t.Errorf("PullHeadRef(123) = %q; want refs/pull/123/head", got)
	}
}

func TestParseGHPR(t *testing.T) {
	sameRepo := []byte(`{
		"number": 7,
		"state": "OPEN",
		"title": "Fix login",
		"url": "https://github.com/acme/myapp/pull/7",
		"headRefName": "alice/fix-login",
		"isCrossRepository": false,
		"headRepositoryOwner": {"login": "acme"}
	}`)
	pr, err := parseGHPR(sameRepo)
	if err != nil {
		t.Fatal(err)
	}
	want := PR{
		Number:    7,
		State:     "OPEN",
		Title:     "Fix login",
		URL:       "https://github.com/acme/myapp/pull/7",
		HeadRef:   "alice/fix-login",
		HeadOwner: "acme",
	}
	if pr != want {
		t.Errorf("parseGHPR = %+v; want %+v", pr, want)
	}

	fork := []byte(`{
		"number": 8,
		"state": "MERGED",
		"headRefName": "bob/feature",
		"isCrossRepository": true,
		"headRepositoryOwner": {"login": "bob"}
	}`)
	pr, err = parseGHPR(fork)
	if err != nil {
		t.Fatal(err)
	}
	if !pr.IsCrossRepo || pr.HeadOwner != "bob" || pr.State != "MERGED" {
		t.Errorf("fork PR = %+v; want IsCrossRepo, HeadOwner bob, State MERGED", pr)
	}

	if _, err := parseGHPR([]byte(`{"number": 9}`)); err == nil ||
		!strings.Contains(err.Error(), "head branch") {
		t.Errorf("missing headRefName: err = %v; want head-branch error", err)
	}

	if _, err := parseGHPR([]byte(`not json`)); err == nil {
		t.Error("garbage input: want error")
	}
}
