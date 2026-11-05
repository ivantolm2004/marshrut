package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"marshrut/app"
	"marshrut/ipc"
	windowsruntime "marshrut/platform/windows"
	"marshrut/secure"
	"os"
	"os/signal"
	"path/filepath"
	"time"

	"golang.org/x/sys/windows/svc"
)

type handler struct {
	m     *app.Manager
	owner string
}

func (h handler) Execute(_ []string, requests <-chan svc.ChangeRequest, status chan<- svc.Status) (bool, uint32) {
	status <- svc.Status{State: svc.StartPending}
	l, e := ipc.Listen(h.owner)
	if e != nil {
		return false, 1
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go ipc.Serve(ctx, l, h.m)
	status <- svc.Status{State: svc.Running, Accepts: svc.AcceptStop | svc.AcceptShutdown}
	h.m.StartBackground()
	for c := range requests {
		switch c.Cmd {
		case svc.Interrogate:
			status <- c.CurrentStatus
		case svc.Stop, svc.Shutdown:
			status <- svc.Status{State: svc.StopPending}
			cancel()
			h.m.Close()
			return false, 0
		}
	}
	return false, 0
}
func main() {
	flag.CommandLine.SetOutput(io.Discard)
	mode := flag.String("mode", "rpc", "rpc | serve | service")
	owner := flag.String("owner", "", "Windows SID allowed to control the service")
	runtimeDir := flag.String("runtime", "", "Verified runtime directory")
	state := flag.String("state", "", "Encrypted service state directory")
	flag.Parse()
	if *mode == "rpc" {
		b, e := io.ReadAll(io.LimitReader(os.Stdin, 4<<20+4096))
		if e != nil {
			outputError("Не удалось прочитать команду")
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()
		response, e := ipc.Call(ctx, string(b))
		if e != nil {
			outputError(e.Error())
			return
		}
		fmt.Println(response)
		return
	}
	if *owner == "" || *runtimeDir == "" || *state == "" {
		outputError("Для запуска службы нужны owner, runtime и state")
		return
	}
	r, e := windowsruntime.New(*runtimeDir, *state)
	if e != nil {
		outputError("Не удалось создать среду выполнения")
		return
	}
	defer r.Close()
	r.OwnerSID = *owner
	self, _ := os.Executable()
	r.UIExecutable = filepath.Join(filepath.Dir(self), "marshrut.exe")
	m, e := app.New("windows", r, secure.Store{Path: filepath.Join(*state, "settings.dpapi"), Codec: secure.DPAPI{}})
	if e != nil {
		outputError(e.Error())
		return
	}
	defer m.Close()
	if *mode == "service" {
		if e = svc.Run("Marshrut", handler{m, *owner}); e != nil {
			outputError("Не удалось запустить службу Windows")
		}
		return
	}
	l, e := ipc.Listen(*owner)
	if e != nil {
		outputError("Не удалось открыть локальный канал управления")
		return
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	ipc.Serve(ctx, l, m)
}
func outputError(message string) {
	b, _ := json.Marshal(app.Response{Error: message})
	fmt.Println(string(b))
	os.Exit(1)
}
