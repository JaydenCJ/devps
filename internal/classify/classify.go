// Package classify labels a listening process from its command line: a
// development server someone launched by hand, background infrastructure,
// or something in between. Rules are data-driven and quotable — nothing is
// inferred from traffic or port numbers.
package classify

import (
	"path/filepath"
	"strings"
)

// Kind buckets a listener by how devps treats it in the default view.
type Kind int

const (
	// Dev is a recognized development server (vite, runserver, go run, …).
	Dev Kind = iota
	// Other is an unrecognized process. It is still shown by default when
	// its working directory sits inside a git repository — a strong signal
	// that it is project work, whatever it is called.
	Other
	// Infra is system or background infrastructure (sshd, postgres,
	// docker-proxy); hidden unless --all is set.
	Infra
)

func (k Kind) String() string {
	switch k {
	case Dev:
		return "dev"
	case Infra:
		return "infra"
	default:
		return "other"
	}
}

// infraNames are daemon process names that are never the dev server you
// were looking for. Matched against comm and argv[0] basename, exact and
// case-sensitive (NetworkManager, Xorg are spelled like this).
var infraNames = map[string]bool{
	"agetty": true, "apache2": true, "avahi-daemon": true, "chronyd": true,
	"containerd": true, "cupsd": true, "dnsmasq": true, "docker-proxy": true,
	"dockerd": true, "exim4": true, "httpd": true, "init": true,
	"kubelet": true, "mariadbd": true, "master": true, "memcached": true,
	"mongod": true, "mysqld": true, "NetworkManager": true, "nginx": true,
	"ntpd": true, "pipewire": true, "postgres": true, "redis-server": true,
	"rpcbind": true, "smbd": true, "sshd": true, "systemd": true,
	"systemd-network": true, "systemd-resolve": true, "systemd-resolved": true,
	"tailscaled": true, "wireplumber": true, "Xorg": true,
}

// Classify returns a short human label and the Kind for one process.
// comm is the kernel process name, argv the full command line, exe the
// resolved executable path (used to spot `go run` temp binaries).
func Classify(comm string, argv []string, exe string) (string, Kind) {
	bases := make([]string, len(argv))
	for i, a := range argv {
		bases[i] = strings.ToLower(filepath.Base(a))
	}
	has := func(names ...string) bool {
		for _, b := range bases {
			for _, n := range names {
				if b == n {
					return true
				}
			}
		}
		return false
	}

	switch {
	// Node ecosystem — the argv usually contains the tool's bin shim
	// (node_modules/.bin/vite), so basename matching finds it wherever
	// it is installed.
	case has("vite"):
		return "vite", Dev
	case has("next"):
		return "next", Dev
	case has("nuxt", "nuxi"):
		return "nuxt", Dev
	case has("astro"):
		return "astro", Dev
	case has("remix"):
		return "remix", Dev
	case has("react-scripts"):
		return "react-scripts", Dev
	case has("vue-cli-service"):
		return "vue-cli-service", Dev
	case has("ng") && has("serve"):
		return "ng serve", Dev
	case has("webpack-dev-server"), has("webpack") && has("serve"):
		return "webpack-dev-server", Dev
	case has("storybook"):
		return "storybook", Dev
	case has("http-server", "live-server", "browser-sync", "sirv"):
		return bases[firstIndex(bases, "http-server", "live-server", "browser-sync", "sirv")], Dev
	case len(bases) > 0 && bases[0] == "serve":
		return "serve", Dev

	// Python.
	case has("http.server"):
		return "python http.server", Dev
	case has("manage.py") && has("runserver"):
		return "django runserver", Dev
	case has("flask"):
		return "flask", Dev
	case has("uvicorn"):
		return "uvicorn", Dev
	case has("gunicorn"):
		return "gunicorn", Dev
	case has("mkdocs") && has("serve"):
		return "mkdocs serve", Dev
	case has("jupyter", "jupyter-lab", "jupyter-notebook"):
		return "jupyter", Dev

	// Ruby / PHP / Elixir / static-site generators.
	case has("rails") && has("server", "s"):
		return "rails server", Dev
	case comm == "puma":
		return "puma", Dev
	case has("jekyll") && has("serve", "server"):
		return "jekyll serve", Dev
	case has("php") && has("-s"):
		return "php -S", Dev
	case has("mix") && has("phx.server"):
		return "phoenix", Dev
	case has("hugo") && has("server", "serve"):
		return "hugo server", Dev

	// Compiled-language dev loops.
	case has("cargo") && has("run", "watch"):
		return "cargo run", Dev
	case has("dotnet") && has("run", "watch"):
		return "dotnet run", Dev
	case has("air"):
		return "air", Dev
	case strings.Contains(exe, "/go-build"):
		// `go run ./cmd/api` execs a throwaway binary out of the build
		// cache; the cache path is the reliable fingerprint.
		return "go run (" + comm + ")", Dev

	// Package-manager script runners: `npm run dev` and friends.
	case len(bases) > 0 && isScriptRunner(bases[0]) && has("dev", "start", "serve", "preview"):
		return strings.Join(bases[:min(3, len(bases))], " "), Dev
	}

	if infraNames[comm] || (len(argv) > 0 && infraNames[filepath.Base(argv[0])]) {
		return comm, Infra
	}

	// Interpreter fallbacks: name the script so `node server.js` reads as
	// what it is, even though we cannot vouch that it is a dev server.
	if len(bases) > 0 {
		switch {
		case bases[0] == "node" || bases[0] == "nodejs" || bases[0] == "deno" || bases[0] == "bun":
			if s := firstScript(argv); s != "" {
				return bases[0] + " " + filepath.Base(s), Other
			}
			return bases[0], Other
		case strings.HasPrefix(bases[0], "python") || strings.HasPrefix(bases[0], "ruby"):
			if s := firstScript(argv); s != "" {
				return bases[0] + " " + filepath.Base(s), Other
			}
			return bases[0], Other
		}
	}
	if comm != "" {
		return comm, Other
	}
	if len(bases) > 0 {
		return bases[0], Other
	}
	return "?", Other
}

// isScriptRunner reports whether base is a JS package-manager launcher.
func isScriptRunner(base string) bool {
	switch base {
	case "npm", "pnpm", "yarn", "npx", "bunx":
		return true
	}
	return false
}

// firstScript returns the first argument after argv[0] that does not look
// like a flag — for interpreters this is almost always the script path.
func firstScript(argv []string) string {
	for _, a := range argv[1:] {
		if !strings.HasPrefix(a, "-") {
			return a
		}
	}
	return ""
}

// firstIndex returns the first element of bases equal to any name; used to
// echo back which of several aliases matched.
func firstIndex(bases []string, names ...string) int {
	for i, b := range bases {
		for _, n := range names {
			if b == n {
				return i
			}
		}
	}
	return 0
}
