package windowsruntime

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"marshrut/engine"
	"marshrut/profile"
	"net"
	"path/filepath"
	"sync"
	"time"

	box "github.com/sagernet/sing-box"
	"github.com/sagernet/sing-box/include"
	"github.com/sagernet/sing-box/option"
	boxjson "github.com/sagernet/sing/common/json"
)

// Preflight each node independently so one invalid provider entry cannot reject
// the combined configuration. Cached profiles are preserved for future updates.
func (r *Runtime) PrepareServers(ctx context.Context, servers []profile.Server) ([]profile.Server, []string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	issues := make([]string, len(servers))
	var workers sync.WaitGroup
	limit := make(chan struct{}, 4)
	for i, server := range servers {
		workers.Add(1)
		go func(i int, server profile.Server) {
			defer workers.Done()
			select {
			case limit <- struct{}{}:
			case <-ctx.Done():
				issues[i] = "Проверка отменена"
				return
			}
			defer func() { <-limit }()
			plan, err := engine.Build(engine.Options{Platform: "windows", Servers: []profile.Server{server}, APISecret: "preflight", ProbeUser: "preflight", ProbePassword: "preflight-not-started"})
			if err != nil {
				issues[i] = "Не удалось подготовить параметры подключения"
				return
			}
			if server.Xray != nil {
				binary := filepath.Join(r.Directory, "xray", "xray.exe")
				process, err := r.spawn(binary, []string{"run", "-test", "-format", "json", "-config", "stdin:"}, plan.XrayConfig)
				if err != nil {
					issues[i] = "Недоступен проверенный компонент Xray; установите новый комплект Windows"
					return
				}
				select {
				case <-process.done:
					if process.cmd.ProcessState.ExitCode() != 0 {
						issues[i] = "Xray не поддерживает параметры этого сервера"
					}
				case <-ctx.Done():
					stop(process)
					issues[i] = "Проверка отменена"
				case <-time.After(10 * time.Second):
					stop(process)
					issues[i] = "Xray не завершил проверку параметров"
				}
				return
			}
			registry := include.OutboundRegistry()
			registerMarkedOutbound(registry)
			boxContext := box.Context(ctx, include.InboundRegistry(), registry, include.EndpointRegistry(), include.DNSTransportRegistry(), include.ServiceRegistry(), include.CertificateProviderRegistry())
			options, err := boxjson.UnmarshalExtendedContext[option.Options](boxContext, plan.Config)
			if err != nil {
				issues[i] = "sing-box не поддерживает параметры этого сервера"
				return
			}
			instance, err := box.New(box.Options{Context: boxContext, Options: options})
			if err != nil {
				issues[i] = "sing-box не поддерживает параметры этого сервера"
				return
			}
			instance.Close()
		}(i, server)
	}
	workers.Wait()
	var accepted []profile.Server
	var warnings []string
	for i, server := range servers {
		if issues[i] == "" {
			accepted = append(accepted, server)
		} else {
			warnings = append(warnings, fmt.Sprintf("Сервер %d: %s", i+1, issues[i]))
		}
	}
	return accepted, warnings
}

// StartPlan starts the optional authenticated Xray bridge before installing TUN
// routes. Credentials travel over stdin and never appear in a process command.
func (r *Runtime) StartPlan(plan engine.Plan) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.engine != nil || r.xray != nil {
		return errors.New("Ядро уже запущено")
	}
	if len(plan.XrayConfig) == 0 {
		return r.startLocked(plan.Config)
	}
	config := plan.XrayConfig
	var err error
	if plan.TUN {
		iface, interfaceError := outboundInterface()
		if interfaceError != nil {
			return interfaceError
		}
		config, err = bindXrayConfig(config, iface)
		if err != nil {
			return err
		}
	}
	binary := filepath.Join(r.Directory, "xray", "xray.exe")
	if err = r.verify(binary); err != nil {
		return errors.New("Для этого сервера нужен комплект Windows с Xray; переустановите новую версию Маршрута")
	}
	check, err := r.spawn(binary, []string{"run", "-test", "-format", "json", "-config", "stdin:"}, config)
	if err != nil {
		return err
	}
	select {
	case <-check.done:
		if check.cmd.ProcessState.ExitCode() != 0 {
			return errors.New("Xray отклонил параметры подписки; попробуйте другой сервер или скопируйте отчёт диагностики")
		}
	case <-time.After(10 * time.Second):
		stop(check)
		return errors.New("Xray не завершил проверку конфигурации")
	}
	r.xray, err = r.spawn(binary, []string{"run", "-format", "json", "-config", "stdin:"}, config)
	if err != nil {
		return err
	}
	ok := false
	defer func() {
		if !ok {
			stop(r.xray)
			r.xray = nil
		}
	}()
	if err = waitXray(r.xray, plan.XrayPorts, config); err != nil {
		return err
	}
	if err = r.startLocked(plan.Config); err != nil {
		return err
	}
	ok = true
	return nil
}

// A UDP connect selects an OS route without sending a datagram. This is done
// before TUN starts, so Xray can explicitly bind its sockets to that interface.
func outboundInterface() (string, error) {
	c, err := net.DialTimeout("udp4", "1.1.1.1:53", time.Second)
	if err != nil {
		return "", errors.New("Не найден сетевой адаптер для подключения Xray")
	}
	ip := c.LocalAddr().(*net.UDPAddr).IP
	c.Close()
	interfaces, err := net.Interfaces()
	if err == nil {
		for _, iface := range interfaces {
			if iface.Name == "Marshrut" || iface.Flags&net.FlagUp == 0 {
				continue
			}
			addresses, _ := iface.Addrs()
			for _, address := range addresses {
				local, _, parseErr := net.ParseCIDR(address.String())
				if parseErr == nil && local.Equal(ip) {
					return iface.Name, nil
				}
			}
		}
	}
	return "", errors.New("Не найден исходящий сетевой адаптер; отключите прежнее VPN-соединение и повторите")
}

func bindXrayConfig(data []byte, iface string) ([]byte, error) {
	var config map[string]any
	if json.Unmarshal(data, &config) != nil {
		return nil, errors.New("Некорректная конфигурация Xray")
	}
	outbounds, _ := config["outbounds"].([]any)
	// Resolve provider endpoints before hijacking system DNS. Failed lookups
	// remain hostnames, so a single unavailable node does not stop other nodes.
	type lookup struct {
		server  map[string]any
		address string
	}
	var jobs []lookup
	for _, value := range outbounds {
		o := value.(map[string]any)
		stream := o["streamSettings"].(map[string]any)
		stream["sockopt"] = map[string]any{"interface": iface}
		servers := o["settings"].(map[string]any)["vnext"].([]any)
		for _, value := range servers {
			server := value.(map[string]any)
			address, _ := server["address"].(string)
			if xhttp, ok := stream["xhttpSettings"].(map[string]any); ok && xhttp["host"] == "" {
				xhttp["host"] = address
			}
			if net.ParseIP(address) == nil {
				jobs = append(jobs, lookup{server, address})
			}
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	// Bound the whole subscription, including slow DNS, to one startup deadline.
	results := make(chan lookup, len(jobs))
	limit := make(chan struct{}, 8)
	for _, job := range jobs {
		go func(job lookup) {
			select {
			case limit <- struct{}{}:
			case <-ctx.Done():
				results <- job
				return
			}
			ips, err := net.DefaultResolver.LookupIP(ctx, "ip4", job.address)
			<-limit
			if err == nil && len(ips) > 0 {
				job.address = ips[0].String()
			}
			results <- job
		}(job)
	}
	for range jobs {
		job := <-results
		job.server["address"] = job.address
	}
	return json.Marshal(config)
}

func waitXray(process *Process, ports []int, config []byte) error {
	var parsed struct {
		Inbounds []struct {
			Settings struct{ Accounts []struct{ User, Pass string } }
		}
	}
	if json.Unmarshal(config, &parsed) != nil || len(parsed.Inbounds) != len(ports) {
		return errors.New("Некорректный локальный мост Xray")
	}
	deadline := time.Now().Add(5 * time.Second)
	for i, port := range ports {
		accounts := parsed.Inbounds[i].Settings.Accounts
		if len(accounts) != 1 {
			return errors.New("Не заданы ключи локального моста Xray")
		}
		for {
			select {
			case <-process.done:
				return fmt.Errorf("Xray завершился при открытии локального моста: %w", engine.ErrLocalPortBusy)
			default:
			}
			if authenticateSOCKS(port, accounts[0].User, accounts[0].Pass) == nil {
				break
			}
			if time.Now().After(deadline) {
				return errors.New("Локальный мост Xray не ответил")
			}
			time.Sleep(30 * time.Millisecond)
		}
	}
	return nil
}

func authenticateSOCKS(port int, user, password string) error {
	c, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", port), 100*time.Millisecond)
	if err != nil {
		return err
	}
	defer c.Close()
	c.SetDeadline(time.Now().Add(200 * time.Millisecond))
	if _, err = c.Write([]byte{5, 1, 2}); err != nil {
		return err
	}
	var response [2]byte
	if _, err = io.ReadFull(c, response[:]); err != nil {
		return err
	}
	if response != [2]byte{5, 2} {
		return errors.New("SOCKS authentication unavailable")
	}
	request := append([]byte{1, byte(len(user))}, []byte(user)...)
	request = append(request, byte(len(password)))
	request = append(request, []byte(password)...)
	if _, err = c.Write(request); err != nil {
		return err
	}
	if _, err = io.ReadFull(c, response[:]); err != nil {
		return err
	}
	if response != [2]byte{1, 0} {
		return errors.New("SOCKS authentication rejected")
	}
	return nil
}
