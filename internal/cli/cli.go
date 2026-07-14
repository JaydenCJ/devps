// Package cli implements the devps command-line interface. Run takes argv
// and two writers and returns an exit code, so the whole surface is
// testable in-process against a fabricated proc tree.
package cli

import (
	"flag"
	"fmt"
	"io"
	"strconv"
	"time"

	"github.com/JaydenCJ/devps/internal/render"
	"github.com/JaydenCJ/devps/internal/scan"
	"github.com/JaydenCJ/devps/internal/version"
)

// Exit codes, documented in the README. `kill` uses ExitNone when nothing
// matched or a target was refused, so scripts can branch on the verdict.
const (
	ExitOK      = 0
	ExitNone    = 1
	ExitUsage   = 2
	ExitRuntime = 3
)

// defaultProcRoot is the live kernel view; --proc-root overrides it for
// tests and for inspecting a container's proc mounted elsewhere.
const defaultProcRoot = "/proc"

// Run dispatches argv and returns the process exit code.
func Run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		return runList(nil, stdout, stderr)
	}
	switch args[0] {
	case "list":
		return runList(args[1:], stdout, stderr)
	case "kill":
		return runKill(args[1:], stdout, stderr)
	case "version", "--version", "-v":
		fmt.Fprintf(stdout, "devps %s\n", version.Version)
		return ExitOK
	case "help", "--help", "-h":
		usage(stdout)
		return ExitOK
	default:
		// Bare flags or bare ports: treat as `list …`.
		return runList(args, stdout, stderr)
	}
}

// listFlags are shared by list and kill (kill scans before signalling).
type listFlags struct {
	procRoot string
	noGit    bool
}

func (l *listFlags) register(fs *flag.FlagSet) {
	fs.StringVar(&l.procRoot, "proc-root", defaultProcRoot, "proc filesystem root to scan")
	fs.BoolVar(&l.noGit, "no-git", false, "skip repository lookup (project falls back to the directory name; branch shows -)")
}

func runList(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("list", flag.ContinueOnError)
	fs.SetOutput(stderr)
	var lf listFlags
	lf.register(fs)
	format := fs.String("format", "text", "output format: text or json")
	all := fs.Bool("all", false, "show every listener, including infrastructure daemons")
	wide := fs.Bool("wide", false, "full directories, user, and argv columns")
	if err := fs.Parse(args); err != nil {
		return ExitUsage
	}
	if *format != "text" && *format != "json" {
		fmt.Fprintf(stderr, "devps: unknown --format %q (want text or json)\n", *format)
		return ExitUsage
	}
	ports, code := parsePorts(fs.Args(), stderr)
	if code != ExitOK {
		return code
	}
	res, err := scan.Scan(scan.Options{
		ProcRoot: lf.procRoot,
		All:      *all,
		Ports:    ports,
		NoGit:    lf.noGit,
		Now:      time.Now(),
	})
	if err != nil {
		fmt.Fprintf(stderr, "devps: %v\n", err)
		return ExitRuntime
	}
	if *format == "json" {
		if err := render.JSON(stdout, res); err != nil {
			fmt.Fprintf(stderr, "devps: %v\n", err)
			return ExitRuntime
		}
		return ExitOK
	}
	render.Text(stdout, res, *wide)
	return ExitOK
}

// parsePorts validates the optional positional port arguments.
func parsePorts(rest []string, stderr io.Writer) ([]int, int) {
	var ports []int
	for _, a := range rest {
		p, err := strconv.Atoi(a)
		if err != nil || p < 1 || p > 65535 {
			fmt.Fprintf(stderr, "devps: %q is not a port (want 1-65535)\n", a)
			return nil, ExitUsage
		}
		ports = append(ports, p)
	}
	return ports, ExitOK
}

func usage(w io.Writer) {
	fmt.Fprintf(w, `devps %s — which dev servers did I leave running?

Usage:
  devps [list] [flags] [port ...]   list dev servers: port, project, branch, age
  devps kill [flags] <port ...>     signal the dev server(s) on the given port(s)
  devps version                     print the version

List flags:
  --format FORMAT   text (default) or json
  --all             show every listener, including infrastructure daemons
  --wide            full directories, user, and argv columns
  --no-git          skip repository lookup
  --proc-root DIR   proc filesystem root (default /proc)

Kill flags:
  --signal SIG      TERM (default), INT, HUP, QUIT, KILL, USR1, USR2, or a number
  --force           allow signalling infra and non-project listeners
  --dry-run         print what would be signalled without sending anything
  --no-git          skip repository lookup
  --proc-root DIR   proc filesystem root (default /proc)

Exit codes: 0 ok · 1 no match / refused · 2 usage error · 3 runtime error
`, version.Version)
}
