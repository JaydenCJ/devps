// Tests for process classification. Each case reproduces the argv shape
// the real tool produces, bin shims and all, so the rules are checked
// against what actually shows up in /proc/<pid>/cmdline.
package classify

import "testing"

func assertClass(t *testing.T, comm string, argv []string, exe, wantLabel string, wantKind Kind) {
	t.Helper()
	label, kind := Classify(comm, argv, exe)
	if label != wantLabel || kind != wantKind {
		t.Fatalf("Classify(%q, %q) = (%q, %v), want (%q, %v)",
			comm, argv, label, kind, wantLabel, wantKind)
	}
}

func TestNodeBundlerDevServers(t *testing.T) {
	// The bin shim lives in node_modules/.bin, so only basename matching
	// finds the tool wherever the project is checked out.
	assertClass(t, "node",
		[]string{"node", "/srv/shop/node_modules/.bin/vite"}, "/usr/bin/node",
		"vite", Dev)
	assertClass(t, "node",
		[]string{"node", "/srv/app/node_modules/.bin/next", "dev"}, "/usr/bin/node",
		"next", Dev)
	assertClass(t, "node",
		[]string{"node", "/srv/cra/node_modules/.bin/react-scripts", "start"}, "/usr/bin/node",
		"react-scripts", Dev)
}

func TestNodeToolingServeCommands(t *testing.T) {
	// Both the dedicated webpack binary and `webpack serve` must resolve
	// to the same label so counts aggregate.
	assertClass(t, "node",
		[]string{"node", "/x/node_modules/.bin/webpack-dev-server"}, "",
		"webpack-dev-server", Dev)
	assertClass(t, "node",
		[]string{"node", "/x/node_modules/.bin/webpack", "serve"}, "",
		"webpack-dev-server", Dev)
	assertClass(t, "node",
		[]string{"node", "/x/node_modules/.bin/ng", "serve"}, "",
		"ng serve", Dev)
	assertClass(t, "node",
		[]string{"node", "/x/node_modules/.bin/storybook", "dev", "-p", "6006"}, "",
		"storybook", Dev)
	assertClass(t, "node",
		[]string{"node", "/x/node_modules/.bin/http-server", "-p", "8081"}, "",
		"http-server", Dev)
}

func TestPythonDevServers(t *testing.T) {
	assertClass(t, "python3",
		[]string{"python3", "-m", "http.server", "8000"}, "/usr/bin/python3",
		"python http.server", Dev)
	assertClass(t, "python3",
		[]string{"python3", "manage.py", "runserver", "0.0.0.0:8000"}, "",
		"django runserver", Dev)
	assertClass(t, "flask",
		[]string{"/venv/bin/flask", "run", "--debug"}, "",
		"flask", Dev)
	assertClass(t, "uvicorn",
		[]string{"/venv/bin/uvicorn", "app.main:app", "--reload"}, "",
		"uvicorn", Dev)
	assertClass(t, "jupyter-lab",
		[]string{"/venv/bin/python3", "/venv/bin/jupyter-lab", "--no-browser"}, "",
		"jupyter", Dev)
}

func TestRubyPHPElixirHugoDevServers(t *testing.T) {
	// `rails server` and its `rails s` shorthand must land on one label.
	assertClass(t, "ruby",
		[]string{"ruby", "bin/rails", "server"}, "", "rails server", Dev)
	assertClass(t, "ruby",
		[]string{"ruby", "bin/rails", "s"}, "", "rails server", Dev)
	assertClass(t, "php",
		[]string{"php", "-S", "127.0.0.1:8080"}, "", "php -S", Dev)
	assertClass(t, "beam.smp",
		[]string{"/usr/bin/elixir", "/usr/bin/mix", "phx.server"}, "",
		"phoenix", Dev)
	assertClass(t, "hugo",
		[]string{"hugo", "server", "-D"}, "", "hugo server", Dev)
}

func TestCompiledLanguageDevLoops(t *testing.T) {
	// `go run ./cmd/api` leaves no "go" in argv of the final process; the
	// build-cache exe path is the only reliable fingerprint.
	assertClass(t, "api",
		[]string{"/tmp/go-build2087553012/b001/exe/api"},
		"/tmp/go-build2087553012/b001/exe/api",
		"go run (api)", Dev)
	assertClass(t, "cargo",
		[]string{"/home/dev/.cargo/bin/cargo", "run"}, "", "cargo run", Dev)
	assertClass(t, "cargo",
		[]string{"cargo", "watch", "-x", "run"}, "", "cargo run", Dev)
}

func TestNpmRunDevScriptRunner(t *testing.T) {
	assertClass(t, "npm",
		[]string{"npm", "run", "dev"}, "", "npm run dev", Dev)
	assertClass(t, "node",
		[]string{"pnpm", "dev"}, "", "pnpm dev", Dev)
}

func TestInterpreterFallbacksNameTheScript(t *testing.T) {
	// An unrecognized interpreter script is not certified as a dev server,
	// but the label still names the script so the row reads as what it is.
	assertClass(t, "node",
		[]string{"node", "--inspect", "/srv/api/server.js"}, "/usr/bin/node",
		"node server.js", Other)
	assertClass(t, "python3",
		[]string{"python3", "/srv/tools/webhook.py"}, "",
		"python3 webhook.py", Other)
}

func TestInfraDaemonsAreClassifiedInfra(t *testing.T) {
	assertClass(t, "sshd",
		[]string{"sshd: /usr/sbin/sshd -D [listener]"}, "", "sshd", Infra)
	assertClass(t, "postgres",
		[]string{"/usr/lib/postgresql/16/bin/postgres", "-D", "/var/lib/postgresql"}, "",
		"postgres", Infra)
	assertClass(t, "docker-proxy",
		[]string{"/usr/bin/docker-proxy", "-proto", "tcp", "-host-port", "5432"}, "",
		"docker-proxy", Infra)
	// Matching is case-sensitive on purpose: these are spelled like this.
	assertClass(t, "NetworkManager", []string{"/usr/sbin/NetworkManager"}, "",
		"NetworkManager", Infra)
}

func TestUnknownProcessesFallBackToComm(t *testing.T) {
	assertClass(t, "myserver",
		[]string{"/opt/things/myserver", "--listen", ":9000"}, "/opt/things/myserver",
		"myserver", Other)
	assertClass(t, "kworker/0:1", nil, "", "kworker/0:1", Other)
}

func TestKindStringNames(t *testing.T) {
	if Dev.String() != "dev" || Other.String() != "other" || Infra.String() != "infra" {
		t.Fatalf("Kind names = %q/%q/%q, want dev/other/infra",
			Dev.String(), Other.String(), Infra.String())
	}
}
