package main

import (
	"context"
	"fmt"
	"sync"
	"time"

	peggplugin "github.com/peggco/pegg/pkg/plugin"
)

const (
	version       = "1.0.0"
	pluginName    = "logs"
	pluginDisplay = "Logs"
)

type Plugin struct {
	peggplugin.Base

	mu       sync.Mutex
	settings Settings
}

func (p *Plugin) Name() string { return pluginName }

func (p *Plugin) Version() string { return version }

func (p *Plugin) Description() string {
	return "Inspect Pegg system logs from ~/.pegg/logs.db with filters, search, and a local dashboard."
}

func (p *Plugin) Boot(deps peggplugin.Deps) error {
	settings := defaultSettings()
	if deps.Workdir != "" {
		settings.Workdir = deps.Workdir
	}
	if deps.PluginPath != "" {
		settings.PluginPath = deps.PluginPath
	}
	settings.applyHost(deps.ConfigJSON)
	if err := settings.applyOptions(deps.Options); err != nil {
		return fmt.Errorf("parse %s options: %w", pluginName, err)
	}
	settings.applyEnv()
	settings.normalize()

	p.mu.Lock()
	p.settings = settings
	p.mu.Unlock()
	return nil
}

func (p *Plugin) Capabilities() []string {
	return []string{
		string(peggplugin.CapabilityTools),
		string(peggplugin.CapabilitySkills),
		string(peggplugin.CapabilityPermissions),
		string(peggplugin.CapabilityCommands),
		string(peggplugin.CapabilityTUI),
	}
}

func (p *Plugin) Tools() ([]peggplugin.ToolSpec, error) {
	if !p.currentSettings().Enabled {
		return nil, nil
	}
	return toolSpecs(), nil
}

func (p *Plugin) Skills() ([]peggplugin.SkillSpec, error) {
	return []peggplugin.SkillSpec{skillSpec()}, nil
}

func (p *Plugin) PermissionRules() (map[string]string, error) {
	return permissionRules(), nil
}

func (p *Plugin) Commands() ([]peggplugin.CommandSpec, error) {
	return commandSpecs(), nil
}

func (p *Plugin) TUIPages() ([]peggplugin.TUIPage, error) {
	return []peggplugin.TUIPage{tuiPage(p.currentSettings())}, nil
}

func (p *Plugin) ExecuteTool(req peggplugin.ToolRequest) (*peggplugin.ToolResult, error) {
	settings := p.currentSettings()
	if !settings.Enabled {
		return nil, fmt.Errorf("%s is disabled", pluginName)
	}
	timeout := settings.timeout()
	if req.TimeoutMS > 0 {
		timeout = time.Duration(req.TimeoutMS) * time.Millisecond
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	client, err := newClient(settings)
	if err != nil {
		return nil, fmt.Errorf("%s", redactSettings(err.Error(), settings))
	}
	defer client.Close()

	output, err := executeTool(ctx, client, req.Name, req.Args)
	if err != nil {
		return nil, fmt.Errorf("%s", redactSettings(err.Error(), settings))
	}
	return &peggplugin.ToolResult{Output: truncateOutput(output, 0)}, nil
}

func (p *Plugin) RunCommand(req peggplugin.CommandRequest) (*peggplugin.CommandResult, error) {
	output, err := runCommand(context.Background(), p.currentSettings(), req)
	if err != nil {
		return nil, fmt.Errorf("%s", redactSettings(err.Error(), p.currentSettings()))
	}
	return &peggplugin.CommandResult{Output: output}, nil
}

func (p *Plugin) ExecuteTUIAction(req peggplugin.TUIActionRequest) (*peggplugin.TUIActionResult, error) {
	result, err := p.executeTUIAction(req)
	if err != nil {
		return nil, fmt.Errorf("%s", redactSettings(err.Error(), p.currentSettings()))
	}
	return result, nil
}

func (p *Plugin) currentSettings() Settings {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.settings
}
