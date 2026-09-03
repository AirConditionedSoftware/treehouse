package forge

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
)

type gitHub struct{}

func (gitHub) Name() string { return "github" }

func (gitHub) PullHeadRef(number int) string {
	return fmt.Sprintf("refs/pull/%d/head", number)
}

// ghPRFields is the --json field list for gh pr view.
const ghPRFields = "number,state,title,url,headRefName,isCrossRepository,headRepositoryOwner"

// ResolvePR shells out to gh. A missing gh binary or a non-zero exit (not
// authenticated, unknown PR, network down) is an ordinary error the caller
// treats as "fall back to plain git".
func (gitHub) ResolvePR(dir string, number int) (PR, error) {
	gh, err := exec.LookPath("gh")
	if err != nil {
		return PR{}, errors.New("gh CLI not found on PATH")
	}
	args := []string{"pr", "view", strconv.Itoa(number), "--json", ghPRFields}
	cmd := exec.Command(gh, args...)
	cmd.Dir = dir
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return PR{}, fmt.Errorf("gh %s: %s", strings.Join(args, " "), msg)
	}
	return parseGHPR(stdout.Bytes())
}

// parseGHPR decodes gh pr view --json output.
func parseGHPR(data []byte) (PR, error) {
	var raw struct {
		Number              int    `json:"number"`
		State               string `json:"state"`
		Title               string `json:"title"`
		URL                 string `json:"url"`
		HeadRefName         string `json:"headRefName"`
		IsCrossRepository   bool   `json:"isCrossRepository"`
		HeadRepositoryOwner struct {
			Login string `json:"login"`
		} `json:"headRepositoryOwner"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return PR{}, fmt.Errorf("parsing gh output: %w", err)
	}
	if raw.HeadRefName == "" {
		return PR{}, errors.New("gh returned no head branch name")
	}
	return PR{
		Number:      raw.Number,
		State:       raw.State,
		Title:       raw.Title,
		URL:         raw.URL,
		HeadRef:     raw.HeadRefName,
		IsCrossRepo: raw.IsCrossRepository,
		HeadOwner:   raw.HeadRepositoryOwner.Login,
	}, nil
}
