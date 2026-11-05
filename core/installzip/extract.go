// Package installzip extracts only the application's embedded, reviewed payload.
package installzip

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
)

func SafePath(root, name string) (string, error) {
	root, e := filepath.Abs(root)
	if e != nil {
		return "", e
	}
	if strings.Contains(name, ":") || strings.Contains(name, "\\") || strings.HasPrefix(name, "/") {
		return "", errors.New("Недопустимый путь в пакете")
	}
	path := filepath.Join(root, filepath.FromSlash(name))
	relative, e := filepath.Rel(root, path)
	if e != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(os.PathSeparator)) || filepath.IsAbs(relative) {
		return "", errors.New("Файл выходит за каталог приложения")
	}
	return path, nil
}
func Entries(data []byte) ([]string, error) {
	z, e := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if e != nil {
		return nil, errors.New("Повреждён установочный пакет")
	}
	var names []string
	var total uint64
	for _, f := range z.File {
		if f.Mode()&os.ModeSymlink != 0 {
			return nil, errors.New("Ссылки в установочном пакете не допускаются")
		}
		if _, e := SafePath(os.TempDir(), f.Name); e != nil {
			return nil, e
		}
		if f.FileInfo().IsDir() {
			continue
		}
		total += f.UncompressedSize64
		if total > 1<<30 {
			return nil, errors.New("Установочный пакет слишком большой")
		}
		names = append(names, f.Name)
	}
	return names, nil
}
func Extract(data []byte, destination string) error {
	if _, e := Entries(data); e != nil {
		return e
	}
	z, _ := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	for _, f := range z.File {
		target, e := SafePath(destination, f.Name)
		if e != nil {
			return e
		}
		if f.FileInfo().IsDir() {
			if e = os.MkdirAll(target, 0755); e != nil {
				return e
			}
			continue
		}
		if e = os.MkdirAll(filepath.Dir(target), 0755); e != nil {
			return e
		}
		// Never follow a pre-existing reparse/symlink final file.
		if st, e := os.Lstat(target); e == nil && st.Mode()&os.ModeSymlink != 0 {
			return errors.New("Каталог приложения содержит ссылку")
		}
		r, e := f.Open()
		if e != nil {
			return e
		}
		// An unchanged driver/DLL may still be mapped after stopping the
		// service. Compare contents and leave identical installed files alone.
		if existing, openErr := os.Open(target); openErr == nil {
			oldHash, newHash := sha256.New(), sha256.New()
			oldSize, oldErr := io.Copy(oldHash, existing)
			existing.Close()
			newSize, newErr := io.Copy(newHash, io.LimitReader(r, int64(f.UncompressedSize64)+1))
			r.Close()
			if newErr != nil || uint64(newSize) != f.UncompressedSize64 {
				return errors.New("Проверка файла установочного пакета не пройдена")
			}
			if oldErr == nil && oldSize == newSize && bytes.Equal(oldHash.Sum(nil), newHash.Sum(nil)) {
				continue
			}
			r, e = f.Open()
			if e != nil {
				return e
			}
		}
		out, e := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0644)
		if e != nil {
			r.Close()
			return e
		}
		n, copyErr := io.Copy(out, io.LimitReader(r, int64(f.UncompressedSize64)+1))
		closeErr := out.Close()
		r.Close()
		if copyErr != nil || closeErr != nil || uint64(n) != f.UncompressedSize64 {
			return errors.New("Проверка файла установочного пакета не пройдена")
		}
	}
	return nil
}
