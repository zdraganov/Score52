// Score52: bridge scoring for the Bulgarian Bridge Federation. It serves its pages on 127.0.0.1 and
// opens them in the browser; every event is a file in the data folder.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"time"

	"github.com/zdraganov/Score52/internal/web"
)

func main() {
	home, _ := os.UserHomeDir()
	addr := flag.String("addr", "127.0.0.1:47652", "where to serve the pages (keep it on 127.0.0.1)")
	dir := flag.String("data", filepath.Join(home, "Documents", "Score52"), "folder with the event files")
	noBrowser := flag.Bool("no-browser", false, "don't open the browser")
	exe, _ := os.Executable()
	template := flag.String("bws-template", filepath.Join(filepath.Dir(exe), "Template_Access2000.bws"),
		"Bridgemate's empty database (Template_Access2000.bws), copied for each BridgeMate file (Windows)")
	bcs := flag.String("bcs", `C:\Program Files (x86)\Bridgemate Pro\BMPro.exe`, "Bridgemate Control Software (Windows)")
	flag.Parse()

	srv, err := web.New(*dir)
	if err != nil {
		log.Fatal(err)
	}
	defer srv.Close()
	if h := bridgemateHost(*template, *bcs); h != nil {
		srv.SetBridgemate(h)
	}

	ln, err := net.Listen("tcp", *addr)
	if err != nil {
		// another copy probably holds the port: show its pages instead of failing
		url := "http://" + *addr + "/"
		fmt.Printf("Score52 вече работи: %s\n", url)
		if !*noBrowser {
			openBrowser(url)
		}
		return
	}
	url := "http://" + ln.Addr().String() + "/"
	fmt.Printf("Score52 работи на %s\nСъстезанията са в %s\nЗатварянето на този прозорец спира програмата.\n", url, *dir)
	if !*noBrowser {
		openBrowser(url)
	}

	hs := &http.Server{Handler: srv.Handler(), ReadHeaderTimeout: 10 * time.Second}
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt)
	go func() {
		<-stop
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		hs.Shutdown(ctx)
	}()
	if err := hs.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatal(err)
	}
}

func openBrowser(url string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	case "darwin":
		cmd = exec.Command("open", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	cmd.Start()
}
