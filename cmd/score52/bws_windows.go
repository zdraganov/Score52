//go:build windows

package main

import (
	"database/sql"
	"fmt"
	"os"
	"os/exec"
	"syscall"

	"github.com/zdraganov/Score52/internal/bridgemate"
	"github.com/zdraganov/Score52/internal/web"
)

// winHost is BridgeMate on this PC: Bridgemate's template, the 32-bit Access driver (so this build is
// GOARCH=386) and BCS.
type winHost struct{ template, bcs string }

func bridgemateHost(template, bcs string) web.BwsHost { return winHost{template, bcs} }

func (h winHost) Create(path string) error {
	if _, err := os.Stat(h.template); err != nil {
		return fmt.Errorf("няма шаблон на Bridgemate (%s): сложете Template_Access2000.bws до програмата или посочете го с -bws-template", h.template)
	}
	return bridgemate.Create(h.template, path)
}

func (h winHost) Open(path string) (*sql.DB, error) { return bridgemate.Open(path) }

func (h winHost) Computer() (string, error) { return bridgemate.ComputerName() }

// Start runs BCS on the file: reset the server, upload the tables, read results. The path goes in
// square brackets, as BCS wants it, so the command line is written out as it is.
func (h winHost) Start(path string) error {
	if _, err := os.Stat(h.bcs); err != nil {
		return fmt.Errorf("няма BCS в %s (посочете го с -bcs)", h.bcs)
	}
	cmd := exec.Command(h.bcs)
	cmd.SysProcAttr = &syscall.SysProcAttr{CmdLine: fmt.Sprintf(`"%s" /f:[%s] /s /r`, h.bcs, path)}
	return cmd.Start()
}
