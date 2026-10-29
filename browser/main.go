package main

import (
	"flag"
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"
)

// Build metadata. Stamped by the Makefile:
//
//	go build -ldflags "-X main.version=0.2.0 -X main.commit=$(git rev-parse --short HEAD) -X main.buildDate=$(date -u +%Y-%m-%dT%H:%M:%SZ)"
var (
	version   = "dev"
	commit    = "none"
	buildDate = "unknown"
)

func main() {
	downloadDir := flag.String("download-dir", "./tabs", "default directory for downloaded Guitar Pro files")
	workerPath := flag.String("worker", "download-worker.mjs", "path to the Puppeteer download worker")
	logDir := flag.String("log-dir", "./logs", "directory for optional debug logs")
	dashboard := flag.Bool("dashboard", false, "run the local web dashboard instead of the terminal UI")
	listen := flag.String("listen", "127.0.0.1:8787", "dashboard listen address")
	showVersion := flag.Bool("version", false, "print version information and exit")
	flag.Parse()
	if *showVersion {
		fmt.Printf("echoheistv2 %s (commit %s, built %s)\n", version, commit, buildDate)
		return
	}
	c, e := OpenCache()
	if e != nil {
		fmt.Fprintln(os.Stderr, "cache:", e)
		os.Exit(1)
	}
	defer c.Close()
	m := newModel(NewFetcher(), c)
	m.downloadDir = *downloadDir
	m.workerPath = *workerPath
	m.logDir = *logDir
	if *dashboard {
		if e = runDashboard(*listen, NewFetcher(), c, dashboardConfig{DownloadDir: *downloadDir, WorkerPath: *workerPath, LogDir: *logDir}); e != nil {
			fmt.Fprintln(os.Stderr, e)
			os.Exit(1)
		}
		return
	}
	p := tea.NewProgram(m, tea.WithAltScreen())
	if _, e = p.Run(); e != nil {
		fmt.Fprintln(os.Stderr, e)
		os.Exit(1)
	}
}
