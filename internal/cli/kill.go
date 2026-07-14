package cli

import (
	"flag"
	"fmt"
	"io"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/JaydenCJ/devps/internal/classify"
	"github.com/JaydenCJ/devps/internal/scan"
)

// sendSignal delivers a signal to a pid. A package variable so tests can
// intercept it and assert on (pid, signal) without touching real processes.
var sendSignal = func(pid int, sig syscall.Signal) error {
	return syscall.Kill(pid, sig)
}

// signalNames maps the signals that make sense against a dev server.
var signalNames = map[string]syscall.Signal{
	"TERM": syscall.SIGTERM,
	"INT":  syscall.SIGINT,
	"HUP":  syscall.SIGHUP,
	"QUIT": syscall.SIGQUIT,
	"KILL": syscall.SIGKILL,
	"USR1": syscall.SIGUSR1,
	"USR2": syscall.SIGUSR2,
}

// parseSignal accepts "TERM", "SIGTERM", "term", or a raw number.
func parseSignal(s string) (syscall.Signal, string, error) {
	name := strings.TrimPrefix(strings.ToUpper(s), "SIG")
	if sig, ok := signalNames[name]; ok {
		return sig, "SIG" + name, nil
	}
	if n, err := strconv.Atoi(s); err == nil && n >= 1 && n <= 64 {
		return syscall.Signal(n), fmt.Sprintf("signal %d", n), nil
	}
	return 0, "", fmt.Errorf("unknown signal %q (want TERM, INT, HUP, QUIT, KILL, USR1, USR2, or a number)", s)
}

func runKill(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("kill", flag.ContinueOnError)
	fs.SetOutput(stderr)
	var lf listFlags
	lf.register(fs)
	signalName := fs.String("signal", "TERM", "signal to send")
	force := fs.Bool("force", false, "allow signalling infra and non-project listeners")
	dryRun := fs.Bool("dry-run", false, "print what would be signalled without sending anything")
	if err := fs.Parse(args); err != nil {
		return ExitUsage
	}
	if len(fs.Args()) == 0 {
		fmt.Fprintf(stderr, "devps kill: at least one port is required\n")
		return ExitUsage
	}
	ports, code := parsePorts(fs.Args(), stderr)
	if code != ExitOK {
		return code
	}
	sig, sigLabel, err := parseSignal(*signalName)
	if err != nil {
		fmt.Fprintf(stderr, "devps kill: %v\n", err)
		return ExitUsage
	}

	// kill sees every listener (--all semantics): the guard below, not the
	// default-view filter, decides what may be signalled.
	res, err := scan.Scan(scan.Options{
		ProcRoot: lf.procRoot,
		All:      true,
		Ports:    ports,
		NoGit:    lf.noGit,
		Now:      time.Now(),
	})
	if err != nil {
		fmt.Fprintf(stderr, "devps: %v\n", err)
		return ExitRuntime
	}

	matched := map[int][]scan.Listener{}
	for _, l := range res.Listeners {
		matched[l.Port] = append(matched[l.Port], l)
	}

	exit := ExitOK
	signalled := map[int]bool{} // one signal per pid, even across ports
	for _, port := range ports {
		targets := matched[port]
		if len(targets) == 0 {
			fmt.Fprintf(stderr, "devps kill: no listener on port %d\n", port)
			exit = ExitNone
			continue
		}
		for _, l := range targets {
			// Guard: a dev server or anything running inside a git repo is
			// fair game; sshd, postgres, and unclassified system processes
			// need --force. This is the difference from `kill $(lsof -t)`.
			if !*force && !(l.Kind == classify.Dev || (l.Kind == classify.Other && l.GitRoot != "")) {
				fmt.Fprintf(stderr, "devps kill: refusing %s (pid %d, port %d): %s listener — use --force\n",
					l.Command, l.PID, port, l.Kind)
				exit = ExitNone
				continue
			}
			if signalled[l.PID] {
				continue
			}
			signalled[l.PID] = true
			if *dryRun {
				fmt.Fprintf(stdout, "would send %s to pid %d (%s, port %d%s)\n",
					sigLabel, l.PID, l.Command, port, projectSuffix(l))
				continue
			}
			if err := sendSignal(l.PID, sig); err != nil {
				fmt.Fprintf(stderr, "devps kill: pid %d: %v\n", l.PID, err)
				exit = ExitNone
				continue
			}
			fmt.Fprintf(stdout, "sent %s to pid %d (%s, port %d%s)\n",
				sigLabel, l.PID, l.Command, port, projectSuffix(l))
		}
	}
	return exit
}

// projectSuffix renders ", project @ branch" when git context is known, so
// the kill confirmation names what was stopped in the user's own terms.
func projectSuffix(l scan.Listener) string {
	if l.Project == "" {
		return ""
	}
	if l.Branch == "" {
		return ", " + l.Project
	}
	return ", " + l.Project + " @ " + l.Branch
}
