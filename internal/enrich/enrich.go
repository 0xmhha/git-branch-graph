// Package enrich augments PR data from the GitHub GraphQL API: real state,
// base/head refs, URL, CI rollup, and an authoritative merge/squash/rebase
// classification from the merge commit's parent count. It is optional — without
// a token the whole stage is skipped and the pipeline still produces a graph
// from the offline classification.
package enrich

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/wm-it-25/git-branch-graph/internal/model"
)

const endpoint = "https://api.github.com/graphql"
const batchSize = 40

// Token resolves a GitHub token from env, then the gh CLI. Returns "" if none.
func Token() string {
	for _, k := range []string{"GITHUB_TOKEN", "GH_TOKEN"} {
		if v := strings.TrimSpace(os.Getenv(k)); v != "" {
			return v
		}
	}
	out, err := exec.Command("gh", "auth", "token").Output()
	if err == nil {
		return strings.TrimSpace(string(out))
	}
	return ""
}

// Fetch queries PR metadata for the given PR numbers. Returns a map keyed by PR
// number string. Errors on any batch abort the stage (caller treats enrich as
// best-effort).
func Fetch(owner, repo, token string, prNums []string) (map[string]model.PR, error) {
	nums := dedupSortNums(prNums)
	result := make(map[string]model.PR, len(nums))
	client := &http.Client{Timeout: 30 * time.Second}

	for i := 0; i < len(nums); i += batchSize {
		end := i + batchSize
		if end > len(nums) {
			end = len(nums)
		}
		if err := fetchBatch(client, owner, repo, token, nums[i:end], result); err != nil {
			return result, err
		}
	}
	return result, nil
}

func fetchBatch(client *http.Client, owner, repo, token string, nums []int, out map[string]model.PR) error {
	var b strings.Builder
	fmt.Fprintf(&b, `query{repository(owner:%q,name:%q){`, owner, repo)
	for _, n := range nums {
		fmt.Fprintf(&b, `p%d:pullRequest(number:%d){number state mergeCommit{oid parents{totalCount}} baseRefName headRefName url commits(last:1){nodes{commit{statusCheckRollup{state}}}}} `, n, n)
	}
	b.WriteString(`}}`)

	body, _ := json.Marshal(map[string]string{"query": b.String()})
	req, err := http.NewRequest(http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "bearer "+token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("graphql %d", resp.StatusCode)
	}

	var parsed struct {
		Data struct {
			Repository map[string]json.RawMessage `json:"repository"`
		} `json:"data"`
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return err
	}
	// A missing individual PR yields a null alias + an error entry; that is fine.
	// Only a null repository means the owner/name is wrong.
	if parsed.Data.Repository == nil {
		if len(parsed.Errors) > 0 {
			return fmt.Errorf("graphql: %s", parsed.Errors[0].Message)
		}
		return fmt.Errorf("graphql: null repository")
	}

	for alias, raw := range parsed.Data.Repository {
		if !strings.HasPrefix(alias, "p") || len(raw) == 0 || string(raw) == "null" {
			continue
		}
		var pr struct {
			Number      int    `json:"number"`
			State       string `json:"state"`
			MergeCommit *struct {
				OID     string `json:"oid"`
				Parents struct {
					TotalCount int `json:"totalCount"`
				} `json:"parents"`
			} `json:"mergeCommit"`
			BaseRefName string `json:"baseRefName"`
			HeadRefName string `json:"headRefName"`
			URL         string `json:"url"`
			Commits     struct {
				Nodes []struct {
					Commit struct {
						StatusCheckRollup *struct {
							State string `json:"state"`
						} `json:"statusCheckRollup"`
					} `json:"commit"`
				} `json:"nodes"`
			} `json:"commits"`
		}
		if json.Unmarshal(raw, &pr) != nil || pr.Number == 0 {
			continue
		}
		p := model.PR{
			Num:     strconv.Itoa(pr.Number),
			State:   strings.ToLower(pr.State),
			BaseRef: pr.BaseRefName,
			HeadRef: pr.HeadRefName,
			URL:     pr.URL,
		}
		if pr.MergeCommit != nil {
			p.MergeSHA = pr.MergeCommit.OID
			p.MergeMethod = methodFromParents(pr.MergeCommit.Parents.TotalCount, pr.HeadRefName)
		}
		if len(pr.Commits.Nodes) > 0 && pr.Commits.Nodes[0].Commit.StatusCheckRollup != nil {
			p.CIState = pr.Commits.Nodes[0].Commit.StatusCheckRollup.State
		}
		out[p.Num] = p
	}
	return nil
}

// methodFromParents infers the merge method from the merge commit's parent count.
// 2+ parents = a merge commit; 1 parent = squash (a single landed commit).
// (Rebase also lands single commits; GitHub's API does not distinguish it after
// the fact, so single-parent is reported as squash — the common case.)
func methodFromParents(parents int, _ string) string {
	if parents >= 2 {
		return "merge"
	}
	if parents == 1 {
		return "squash"
	}
	return ""
}

func dedupSortNums(ss []string) []int {
	seen := map[int]bool{}
	var nums []int
	for _, s := range ss {
		n, err := strconv.Atoi(s)
		if err != nil || seen[n] {
			continue
		}
		seen[n] = true
		nums = append(nums, n)
	}
	sort.Ints(nums)
	return nums
}
