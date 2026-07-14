// Tests for the terminal table, the JSON envelope, and age humanization.
// Rendering is a pure function of the scan result, so every expectation
// here is an exact string.
package render

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/JaydenCJ/devps/internal/classify"
	"github.com/JaydenCJ/devps/internal/scan"
)

func sampleResult() scan.Result {
	return scan.Result{
		Listeners: []scan.Listener{
			{
				Port: 3000, Addresses: []string{"127.0.0.1"}, PID: 4102, UID: 1000,
				User: "dev", Command: "next", Argv: "node /srv/store/node_modules/.bin/next dev",
				Kind: classify.Dev, Dir: "/srv/store", Project: "store",
				GitRoot: "/srv/store", Branch: "main", Age: 2 * time.Hour,
			},
			{
				Port: 5173, Addresses: []string{"0.0.0.0", "::"}, PID: 4188, UID: 1000,
				User: "dev", Command: "vite", Argv: "node /srv/shop/node_modules/.bin/vite",
				Kind: classify.Dev, Dir: "/srv/shop", Project: "shop",
				GitRoot: "/srv/shop", Branch: "feature/checkout", Age: 50 * time.Hour,
			},
		},
	}
}

func TestTextTableHasHeaderAndAlignedColumns(t *testing.T) {
	var buf bytes.Buffer
	Text(&buf, sampleResult(), false)
	lines := strings.Split(strings.TrimRight(buf.String(), "\n"), "\n")
	if len(lines) != 3 {
		t.Fatalf("got %d lines, want header + 2 rows:\n%s", len(lines), buf.String())
	}
	if !strings.HasPrefix(lines[0], "PORT") || !strings.Contains(lines[0], "BRANCH") {
		t.Fatalf("header = %q", lines[0])
	}
	// Column starts must line up between header and rows.
	col := strings.Index(lines[0], "COMMAND")
	if col < 0 || lines[1][col:col+4] != "next" || lines[2][col:col+4] != "vite" {
		t.Fatalf("COMMAND column misaligned:\n%s", buf.String())
	}
}

func TestTextWildcardBindCollapsesToStar(t *testing.T) {
	var buf bytes.Buffer
	Text(&buf, sampleResult(), false)
	if !strings.Contains(buf.String(), "*") {
		t.Fatalf("0.0.0.0+:: must render as *:\n%s", buf.String())
	}
	if strings.Contains(buf.String(), "0.0.0.0,::") {
		t.Fatalf("wildcard pair must not be spelled out:\n%s", buf.String())
	}
}

func TestTextBranchCellVariants(t *testing.T) {
	cases := []struct {
		name string
		l    scan.Listener
		want string
	}{
		{"named branch", scan.Listener{GitRoot: "/r", Branch: "main"}, "main"},
		{"detached", scan.Listener{GitRoot: "/r", Branch: "9fceb02", Detached: true}, "detached@9fceb02"},
		{"repo without HEAD", scan.Listener{GitRoot: "/r"}, "?"},
		{"no repository", scan.Listener{}, "-"},
	}
	for _, c := range cases {
		if got := branchCell(&c.l); got != c.want {
			t.Errorf("%s: branchCell = %q, want %q", c.name, got, c.want)
		}
	}
}

func TestTextEmptyStateMessage(t *testing.T) {
	var buf bytes.Buffer
	Text(&buf, scan.Result{}, false)
	if got := buf.String(); got != "no dev servers listening\n" {
		t.Fatalf("empty output = %q", got)
	}
}

func TestTextFooterReportsHiddenAndUnowned(t *testing.T) {
	var buf bytes.Buffer
	Text(&buf, scan.Result{Hidden: 3, Unowned: 1}, false)
	out := buf.String()
	if !strings.Contains(out, "3 other listeners hidden (rerun with --all to show)") {
		t.Fatalf("hidden note missing:\n%s", out)
	}
	if !strings.Contains(out, "1 listening port owned by another user") {
		t.Fatalf("unowned note missing:\n%s", out)
	}
	// Singular counts must not read "1 listeners".
	buf.Reset()
	Text(&buf, scan.Result{Hidden: 1}, false)
	if !strings.Contains(buf.String(), "1 other listener hidden") {
		t.Fatalf("singular form wrong:\n%s", buf.String())
	}
}

func TestWideTableIncludesDirUserAndArgv(t *testing.T) {
	var buf bytes.Buffer
	Text(&buf, sampleResult(), true)
	out := buf.String()
	for _, want := range []string{"DIR", "USER", "ARGV", "/srv/shop", "node /srv/shop/node_modules/.bin/vite"} {
		if !strings.Contains(out, want) {
			t.Fatalf("wide output missing %q:\n%s", want, out)
		}
	}
}

func TestWideTableHasNoTrailingSpaces(t *testing.T) {
	var buf bytes.Buffer
	Text(&buf, sampleResult(), true)
	for _, line := range strings.Split(buf.String(), "\n") {
		if strings.HasSuffix(line, " ") {
			t.Fatalf("trailing space on %q", line)
		}
	}
}

func TestTextOutputIsDeterministic(t *testing.T) {
	var a, b bytes.Buffer
	Text(&a, sampleResult(), false)
	Text(&b, sampleResult(), false)
	if a.String() != b.String() {
		t.Fatal("identical input must render byte-identically")
	}
}

func TestJSONEnvelopeShape(t *testing.T) {
	var buf bytes.Buffer
	if err := JSON(&buf, sampleResult()); err != nil {
		t.Fatal(err)
	}
	var env map[string]any
	if err := json.Unmarshal(buf.Bytes(), &env); err != nil {
		t.Fatalf("output is not valid JSON: %v", err)
	}
	if env["tool"] != "devps" || env["schema_version"] != float64(1) {
		t.Fatalf("envelope = %v", env)
	}
	listeners := env["listeners"].([]any)
	if len(listeners) != 2 {
		t.Fatalf("got %d listeners", len(listeners))
	}
	first := listeners[0].(map[string]any)
	if first["port"] != float64(3000) || first["branch"] != "main" || first["kind"] != "dev" {
		t.Fatalf("first row = %v", first)
	}
	if first["age_seconds"] != float64(7200) || first["age"] != "2h" {
		t.Fatalf("age fields = %v / %v", first["age_seconds"], first["age"])
	}
}

func TestJSONListenersIsArrayNotNullWhenEmpty(t *testing.T) {
	var buf bytes.Buffer
	if err := JSON(&buf, scan.Result{Hidden: 2}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), `"listeners": []`) {
		t.Fatalf("empty listeners must be [], got:\n%s", buf.String())
	}
	if !strings.Contains(buf.String(), `"hidden": 2`) {
		t.Fatalf("hidden count missing:\n%s", buf.String())
	}
}

func TestJSONOmitsEmptyGitFields(t *testing.T) {
	var buf bytes.Buffer
	res := scan.Result{Listeners: []scan.Listener{{
		Port: 80, Addresses: []string{"0.0.0.0"}, PID: 1, Command: "nginx",
		Kind: classify.Infra, Argv: "nginx", User: "root",
	}}}
	if err := JSON(&buf, res); err != nil {
		t.Fatal(err)
	}
	for _, absent := range []string{`"branch"`, `"git_root"`, `"project"`, `"detached"`} {
		if strings.Contains(buf.String(), absent) {
			t.Fatalf("field %s must be omitted when empty:\n%s", absent, buf.String())
		}
	}
}

func TestAgeBucketsUpToADay(t *testing.T) {
	cases := map[time.Duration]string{
		0:                               "0s",
		-3 * time.Second:                "0s", // clock skew must never render "-3s"
		42 * time.Second:                "42s",
		59*time.Minute + 59*time.Second: "59m",
		3*time.Hour + 5*time.Minute:     "3h05m",
		4 * time.Hour:                   "4h",
	}
	for d, want := range cases {
		if got := Age(d); got != want {
			t.Errorf("Age(%v) = %q, want %q", d, got, want)
		}
	}
}

func TestAgeBucketsBeyondADay(t *testing.T) {
	// Precision drops as durations grow: "37d" answers the question,
	// "37d6h41m" does not.
	cases := map[time.Duration]string{
		2*24*time.Hour + 4*time.Hour:  "2d4h",
		3 * 24 * time.Hour:            "3d",
		37*24*time.Hour + 6*time.Hour: "37d",
	}
	for d, want := range cases {
		if got := Age(d); got != want {
			t.Errorf("Age(%v) = %q, want %q", d, got, want)
		}
	}
}
