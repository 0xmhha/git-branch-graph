// Command gbg is the git-branch-graph core CLI.
//
// Usage:
//
//	gbg ingest <github-url-or-local-path> [flags]
//
// Flags:
//
//	--data-dir string        output root (default "./data")
//	--default-branch string  override default branch (for local clones)
//	--force                  re-extract even if the run folder already exists
package main

import (
	"context"
	"flag"
	"fmt"
	"net/http"
	"os"

	"github.com/wm-it-25/git-branch-graph/internal/export"
	"github.com/wm-it-25/git-branch-graph/internal/loader"
	"github.com/wm-it-25/git-branch-graph/internal/mcpserver"
	"github.com/wm-it-25/git-branch-graph/internal/pipeline"
	"github.com/wm-it-25/git-branch-graph/internal/runs"
	"github.com/wm-it-25/git-branch-graph/internal/serve"
	"github.com/wm-it-25/git-branch-graph/internal/webui"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	switch os.Args[1] {
	case "ingest":
		if err := runIngest(os.Args[2:]); err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
			os.Exit(1)
		}
	case "ontology":
		if err := runOntology(os.Args[2:]); err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
			os.Exit(1)
		}
	case "serve":
		if err := runServe(os.Args[2:]); err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
			os.Exit(1)
		}
	case "export":
		if err := runExport(os.Args[2:]); err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
			os.Exit(1)
		}
	case "runs":
		if err := runRuns(os.Args[2:]); err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
			os.Exit(1)
		}
	case "mcp":
		if err := runMCP(os.Args[2:]); err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
			os.Exit(1)
		}
	case "-h", "--help", "help":
		usage()
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n", os.Args[1])
		usage()
		os.Exit(2)
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, "gbg ingest   <github-url-or-local-path> [--data-dir dir] [--default-branch b] [--force]")
	fmt.Fprintln(os.Stderr, "gbg ontology <run-dir>   # recompute graph.json + graph.sqlite from raw/*.csv")
	fmt.Fprintln(os.Stderr, "gbg serve    [--data-dir dir] [--web-dir web/dist] [--addr :8080]")
	fmt.Fprintln(os.Stderr, "gbg export   <out-dir> [--data-dir dir] [--run id]   # static site (no server needed)")
	fmt.Fprintln(os.Stderr, "gbg runs     list | rm <id>   [--data-dir dir]        # manage run folders")
	fmt.Fprintln(os.Stderr, "gbg mcp      [--data-dir dir]                          # MCP server (stdio) for AI clients")
}

// runMCP serves the analysis to AI clients over MCP (stdio).
func runMCP(args []string) error {
	fs := flag.NewFlagSet("mcp", flag.ExitOnError)
	dataDir := fs.String("data-dir", "./data", "run folders root")
	_ = fs.Parse(args)
	srv := &mcpserver.Server{DataDir: *dataDir}
	return srv.Run(context.Background())
}

// runRuns lists or removes run folders.
func runRuns(args []string) error {
	fs := flag.NewFlagSet("runs", flag.ExitOnError)
	dataDir := fs.String("data-dir", "./data", "run folders root")
	sub, rest := splitPositional(args)

	switch sub {
	case "", "list":
		_ = fs.Parse(rest)
		list, err := runs.List(*dataDir)
		if err != nil {
			return err
		}
		if len(list) == 0 {
			fmt.Println("no runs.")
			return nil
		}
		for _, r := range list {
			fmt.Printf("%-48s %s/%s@%s  %d commits  %s  %s\n",
				r.ID, r.Org, r.Repo, r.Branch, r.Commits, humanSize(r.Size), r.CapturedAt)
		}
	case "rm":
		id, more := splitPositional(rest)
		_ = fs.Parse(more)
		if id == "" {
			return fmt.Errorf("usage: gbg runs rm <id>")
		}
		if err := runs.Remove(*dataDir, id); err != nil {
			return err
		}
		fmt.Printf("removed %s\n", id)
	default:
		return fmt.Errorf("unknown subcommand %q (use: list | rm)", sub)
	}
	return nil
}

func humanSize(n int64) string {
	switch {
	case n >= 1<<30:
		return fmt.Sprintf("%.1fGB", float64(n)/(1<<30))
	case n >= 1<<20:
		return fmt.Sprintf("%.0fMB", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.0fKB", float64(n)/(1<<10))
	default:
		return fmt.Sprintf("%dB", n)
	}
}

// runExport writes a static, server-less site (SPA + data + sql.js queries).
func runExport(args []string) error {
	fs := flag.NewFlagSet("export", flag.ExitOnError)
	dataDir := fs.String("data-dir", "./data", "run folders root")
	run := fs.String("run", "", "export only this run id (default: all)")
	out, rest := splitPositional(args)
	if out == "" {
		return fmt.Errorf("usage: gbg export <out-dir> [--data-dir dir] [--run id]")
	}
	_ = fs.Parse(rest)

	n, err := export.Export(*dataDir, out, *run)
	if err != nil {
		return err
	}
	fmt.Printf("exported %d run(s) to %s\n", n, out)
	fmt.Printf("serve it with any static host, e.g.:  cd %s && python3 -m http.server\n", out)
	return nil
}

// runServe starts the HTTP backend.
func runServe(args []string) error {
	fs := flag.NewFlagSet("serve", flag.ExitOnError)
	dataDir := fs.String("data-dir", "./data", "run folders root")
	webDir := fs.String("web-dir", "web/dist", "built SPA directory (optional)")
	addr := fs.String("addr", ":8080", "listen address")
	_ = fs.Parse(args)

	srv := &serve.Server{DataDir: *dataDir}
	webSrc := "none (API only)"
	if fsys, ok := webui.FS(); ok {
		srv.WebFS = fsys
		webSrc = "embedded"
	} else if _, err := os.Stat(*webDir); err == nil {
		srv.WebDir = *webDir
		webSrc = *webDir
	}

	fmt.Printf("gbg serve on http://localhost%s  (data=%s web=%s)\n", *addr, *dataDir, webSrc)
	fmt.Printf("  GET /api/runs\n  GET /api/runs/{id}/graph.json\n  GET /api/runs/{id}/containment?sha=...\n")
	return http.ListenAndServe(*addr, srv.Handler())
}

// runOntology recomputes the ontology outputs for an existing run folder.
func runOntology(args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("usage: gbg ontology <run-dir>")
	}
	runDir := args[0]
	fmt.Printf("[ontology] load %s\n", runDir)
	snap, commits, refs, edges, err := loader.Load(runDir)
	if err != nil {
		return err
	}
	fmt.Printf("      commits=%d refs=%d edges=%d\n", len(commits), len(refs), len(edges))
	// Standalone recompute: no enrich, cherries from the cached raw/cherries.csv.
	if err := pipeline.BuildOntology(runDir, snap, commits, refs, edges, nil, loader.LoadCherries(runDir), ""); err != nil {
		return err
	}
	fmt.Printf("done: %s\n", runDir)
	return nil
}

// runIngest ingests a URL / local path / existing run folder via the pipeline.
func runIngest(args []string) error {
	fs := flag.NewFlagSet("ingest", flag.ExitOnError)
	dataDir := fs.String("data-dir", "./data", "output root directory")
	defBranch := fs.String("default-branch", "", "override default branch")
	repoOverride := fs.String("repo", "", "canonical GitHub owner/name (links + enrich)")
	noEnrich := fs.Bool("no-enrich", false, "skip GitHub PR enrichment even if a token is available")
	refreshEnrich := fs.Bool("refresh-enrich", false, "ignore the per-repo enrich cache and re-query all PRs")
	containment := fs.String("containment", "", "containment output scope: full | pr-only | recent:N")
	force := fs.Bool("force", false, "re-extract even if the run folder exists")

	input, rest := splitPositional(args)
	if input == "" {
		return fmt.Errorf("missing repository URL or path")
	}
	_ = fs.Parse(rest)

	res, err := pipeline.Run(pipeline.Options{
		Input: input, DataDir: *dataDir, DefaultBranch: *defBranch,
		Repo: *repoOverride, NoEnrich: *noEnrich, RefreshEnrich: *refreshEnrich,
		Containment: *containment, Force: *force,
	}, func(_ string, pct int, msg string) {
		fmt.Printf("[%3d%%] %s\n", pct, msg)
	})
	if err != nil {
		return err
	}
	fmt.Printf("done: %s\n", res.RunDir)
	return nil
}

// splitPositional returns the first non-flag argument (the repo URL/path) and
// the remaining args with it removed, so flags may appear on either side.
// A value following a known value-flag is skipped so it isn't mistaken for the
// positional (e.g. "--data-dir ./out <url>").
func splitPositional(args []string) (pos string, rest []string) {
	valueFlags := map[string]bool{
		"--data-dir": true, "-data-dir": true,
		"--default-branch": true, "-default-branch": true,
	}
	for i := 0; i < len(args); i++ {
		a := args[i]
		if pos == "" && len(a) > 0 && a[0] != '-' {
			pos = a
			continue
		}
		rest = append(rest, a)
		// skip the value that belongs to a "--flag value" form
		if valueFlags[a] && i+1 < len(args) {
			i++
			rest = append(rest, args[i])
		}
	}
	return pos, rest
}

func short(sha string) string {
	if len(sha) > 7 {
		return sha[:7]
	}
	return sha
}
