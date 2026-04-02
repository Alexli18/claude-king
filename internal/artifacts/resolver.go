package artifacts

import (
	"fmt"
	"strings"
)

// URIScheme is the prefix for king artifact URIs.
const URIScheme = "king://artifacts/"

// ParseURI parses a king://artifacts/<name> URI and returns the artifact name.
func ParseURI(uri string) (name string, err error) {
	if !strings.HasPrefix(uri, URIScheme) {
		return "", fmt.Errorf("invalid artifact URI scheme: %s (expected prefix %s)", uri, URIScheme)
	}
	name = strings.TrimPrefix(uri, URIScheme)
	if name == "" {
		return "", fmt.Errorf("empty artifact name in URI: %s", uri)
	}
	return name, nil
}

// BuildURI builds a king://artifacts/<name> URI.
func BuildURI(name string) string {
	return URIScheme + name
}

// ResolveURI resolves a king:// URI to a file path using the ledger.
func (l *Ledger) ResolveURI(uri string) (filePath string, err error) {
	name, err := ParseURI(uri)
	if err != nil {
		return "", err
	}
	a, err := l.Resolve(name)
	if err != nil {
		return "", err
	}
	return a.FilePath, nil
}
