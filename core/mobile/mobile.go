// Package mobile exposes only strings and primitive callbacks to gomobile/JNI.
package mobile

import (
	"encoding/base64"
	"errors"
	"marshrut/app"
	"marshrut/secure"
	"strings"
)

type Host interface {
	StartEngine(config string) error
	StopEngine() error
	EngineAlive() bool
	DpiAvailable() bool
	ConfigureDpi(domains, excluded string) error
	Seal(plain string) (string, error)
	Unseal(cipher string) (string, error)
	StartupEnabled() bool
	SetStartupEnabled(value bool) error
}
type bridge struct{ host Host }

func (b bridge) Start(c []byte) error           { return b.host.StartEngine(string(c)) }
func (b bridge) Stop() error                    { return b.host.StopEngine() }
func (b bridge) Alive() bool                    { return b.host.EngineAlive() }
func (b bridge) DPIAvailable() bool             { return b.host.DpiAvailable() }
func (b bridge) StartupSupported() bool         { return true }
func (b bridge) StartupEnabled() (bool, error)  { return b.host.StartupEnabled(), nil }
func (b bridge) SetStartupEnabled(v bool) error { return b.host.SetStartupEnabled(v) }
func (b bridge) ConfigureDPI(d, e []string) error {
	return b.host.ConfigureDpi(strings.Join(d, "\n"), strings.Join(e, "\n"))
}

type codec struct{ host Host }

func (c codec) Seal(b []byte) ([]byte, error) {
	r, e := c.host.Seal(base64.StdEncoding.EncodeToString(b))
	return []byte(r), e
}
func (c codec) Open(b []byte) ([]byte, error) {
	r, e := c.host.Unseal(string(b))
	if e != nil {
		return nil, e
	}
	return base64.StdEncoding.DecodeString(r)
}

type Client struct{ manager *app.Manager }

func NewClient(directory string, host Host) (*Client, error) {
	if host == nil {
		return nil, errors.New("Android platform unavailable")
	}
	m, e := app.New("android", bridge{host}, secure.Store{Path: directory + "/settings.keystore", Codec: codec{host}})
	if e != nil {
		return nil, e
	}
	m.StartBackground()
	return &Client{manager: m}, nil
}
func (c *Client) Request(request string) string { return c.manager.HandleJSON(request) }
func (c *Client) Close()                        { c.manager.Close() }
