//go:build windows

package bridgemate

import (
	"database/sql"
	"fmt"
	"io"
	"os"
	"syscall"
	"unsafe"

	_ "github.com/alexbrainman/odbc"
)

// Open opens a .bws through the Access ODBC driver. The driver is 32-bit only, so the program must
// be built with GOARCH=386.
func Open(path string) (*sql.DB, error) {
	db, err := sql.Open("odbc", "Driver={Microsoft Access Driver (*.mdb)};DBQ="+path+";")
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("%s: %w (is the program 32-bit? the Access driver is)", path, err)
	}
	return db, nil
}

// Create copies Bridgemate's template to a new .bws.
func Create(template, path string) error {
	in, err := os.Open(template)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

// ComputerName is the name BCS matches Clients.Computer against: GetComputerNameW's (NetBIOS, case
// as Windows has it), not os.Hostname's DNS name.
func ComputerName() (string, error) {
	buf := make([]uint16, 64)
	n := uint32(len(buf))
	proc := syscall.NewLazyDLL("kernel32.dll").NewProc("GetComputerNameW")
	if r, _, err := proc.Call(uintptr(unsafe.Pointer(&buf[0])), uintptr(unsafe.Pointer(&n))); r == 0 {
		return "", err
	}
	return syscall.UTF16ToString(buf[:n]), nil
}
