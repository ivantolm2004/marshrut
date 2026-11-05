package secure

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
)

type Codec interface {
	Seal([]byte) ([]byte, error)
	Open([]byte) ([]byte, error)
}
type Store struct {
	Path  string
	Codec Codec
}

func (s Store) Save(v any) error {
	if s.Codec == nil {
		return errors.New("Защищённое хранилище недоступно")
	}
	plain, e := json.Marshal(v)
	if e != nil {
		return e
	}
	defer clear(plain)
	data, e := s.Codec.Seal(plain)
	if e != nil {
		return errors.New("Не удалось зашифровать настройки")
	}
	if e = os.MkdirAll(filepath.Dir(s.Path), 0700); e != nil {
		return e
	}
	tmp := s.Path + ".new"
	if e = os.WriteFile(tmp, data, 0600); e != nil {
		return e
	}
	if e = replaceFile(tmp, s.Path); e != nil {
		os.Remove(tmp)
		return errors.New("Не удалось сохранить настройки")
	}
	return nil
}
func (s Store) Load(v any) error {
	data, e := os.ReadFile(s.Path)
	if e != nil {
		return e
	}
	if s.Codec == nil {
		return errors.New("Защищённое хранилище недоступно")
	}
	plain, e := s.Codec.Open(data)
	if e != nil {
		return errors.New("Не удалось расшифровать настройки")
	}
	defer clear(plain)
	if json.Unmarshal(plain, v) != nil {
		return errors.New("Настройки повреждены")
	}
	return nil
}
