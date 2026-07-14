// Package render turns a scan result into terminal tables or stable JSON.
// Rendering is a pure function of the result: identical input produces
// byte-identical output, which the tests rely on.
package render

import (
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/JaydenCJ/devps/internal/scan"
)

// Text writes the human table. wide switches the PROJECT column to the
// full working directory and appends USER and ARGV columns.
func Text(w io.Writer, res scan.Result, wide bool) {
	if len(res.Listeners) == 0 {
		fmt.Fprintln(w, "no dev servers listening")
		footer(w, res)
		return
	}
	var header []string
	if wide {
		header = []string{"PORT", "ADDR", "PID", "USER", "COMMAND", "DIR", "BRANCH", "AGE", "ARGV"}
	} else {
		header = []string{"PORT", "ADDR", "PID", "COMMAND", "PROJECT", "BRANCH", "AGE"}
	}
	rows := [][]string{header}
	for i := range res.Listeners {
		l := &res.Listeners[i]
		if wide {
			rows = append(rows, []string{
				fmt.Sprintf("%d", l.Port), displayAddr(l.Addresses),
				fmt.Sprintf("%d", l.PID), l.User, l.Command,
				dash(l.Dir), branchCell(l), Age(l.Age), l.Argv,
			})
		} else {
			rows = append(rows, []string{
				fmt.Sprintf("%d", l.Port), displayAddr(l.Addresses),
				fmt.Sprintf("%d", l.PID), l.Command,
				dash(l.Project), branchCell(l), Age(l.Age),
			})
		}
	}
	writeAligned(w, rows)
	footer(w, res)
}

// footer prints the hidden/unowned notes that keep the default view honest
// about what it is not showing.
func footer(w io.Writer, res scan.Result) {
	if res.Hidden > 0 {
		fmt.Fprintf(w, "\n%d other listener%s hidden (rerun with --all to show)\n",
			res.Hidden, plural(res.Hidden))
	}
	if res.Unowned > 0 {
		fmt.Fprintf(w, "%d listening port%s owned by another user (rerun with sudo to map)\n",
			res.Unowned, plural(res.Unowned))
	}
}

// writeAligned pads every column to its widest cell. The last column is
// left unpadded so long argv strings do not trail spaces.
func writeAligned(w io.Writer, rows [][]string) {
	widths := make([]int, len(rows[0]))
	for _, row := range rows {
		for i, cell := range row {
			if len(cell) > widths[i] {
				widths[i] = len(cell)
			}
		}
	}
	for _, row := range rows {
		var b strings.Builder
		for i, cell := range row {
			if i == len(row)-1 {
				b.WriteString(cell)
				break
			}
			b.WriteString(cell)
			b.WriteString(strings.Repeat(" ", widths[i]-len(cell)+2))
		}
		fmt.Fprintln(w, strings.TrimRight(b.String(), " "))
	}
}

// displayAddr collapses the address list for the table: a wildcard bind
// renders as "*", multiple specific binds join with commas.
func displayAddr(addrs []string) string {
	for _, a := range addrs {
		if a == "0.0.0.0" || a == "::" {
			return "*"
		}
	}
	return strings.Join(addrs, ",")
}

// branchCell renders git context: branch name, "detached@<hash>", "?" for
// a repo whose HEAD was unreadable, "-" outside any repository.
func branchCell(l *scan.Listener) string {
	switch {
	case l.Detached:
		return "detached@" + l.Branch
	case l.Branch != "":
		return l.Branch
	case l.GitRoot != "":
		return "?"
	default:
		return "-"
	}
}

func dash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}

// Age humanizes a duration for the table: seconds under a minute, whole
// minutes under an hour, "3h12m" under a day, "2d4h" under ten days, then
// whole days. Precision drops as durations grow because "37d" answers the
// question and "37d6h41m" does not.
func Age(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	s := int64(d.Seconds())
	switch {
	case s < 60:
		return fmt.Sprintf("%ds", s)
	case s < 3600:
		return fmt.Sprintf("%dm", s/60)
	case s < 86400:
		h, m := s/3600, (s%3600)/60
		if m == 0 {
			return fmt.Sprintf("%dh", h)
		}
		return fmt.Sprintf("%dh%02dm", h, m)
	case s < 10*86400:
		days, h := s/86400, (s%86400)/3600
		if h == 0 {
			return fmt.Sprintf("%dd", days)
		}
		return fmt.Sprintf("%dd%dh", days, h)
	default:
		return fmt.Sprintf("%dd", s/86400)
	}
}
