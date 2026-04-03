package mcp

import (
	"strings"

	"github.com/alexli18/claude-king/internal/registry"
)

// detectParentKingdom checks ~/.king/registry.json for a kingdom whose root
// is a parent directory of s.rootDir. If found, sets parentKingdomSocket and
// inObserverMode so the MCP session can contact the parent daemon.
func (s *Server) detectParentKingdom(registryPath string) {
	reg := registry.NewRegistry(registryPath)
	entries, err := reg.ListAlive()
	if err != nil {
		s.logger.Warn("MCP_DETECT_PARENT_ERROR", "err", err)
		return
	}

	for path, entry := range entries {
		if path == s.rootDir {
			continue // Same kingdom — not a parent
		}
		if strings.HasPrefix(s.rootDir, path+"/") {
			s.parentKingdomSocket = entry.Socket
			s.inObserverMode = true
			s.logger.Info("MCP_OBSERVER_MODE",
				"parent_kingdom", path,
				"socket", entry.Socket,
			)
			return
		}
	}
}
