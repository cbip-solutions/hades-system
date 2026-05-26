// SPDX-License-Identifier: MIT
package source

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

var (
	ErrSourceMissing      = errors.New("migrate/source: source path does not exist")
	ErrSymlinkOutsideRoot = errors.New("migrate/source: symlink points outside source root")
	ErrMalformedSettings  = errors.New("migrate/source: settings.json is malformed")
	ErrMalformedMCP       = errors.New("migrate/source: .mcp.json is malformed")
	ErrPermissionDenied   = errors.New("migrate/source: permission denied reading path")
)

type Inventory struct {
	Skills      []SkillSource
	Commands    []CommandSource
	Hooks       []HookSource
	Settings    *SettingsSource
	MemoryFiles []MemorySource
	MCPServers  *MCPSource
	Warnings    []string
}

type SkillSource struct {
	Name string
	Path string
	Body []byte
}

type CommandSource struct {
	Name string
	Path string
	Body []byte
}

type HookSource struct {
	EventName string
	Path      string
	Lang      string
	Body      []byte
}

type SettingsSource struct {
	Path        string                           `json:"-"`
	Permissions PermissionsSource                `json:"permissions"`
	Env         map[string]string                `json:"env,omitempty"`
	Model       string                           `json:"model,omitempty"`
	Hooks       map[string][]SettingsHookMatcher `json:"hooks,omitempty"`
	MCPServers  map[string]MCPServer             `json:"mcpServers,omitempty"`
	Raw         map[string]interface{}           `json:"-"`
}

type SettingsHookMatcher struct {
	Matcher string              `json:"matcher"`
	Hooks   []SettingsHookEntry `json:"hooks"`
}

type SettingsHookEntry struct {
	Type    string `json:"type"`
	Command string `json:"command"`
}

type PermissionsSource struct {
	Allow []string `json:"allow,omitempty"`
	Deny  []string `json:"deny,omitempty"`
}

type MemorySource struct {
	ProjectSlug string
	Path        string
	Body        []byte
}

type MCPSource struct {
	Path       string               `json:"-"`
	MCPServers map[string]MCPServer `json:"mcpServers,omitempty"`
}

type MCPServer struct {
	Command string            `json:"command"`
	Args    []string          `json:"args,omitempty"`
	Env     map[string]string `json:"env,omitempty"`
}

func ReadAll(sourceRoot string) (*Inventory, error) {
	if _, err := os.Stat(sourceRoot); err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("%w: %s", ErrSourceMissing, sourceRoot)
		}
		return nil, fmt.Errorf("stat source root %q: %w", sourceRoot, err)
	}
	absRoot, err := filepath.Abs(sourceRoot)
	if err != nil {
		return nil, fmt.Errorf("abs source root %q: %w", sourceRoot, err)
	}
	if err := assertNoSymlinkEscape(absRoot); err != nil {
		return nil, err
	}
	inv := &Inventory{}
	if err := walkSkills(absRoot, inv); err != nil {
		return nil, err
	}
	if err := walkCommands(absRoot, inv); err != nil {
		return nil, err
	}
	if err := walkHooks(absRoot, inv); err != nil {
		return nil, err
	}
	if err := readSettings(absRoot, inv); err != nil {
		return nil, err
	}
	if err := walkMemory(absRoot, inv); err != nil {
		return nil, err
	}
	if err := readMCP(absRoot, inv); err != nil {
		return nil, err
	}
	return inv, nil
}
