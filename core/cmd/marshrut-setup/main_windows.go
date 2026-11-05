//go:build installer

// The self-contained installer uses only the Windows APIs and the embedded
// payload. Building it requires no installer installation or elevation.
package main

import (
	_ "embed"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"marshrut/installzip"
	"marshrut/startupwindows"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"
	"unsafe"

	"github.com/go-ole/go-ole"
	"github.com/go-ole/go-ole/oleutil"
	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
	"golang.org/x/sys/windows/svc"
	"golang.org/x/sys/windows/svc/mgr"
)

//go:embed payload.zip
var payload []byte

const uninstallKey = `Software\Microsoft\Windows\CurrentVersion\Uninstall\Marshrut`

type Record struct {
	Files   []string `json:"files"`
	Owner   string   `json:"owner"`
	Version string   `json:"version"`
}

func message(text string) {
	windows.NewLazySystemDLL("user32.dll").NewProc("MessageBoxW").Call(0, uintptr(unsafe.Pointer(windows.StringToUTF16Ptr(text))), uintptr(unsafe.Pointer(windows.StringToUTF16Ptr("Маршрут"))), 0x40)
}
func main() {
	remove := flag.Bool("uninstall", false, "uninstall personal application")
	elevated := flag.Bool("elevated", false, "internal elevation marker")
	owner := flag.String("owner", "", "controlling Windows SID")
	quiet := flag.Bool("quiet", false, "write install-result.json instead of completion dialog")
	flag.Parse()
	token := windows.GetCurrentProcessToken()
	if *owner == "" {
		user, e := token.GetTokenUser()
		if e != nil {
			message("Не удалось определить владельца приложения")
			os.Exit(1)
		}
		*owner = user.User.Sid.String()
	}
	if _, e := windows.StringToSid(*owner); e != nil {
		message("Некорректный владелец приложения")
		os.Exit(1)
	}
	if !token.IsElevated() {
		if *elevated {
			message("Для установки службы нужны права администратора")
			os.Exit(1)
		}
		exe, _ := os.Executable()
		args := "--elevated --owner=" + *owner
		if *quiet {
			args += " --quiet"
		}
		if *remove {
			args += " --uninstall"
		}
		result, _, _ := windows.NewLazySystemDLL("shell32.dll").NewProc("ShellExecuteW").Call(0, uintptr(unsafe.Pointer(windows.StringToUTF16Ptr("runas"))), uintptr(unsafe.Pointer(windows.StringToUTF16Ptr(exe))), uintptr(unsafe.Pointer(windows.StringToUTF16Ptr(args))), 0, 0)
		if result <= 32 {
			message("Установка не выполнена: повышение прав отменено или недоступно")
			os.Exit(1)
		}
		return
	}
	root := filepath.Join(os.Getenv("ProgramFiles"), "Marshrut")
	if !filepath.IsAbs(root) {
		message("Не удалось определить каталог Program Files")
		os.Exit(1)
	}
	var e error
	if *remove {
		e = uninstall(root)
	} else {
		e = install(root, *owner)
	}
	if !*remove {
		result := map[string]any{"version": "0.1.3", "ok": e == nil, "time": time.Now().UTC()}
		if e != nil {
			result["error"] = e.Error()
		}
		data, _ := json.MarshalIndent(result, "", "  ")
		os.WriteFile(filepath.Join(root, "install-result.json"), data, 0644)
	}
	if e != nil {
		if !*quiet {
			message(e.Error())
		}
		os.Exit(1)
	}
	if *quiet {
		return
	}
	if *remove {
		message("Маршрут удалён. Занятые файлы будут удалены после перезагрузки. Зашифрованные личные настройки сохранены.")
	} else {
		message("Маршрут установлен. Откройте приложение из меню «Пуск» и добавьте свою подписку.")
	}
}
func stopService(service *mgr.Service) error {
	status, e := service.Query()
	if e != nil {
		return e
	}
	if status.State == svc.Stopped {
		return nil
	}
	if _, e = service.Control(svc.Stop); e != nil {
		return e
	}
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		status, e = service.Query()
		if e != nil {
			return e
		}
		if status.State == svc.Stopped {
			return nil
		}
		time.Sleep(200 * time.Millisecond)
	}
	return errors.New("Не удалось остановить службу. Закройте Маршрут и повторите установку")
}
func copyFile(from, to string) error {
	f, e := os.Open(from)
	if e != nil {
		return e
	}
	defer f.Close()
	if e = os.MkdirAll(filepath.Dir(to), 0755); e != nil {
		return e
	}
	out, e := os.OpenFile(to, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0644)
	if e != nil {
		return e
	}
	_, e = io.Copy(out, f)
	closeErr := out.Close()
	if e != nil {
		return e
	}
	return closeErr
}

func serviceArgs(owner, runtimeDir, state string) []string {
	return []string{"-mode", "service", "-owner", owner, "-runtime", runtimeDir, "-state", state}
}

func serviceConfig(exe string, args []string) mgr.Config {
	return mgr.Config{
		ServiceType:    windows.SERVICE_WIN32_OWN_PROCESS,
		DisplayName:    "Маршрут — сетевое ядро",
		StartType:      mgr.StartAutomatic,
		ErrorControl:   mgr.ErrorNormal,
		BinaryPathName: windows.ComposeCommandLine(append([]string{exe}, args...)),
		Description:    "Личный клиент VLESS, zapret и WARP",
	}
}

func serviceError(action string, cause error) error {
	var code syscall.Errno
	if errors.As(cause, &code) {
		return fmt.Errorf("%s (код Windows %d): %w", action, uint32(code), cause)
	}
	return fmt.Errorf("%s: %w", action, cause)
}

func install(root, owner string) error {
	files, e := installzip.Entries(payload)
	if e != nil {
		return e
	}
	connection, e := mgr.Connect()
	if e != nil {
		return serviceError("Диспетчер служб Windows недоступен", e)
	}
	defer connection.Disconnect()
	var service *mgr.Service
	var oldConfig *mgr.Config
	created := false
	service, e = connection.OpenService("Marshrut")
	if e != nil && !errors.Is(e, windows.ERROR_SERVICE_DOES_NOT_EXIST) {
		return serviceError("Не удалось открыть службу Маршрут", e)
	}
	if e == nil {
		defer service.Close()
		config, e := service.Config()
		if e != nil {
			return errors.New("Не удалось прочитать настройки службы")
		}
		expected := "\"" + filepath.Join(root, "marshrut-core.exe") + "\""
		if !strings.HasPrefix(strings.ToLower(config.BinaryPathName), strings.ToLower(expected)) {
			return errors.New("Имя службы Маршрут уже занято другим приложением")
		}
		oldConfig = &config
		if e = stopService(service); e != nil {
			return e
		}
	}
	if e = os.MkdirAll(root, 0755); e != nil {
		return errors.New("Не удалось создать каталог приложения")
	}
	backup := filepath.Join(root, "previous")
	backed := map[string]bool{}
	for _, name := range files {
		target, e := installzip.SafePath(root, name)
		if e != nil {
			return e
		}
		if _, e = os.Stat(target); e == nil {
			previous, e := installzip.SafePath(backup, name)
			if e != nil {
				return e
			}
			if e = copyFile(target, previous); e != nil {
				return errors.New("Не удалось сохранить предыдущую версию")
			}
			backed[name] = true
		}
	}
	rollback := func() {
		if service != nil {
			stopService(service)
			if created {
				service.Delete()
			}
		}
		for _, name := range files {
			target, _ := installzip.SafePath(root, name)
			if backed[name] {
				previous, _ := installzip.SafePath(backup, name)
				copyFile(previous, target)
			} else {
				os.Remove(target)
			}
		}
		if oldConfig != nil && service != nil {
			service.UpdateConfig(*oldConfig)
			service.Start()
		}
	}
	if e = installzip.Extract(payload, root); e != nil {
		rollback()
		return fmt.Errorf("Не удалось записать файлы: %w. Закройте Маршрут и повторите установку", e)
	}
	state := filepath.Join(os.Getenv("ProgramData"), "Marshrut")
	if e = os.MkdirAll(state, 0700); e != nil {
		rollback()
		return errors.New("Не удалось создать защищённое хранилище")
	}
	sd, e := windows.SecurityDescriptorFromString("D:P(A;OICI;FA;;;SY)(A;OICI;FA;;;BA)")
	if e != nil {
		rollback()
		return e
	}
	dacl, _, e := sd.DACL()
	if e != nil {
		rollback()
		return e
	}
	if e = windows.SetNamedSecurityInfo(state, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION, nil, nil, dacl, nil); e != nil {
		rollback()
		return errors.New("Не удалось защитить личные настройки")
	}
	exe := filepath.Join(root, "marshrut-core.exe")
	args := serviceArgs(owner, filepath.Join(root, "runtime"), state)
	config := serviceConfig(exe, args)
	if service == nil {
		// CreateService composes its own command line from exe and args.
		// Its default ServiceType is applied to a copy, so it must also be
		// explicit in config when updating an existing service.
		service, e = connection.CreateService("Marshrut", exe, config, args...)
		if e != nil {
			rollback()
			return serviceError("Не удалось установить службу Windows", e)
		}
		defer service.Close()
		created = true
	} else if e = service.UpdateConfig(config); e != nil {
		rollback()
		return serviceError("Не удалось обновить службу", e)
	}
	service.SetRecoveryActions([]mgr.RecoveryAction{{Type: mgr.ServiceRestart, Delay: 5 * time.Second}, {Type: mgr.ServiceRestart, Delay: 15 * time.Second}}, 86400)
	if e = service.Start(); e != nil {
		rollback()
		return serviceError("Служба не запустилась; установка отменена", e)
	}
	deadline := time.Now().Add(30 * time.Second)
	for {
		status, queryErr := service.Query()
		if queryErr == nil && status.State == svc.Running {
			break
		}
		if queryErr != nil || status.State == svc.Stopped || time.Now().After(deadline) {
			rollback()
			if queryErr != nil {
				return serviceError("Не удалось проверить запуск службы", queryErr)
			}
			return fmt.Errorf("Служба не подтвердила запуск (состояние %d, код завершения %d); установка отменена", status.State, status.Win32ExitCode)
		}
		time.Sleep(200 * time.Millisecond)
	}
	setup, _ := os.Executable()
	if e = copyFile(setup, filepath.Join(root, "uninstall.exe")); e != nil {
		return errors.New("Не удалось сохранить программу удаления")
	}
	record := Record{Files: append(files, "uninstall.exe", "install-manifest.json"), Owner: owner, Version: "0.1.3"}
	data, _ := json.MarshalIndent(record, "", "  ")
	if e = os.WriteFile(filepath.Join(root, "install-manifest.json"), data, 0644); e != nil {
		return e
	}
	key, _, e := registry.CreateKey(registry.LOCAL_MACHINE, uninstallKey, registry.SET_VALUE)
	if e == nil {
		defer key.Close()
		key.SetStringValue("DisplayName", "Маршрут")
		key.SetStringValue("DisplayVersion", "0.1.3")
		key.SetStringValue("InstallLocation", root)
		key.SetStringValue("DisplayIcon", filepath.Join(root, "marshrut.exe"))
		key.SetStringValue("UninstallString", "\""+filepath.Join(root, "uninstall.exe")+"\" --uninstall")
		key.SetDWordValue("NoModify", 1)
		key.SetDWordValue("NoRepair", 1)
	}
	shortcut(root)
	autoStart, e := startupwindows.Preferred(owner)
	if e != nil {
		return serviceError("Не удалось прочитать настройку автозапуска", e)
	}
	if e = startupwindows.Set(owner, filepath.Join(root, "marshrut.exe"), autoStart); e != nil {
		return serviceError("Не удалось настроить автозапуск", e)
	}
	return nil
}
func shortcut(root string) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	if e := ole.CoInitialize(0); e != nil {
		return
	}
	defer ole.CoUninitialize()
	object, e := oleutil.CreateObject("WScript.Shell")
	if e != nil {
		return
	}
	defer object.Release()
	shell, e := object.QueryInterface(ole.IID_IDispatch)
	if e != nil {
		return
	}
	defer shell.Release()
	path := filepath.Join(os.Getenv("ProgramData"), "Microsoft", "Windows", "Start Menu", "Programs", "Маршрут.lnk")
	variant, e := oleutil.CallMethod(shell, "CreateShortcut", path)
	if e != nil {
		return
	}
	defer variant.Clear()
	link := variant.ToIDispatch()
	oleutil.PutProperty(link, "TargetPath", filepath.Join(root, "marshrut.exe"))
	oleutil.PutProperty(link, "WorkingDirectory", root)
	oleutil.PutProperty(link, "IconLocation", filepath.Join(root, "marshrut.exe"))
	oleutil.CallMethod(link, "Save")
}
func uninstall(root string) error {
	data, e := os.ReadFile(filepath.Join(root, "install-manifest.json"))
	if e != nil {
		return errors.New("Не найден манифест установленного приложения")
	}
	var record Record
	if json.Unmarshal(data, &record) != nil {
		return errors.New("Манифест установки повреждён")
	}
	connection, e := mgr.Connect()
	if e != nil {
		return e
	}
	defer connection.Disconnect()
	if service, e := connection.OpenService("Marshrut"); e == nil {
		defer service.Close()
		if e = stopService(service); e != nil {
			return e
		}
		if e = service.Delete(); e != nil {
			return e
		}
	}
	if e = startupwindows.Remove(record.Owner); e != nil {
		return serviceError("Не удалось удалить автозапуск", e)
	}
	for _, name := range record.Files {
		path, e := installzip.SafePath(root, name)
		if e != nil {
			return e
		}
		if e = os.Remove(path); e != nil && !os.IsNotExist(e) {
			pointer, _ := windows.UTF16PtrFromString(path)
			windows.MoveFileEx(pointer, nil, windows.MOVEFILE_DELAY_UNTIL_REBOOT)
		}
	}
	os.Remove(filepath.Join(os.Getenv("ProgramData"), "Microsoft", "Windows", "Start Menu", "Programs", "Маршрут.lnk"))
	registry.DeleteKey(registry.LOCAL_MACHINE, uninstallKey)
	return nil
}
