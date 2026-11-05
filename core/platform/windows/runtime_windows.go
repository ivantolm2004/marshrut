package windowsruntime

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/netip"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"
	"unsafe"

	box "github.com/sagernet/sing-box"
	"github.com/sagernet/sing-box/include"
	"github.com/sagernet/sing-box/option"
	boxjson "github.com/sagernet/sing/common/json"
	"golang.org/x/sys/windows"
)

type Process struct {
	cmd  *exec.Cmd
	done chan struct{}
}
type Runtime struct {
	mu                     sync.Mutex
	Directory, State       string
	OwnerSID, UIExecutable string
	job                    windows.Handle
	engine                 *box.Box
	cancel                 context.CancelFunc
	dpi                    *Process
	domains                string
}

func New(directory, state string) (*Runtime, error) {
	r := &Runtime{Directory: directory, State: state}
	job, e := windows.CreateJobObject(nil, nil)
	if e != nil {
		return nil, e
	}
	info := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{}
	info.BasicLimitInformation.LimitFlags = windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE
	if _, e = windows.SetInformationJobObject(job, windows.JobObjectExtendedLimitInformation, uintptr(unsafe.Pointer(&info)), uint32(unsafe.Sizeof(info))); e != nil {
		windows.CloseHandle(job)
		return nil, e
	}
	r.job = job
	return r, nil
}
func (r *Runtime) verify(path string) error {
	data, e := os.ReadFile(filepath.Join(r.Directory, "manifest.json"))
	if e != nil {
		return errors.New("Нет проверенного комплекта сетевых компонентов")
	}
	var manifest map[string]string
	if json.Unmarshal(data, &manifest) != nil {
		return errors.New("Повреждён манифест компонентов")
	}
	relative, e := filepath.Rel(r.Directory, path)
	if e != nil || strings.HasPrefix(relative, "..") {
		return errors.New("Недопустимый путь компонента")
	}
	hash := manifest[filepath.ToSlash(relative)]
	if hash == "" {
		return errors.New("Компонент отсутствует в манифесте")
	}
	f, e := os.Open(path)
	if e != nil {
		return errors.New("Компонент не установлен")
	}
	defer f.Close()
	h := sha256.New()
	if _, e = io.Copy(h, f); e != nil {
		return errors.New("Не удалось проверить компонент")
	}
	if !strings.EqualFold(hex.EncodeToString(h.Sum(nil)), hash) {
		return errors.New("Контрольная сумма компонента не совпадает")
	}
	return nil
}
func (r *Runtime) spawn(path string, args []string, input []byte) (*Process, error) {
	if e := r.verify(path); e != nil {
		return nil, e
	}
	c := exec.Command(path, args...)
	c.Dir = filepath.Dir(path)
	c.Stdin = bytes.NewReader(input)
	c.Stdout = io.Discard
	c.Stderr = io.Discard
	c.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: windows.CREATE_NEW_PROCESS_GROUP}
	if e := c.Start(); e != nil {
		return nil, errors.New("Не удалось запустить сетевой компонент")
	}
	handle, e := windows.OpenProcess(windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE, false, uint32(c.Process.Pid))
	if e != nil {
		c.Process.Kill()
		c.Wait()
		return nil, errors.New("Не удалось установить контроль процесса")
	}
	e = windows.AssignProcessToJobObject(r.job, handle)
	windows.CloseHandle(handle)
	if e != nil {
		c.Process.Kill()
		c.Wait()
		return nil, errors.New("Не удалось установить контроль процесса")
	}
	p := &Process{cmd: c, done: make(chan struct{})}
	go func() { c.Wait(); close(p.done) }()
	return p, nil
}
func stop(p *Process) {
	if p == nil {
		return
	}
	p.cmd.Process.Kill()
	select {
	case <-p.done:
	case <-time.After(5 * time.Second):
	}
}
func (r *Runtime) Start(config []byte) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.engine != nil {
		return errors.New("Ядро уже запущено")
	}
	registry := include.OutboundRegistry()
	registerMarkedOutbound(registry)
	ctx, cancel := context.WithCancel(box.Context(context.Background(), include.InboundRegistry(), registry, include.EndpointRegistry(), include.DNSTransportRegistry(), include.ServiceRegistry(), include.CertificateProviderRegistry()))
	options, e := boxjson.UnmarshalExtendedContext[option.Options](ctx, config)
	if e != nil {
		cancel()
		return errors.New("Ядро отклонило конфигурацию подключения")
	}
	instance, e := box.New(box.Options{Context: ctx, Options: options})
	if e != nil {
		cancel()
		return errors.New("Не удалось создать сетевое ядро")
	}
	if e = instance.Start(); e != nil {
		cancel()
		instance.Close()
		return errors.New("Не удалось запустить ядро: проверьте права службы и сетевые компоненты")
	}
	r.engine = instance
	r.cancel = cancel
	return nil
}
func (r *Runtime) Stop() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	var e error
	if r.cancel != nil {
		r.cancel()
		r.cancel = nil
	}
	if r.engine != nil {
		e = r.engine.Close()
	}
	stop(r.dpi)
	r.engine = nil
	r.dpi = nil
	r.domains = ""
	if e != nil {
		return errors.New("Не удалось полностью завершить сетевое ядро")
	}
	return nil
}
func (r *Runtime) Close()      { r.Stop(); windows.CloseHandle(r.job) }
func (r *Runtime) Alive() bool { r.mu.Lock(); defer r.mu.Unlock(); return r.engine != nil }
func (r *Runtime) DPIAvailable() bool {
	for _, name := range []string{"zapret/winws2.exe", "zapret/cygwin1.dll", "zapret/WinDivert.dll", "zapret/WinDivert64.sys", "zapret/lua/zapret-lib.lua", "zapret/lua/zapret-antidpi.lua"} {
		if r.verify(filepath.Join(r.Directory, filepath.FromSlash(name))) != nil {
			return false
		}
	}
	return true
}
func (r *Runtime) ConfigureDPI(domains, excluded []string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	key := strings.Join(domains, "\n") + "|" + strings.Join(excluded, ",")
	if r.domains == key && r.dpi != nil {
		select {
		case <-r.dpi.done:
		default:
			return nil
		}
	}
	stop(r.dpi)
	r.dpi = nil
	r.domains = key
	if len(domains) == 0 {
		return nil
	}
	filter := "outbound and !loopback and tcp and (tcp.DstPort == 80 or tcp.DstPort == 443) and (tcp.Syn or tcp.Fin or tcp.Rst or (ip and ip.TTL == 203) or (ipv6 and ipv6.HopLimit == 203))"
	for _, ip := range excluded {
		a, e := netip.ParseAddr(ip)
		if e != nil {
			return errors.New("Не удалось исключить адрес туннеля из DPI")
		}
		if a.Is4() {
			filter += " and (not ip or ip.DstAddr != " + a.String() + ")"
		} else {
			filter += " and (not ipv6 or ipv6.DstAddr != " + a.String() + ")"
		}
	}
	// Skip private destinations as well as every resolved VLESS/WARP endpoint.
	filter += " and (not ip or ip.DstAddr < 10.0.0.0 or ip.DstAddr > 10.255.255.255) and (not ip or ip.DstAddr < 172.16.0.0 or ip.DstAddr > 172.31.255.255) and (not ip or ip.DstAddr < 192.168.0.0 or ip.DstAddr > 192.168.255.255)"
	lua := filepath.Join(r.Directory, "zapret", "lua")
	args := []string{"--wf-raw-part=" + filter, "--lua-init=@" + filepath.Join(lua, "zapret-lib.lua"), "--lua-init=@" + filepath.Join(lua, "zapret-antidpi.lua"), "--filter-tcp=80,443", "--payload=tls_client_hello,http_req", "--lua-desync=multisplit:pos=1,midsld"}
	p, e := r.spawn(filepath.Join(r.Directory, "zapret", "winws2.exe"), args, nil)
	if e != nil {
		return e
	}
	r.dpi = p
	select {
	case <-p.done:
		r.dpi = nil
		return errors.New("zapret не запустился: проверьте права администратора и драйвер WinDivert")
	case <-time.After(300 * time.Millisecond):
	}
	return nil
}
func (r *Runtime) String() string { return fmt.Sprintf("Windows runtime") }
