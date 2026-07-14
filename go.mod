// devps — lists every dev server you left running: port, project
// directory, git branch, age. Joins listening sockets to working
// directories and git context instead of printing bare PIDs.
//
// version:    0.1.0
// author:     JaydenCJ
// license:    MIT
// repository: https://github.com/JaydenCJ/devps
// keywords:   ports, dev-server, procfs, git, localhost, process, cli
//
// Zero runtime dependencies: standard library only.
module github.com/JaydenCJ/devps

go 1.22
