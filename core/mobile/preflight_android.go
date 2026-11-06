package mobile

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"marshrut/profile"

	"github.com/sagernet/sing-box/experimental/libbox"
)

// Validate providers' outbounds before combining them into a single VPN.
// One invalid node must not prevent the other servers from starting.
func (b bridge) PrepareServers(ctx context.Context, servers []profile.Server) ([]profile.Server, []string) {
	accepted := make([]profile.Server, 0, len(servers))
	var warnings []string
	for i, server := range servers {
		if ctx.Err() != nil {
			break
		}
		outbound := maps.Clone(server.Outbound)
		if server.Xray == nil && outbound != nil {
			outbound["tag"] = "validate"
			config, err := json.Marshal(map[string]any{
				"log":       map[string]bool{"disabled": true},
				"outbounds": []map[string]any{outbound},
			})
			if err == nil && libbox.CheckConfig(string(config)) == nil {
				accepted = append(accepted, server)
				continue
			}
		}
		// The native validation error may contain the server's configuration.
		warnings = append(warnings, fmt.Sprintf("Сервер %d пропущен: конфигурация не поддерживается ядром Android", i+1))
	}
	return accepted, warnings
}
