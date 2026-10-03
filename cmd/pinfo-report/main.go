// pinfo-report — the plaso-pinfo analogue for a tool output tree
// (docs/linux §3.5): walk one or more OUT_DIR roots and print, as JSONL on
// stdout, one row per record file — tool, item, record count, EventTime
// span, the RecordType, snapshot and residue sets seen.
//
// It reads outputs only and reports on runs, not evidence: no joining, no
// identities, no substitute for byakugan (the rules). An operator/debug
// tool; the DX_DFIR gate consumes the same package directly when it needs
// to.
//
//	pinfo-report OUT_DIR [OUT_DIR...]
package main

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/Get-Sybers/gopinfo"
	"github.com/Get-Sybers/gopinfo/report"
)

func main() {
	args := os.Args[1:]
	if len(args) == 1 && (args[0] == "--version" || args[0] == "-version") {
		fmt.Printf("pinfo-report %s\n", gopinfo.Version)
		return
	}
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "usage: pinfo-report OUT_DIR [OUT_DIR...]")
		os.Exit(1)
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetEscapeHTML(false)
	code := 0
	for _, root := range args {
		items, err := report.Tree(root)
		if err != nil {
			fmt.Fprintf(os.Stderr, "pinfo-report: %s: %v\n", root, err)
			code = 1
		}
		for _, it := range items {
			if err := enc.Encode(it); err != nil {
				fmt.Fprintf(os.Stderr, "pinfo-report: %v\n", err)
				os.Exit(1)
			}
		}
	}
	os.Exit(code)
}
