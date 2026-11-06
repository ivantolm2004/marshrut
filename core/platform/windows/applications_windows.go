package windowsruntime

import (
	"context"
	"errors"
	"marshrut/route"
	"os"
	"sort"
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Only query image paths and ownership. No command lines, window titles or
// process memory are read. The service exposes only its configured owner's apps.
func (r *Runtime) ListApplications(ctx context.Context) ([]route.Application, error) {
	if r.OwnerSID == "" {
		return nil, errors.New("Не указан пользователь для обнаружения приложений")
	}
	snapshot, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return nil, errors.New("Не удалось получить список приложений Windows")
	}
	defer windows.CloseHandle(snapshot)
	entry := windows.ProcessEntry32{Size: uint32(unsafe.Sizeof(windows.ProcessEntry32{}))}
	byPath := map[string]route.Application{}
	systemRoot := strings.ToLower(strings.TrimRight(os.Getenv("SystemRoot"), `\`)) + `\`
	err = windows.Process32First(snapshot, &entry)
	for err == nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		path, ok := ownedProcessPath(entry.ProcessID, r.OwnerSID)
		if ok && !strings.EqualFold(path, r.UIExecutable) && !strings.EqualFold(path, os.Args[0]) && strings.HasSuffix(strings.ToLower(path), ".exe") {
			key := strings.ToLower(path)
			if existing, ok := byPath[key]; ok {
				existing.ProcessCount++
				byPath[key] = existing
			} else {
				byPath[key] = route.NewApplication(path, strings.HasPrefix(key, systemRoot))
			}
		}
		err = windows.Process32Next(snapshot, &entry)
	}
	if !errors.Is(err, windows.ERROR_NO_MORE_FILES) {
		return nil, errors.New("Не удалось завершить чтение списка приложений")
	}
	result := make([]route.Application, 0, len(byPath))
	for _, app := range byPath {
		result = append(result, app)
	}
	sort.Slice(result, func(i, j int) bool { return strings.ToLower(result[i].Name) < strings.ToLower(result[j].Name) })
	return result, nil
}

func ownedProcessPath(pid uint32, owner string) (string, bool) {
	process, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, pid)
	if err != nil {
		return "", false
	}
	defer windows.CloseHandle(process)
	var token windows.Token
	if windows.OpenProcessToken(process, windows.TOKEN_QUERY, &token) != nil {
		return "", false
	}
	user, err := token.GetTokenUser()
	token.Close()
	if err != nil || user.User.Sid.String() != owner {
		return "", false
	}
	buffer := make([]uint16, 32768)
	size := uint32(len(buffer))
	if windows.QueryFullProcessImageName(process, 0, &buffer[0], &size) != nil {
		return "", false
	}
	return windows.UTF16ToString(buffer[:size]), true
}
