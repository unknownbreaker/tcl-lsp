// Command tcl-lsp is a Language Server Protocol server for TCL/RVT. It speaks
// LSP over stdio; logs go to stderr (stdout is the protocol channel).
// `tcl-lsp check <paths>` instead runs the Checks over files and directories
// and exits (package checkcmd).
package main

import (
	"log"
	"os"

	"github.com/unknownbreaker/tcl-lsp/internal/checkcmd"
	"github.com/unknownbreaker/tcl-lsp/internal/lsp"
)

func main() {
	log.SetOutput(os.Stderr)
	log.SetPrefix("tcl-lsp: ")
	if len(os.Args) > 1 && os.Args[1] == "check" {
		os.Exit(checkcmd.Run(os.Args[2:], os.Stdout, os.Stderr))
	}
	srv := lsp.NewServer(lsp.NewConn(os.Stdin, os.Stdout))
	if err := srv.Run(); err != nil {
		log.Fatalf("server error: %v", err)
	}
}
