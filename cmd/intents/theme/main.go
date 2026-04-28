package main

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/brightpuddle/clara/pkg/contract"
	"github.com/hashicorp/go-plugin"
	"gopkg.in/yaml.v3"
)

type ThemeConfig struct {
	Mode   string                 `yaml:"mode"`
	Themes map[string]map[string]any `yaml:"themes"` // appearance -> key -> value
}

type ThemeIntent struct{}

func (i *ThemeIntent) Execute(name string, args []byte, ctx contract.Context) error {
	switch name {
	case "on_system_change":
		return i.handleSystemChange(args, ctx)
	case "on_theme_change", "main", "execute":
		return i.handleMain(ctx)
	default:
		return i.handleMain(ctx)
	}
}

func (i *ThemeIntent) handleMain(ctx contract.Context) error {
	config, err := i.loadConfig(ctx)
	if err != nil {
		return err
	}

	var appearance string
	if config.Mode == "system" {
		macos, err := ctx.MacOS()
		if err != nil {
			return err
		}
		appearance, err = macos.GetTheme()
		if err != nil {
			return err
		}
	} else {
		appearance = config.Mode
	}

	return i.applyTheme(appearance, config.Themes, ctx)
}

func (i *ThemeIntent) handleSystemChange(args []byte, ctx contract.Context) error {
	var event struct {
		Theme string `json:"theme"`
	}
	if err := json.Unmarshal(args, &event); err != nil {
		return err
	}

	config, err := i.loadConfig(ctx)
	if err != nil {
		return err
	}

	if config.Mode == "system" {
		return i.applyTheme(event.Theme, config.Themes, ctx)
	}
	return nil
}

func (i *ThemeIntent) loadConfig(ctx contract.Context) (*ThemeConfig, error) {
	fs, err := ctx.FS()
	if err != nil {
		return nil, err
	}

	data, err := fs.ReadFile("~/.config/clara/theme.yaml")
	if err != nil {
		return nil, err
	}

	var config ThemeConfig
	if err := yaml.Unmarshal(data, &config); err != nil {
		return nil, err
	}
	return &config, nil
}

func (i *ThemeIntent) applyTheme(appearance string, themes map[string]map[string]any, ctx contract.Context) error {
	fmt.Printf("Applying theme for appearance: %s\n", appearance)
	
	shell, err := ctx.Shell()
	if err != nil {
		return err
	}

	i.ensureEzaThemes(shell)
	i.ensureYaziFlavors(shell)

	theme := themes[appearance]
	lightTheme := themes["light"]
	darkTheme := themes["dark"]

	fs, err := ctx.FS()
	if err != nil {
		return err
	}

	i.applyWezterm(appearance, i.getString(theme, "wezterm", "nord"), fs)
	i.applyAider(appearance, shell)
	i.applyBtop(appearance, i.getString(theme, "btop", "nord"), shell)
	i.applyGhostty(appearance, i.getString(lightTheme, "ghostty", "Zenwritten Light"), i.getString(darkTheme, "ghostty", "Nord"), fs)
	i.applyNeovim(appearance, i.getString(theme, "nvim", "zenlocal"), fs, shell)
	i.applyTmux(appearance, theme, fs, shell)
	i.applyZsh(appearance, theme, shell, fs)
	i.applyYazi(appearance, theme, shell)
	i.applyGemini(appearance, i.getString(theme, "gemini", "Default Dark"), shell)

	return nil
}

func (i *ThemeIntent) getString(theme map[string]any, key string, defaultVal string) string {
	if val, ok := theme[key]; ok {
		if s, ok := val.(string); ok {
			return s
		}
	}
	return defaultVal
}

func (i *ThemeIntent) ensureEzaThemes(shell contract.ShellIntegration) {
	repoPath := "~/.config/eza/themes-repo"
	exists, _ := shell.Run(fmt.Sprintf("[ -d %s ] && echo true || echo false", repoPath))
	if strings.TrimSpace(exists) != "true" {
		shell.Run(fmt.Sprintf("mkdir -p ~/.config/eza && git clone https://github.com/eza-community/eza-themes.git %s", repoPath))
	}
}

func (i *ThemeIntent) ensureYaziFlavors(shell contract.ShellIntegration) {
	repoPath := "~/.config/yazi/flavors-repo"
	exists, _ := shell.Run(fmt.Sprintf("[ -d %s ] && echo true || echo false", repoPath))
	if strings.TrimSpace(exists) != "true" {
		shell.Run(fmt.Sprintf("mkdir -p ~/.config/yazi && git clone https://github.com/yazi-rs/flavors.git %s", repoPath))
	}
}

func (i *ThemeIntent) applyWezterm(appearance, themeName string, fs contract.FSIntegration) {
	state := fmt.Sprintf("return { theme = \"%s\", appearance = \"%s\" }", themeName, appearance)
	fs.WriteFile("~/.config/wezterm/theme-state.lua", []byte(state))
}

func (i *ThemeIntent) applyNeovim(appearance, themeName string, fs contract.FSIntegration, shell contract.ShellIntegration) {
	state := fmt.Sprintf("{\"theme\": \"%s\", \"appearance\": \"%s\"}", themeName, appearance)
	fs.WriteFile("~/.config/nvim/theme_state.json", []byte(state))
	shell.Run("for socket in $(lsof -U -a -c nvim -F n | awk '/^n\\/.*nvim/ {print substr($0, 2)}'); do nvim --server \"$socket\" --remote-expr 'v:lua.require(\"config.theme\").reload()' || true; done")
}

func (i *ThemeIntent) applyTmux(appearance string, theme map[string]any, fs contract.FSIntegration, shell contract.ShellIntegration) {
	bgVal := theme["tmux_bg"]
	bg, ok := bgVal.(string)
	if !ok {
		if appearance == "dark" {
			bg = "#2e3440"
		} else {
			bg = "#fdfdfd"
		}
	}
	
	templateData, err := fs.ReadFile("~/.config/tmux/theme.template")
	if err != nil {
		return
	}
	conf := strings.ReplaceAll(string(templateData), "{bg}", bg)
	conf = strings.ReplaceAll(conf, "{appearance}", appearance)
	fs.WriteFile("~/.config/tmux/theme.conf", []byte(conf))
	shell.Run("tmux source-file ~/.config/tmux/tmux.conf")
}

func (i *ThemeIntent) applyGhostty(appearance, light, dark string, fs contract.FSIntegration) {
	conf := fmt.Sprintf("theme = light:%s,dark:%s\n", light, dark)
	fs.WriteFile("~/.config/ghostty/current_theme.conf", []byte(conf))
}

func (i *ThemeIntent) applyZsh(appearance string, theme map[string]any, shell contract.ShellIntegration, fs contract.FSIntegration) {
	ezaTheme := i.getString(theme, "eza", "default")
	ezaSrc := fmt.Sprintf("~/.config/eza/themes-repo/themes/%s.yml", ezaTheme)
	shell.Run(fmt.Sprintf("ln -sf %s ~/.config/eza/theme.yml", ezaSrc))
	fs.WriteFile("~/.config/zsh/theme_env.zsh", []byte("# Theme environment variables (managed by clara)"))
}

func (i *ThemeIntent) applyYazi(appearance string, theme map[string]any, shell contract.ShellIntegration) {
	flavor := i.getString(theme, "yazi", "nord")
	yaziSrc := fmt.Sprintf("~/.config/yazi/flavors-repo/%s.yazi/flavor.toml", flavor)
	shell.Run(fmt.Sprintf("ln -sf %s ~/.config/yazi/theme.toml", yaziSrc))
}

func (i *ThemeIntent) applyGemini(appearance, themeName string, shell contract.ShellIntegration) {
	shell.Run(fmt.Sprintf("jq '.ui.theme = \"%s\"' ~/.gemini/settings.json > ~/.gemini/settings.json.tmp && mv ~/.gemini/settings.json.tmp ~/.gemini/settings.json", themeName))
}

func (i *ThemeIntent) applyAider(appearance string, shell contract.ShellIntegration) {
	if appearance == "dark" {
		shell.Run("sed -i '' 's/dark-mode: .*/dark-mode: true/' ~/.aider.conf.yml")
		shell.Run("sed -i '' 's/code-theme: .*/code-theme: nord/' ~/.aider.conf.yml")
	} else {
		shell.Run("sed -i '' 's/dark-mode: .*/dark-mode: false/' ~/.aider.conf.yml")
		shell.Run("sed -i '' 's/code-theme: .*/code-theme: bw/' ~/.aider.conf.yml")
	}
}

func (i *ThemeIntent) applyBtop(appearance, themeName string, shell contract.ShellIntegration) {
	shell.Run(fmt.Sprintf("sed -i '' 's/color_theme = .*/color_theme = \"%s\"/' ~/.config/btop/btop.conf", themeName))
}

func main() {
	intent := &ThemeIntent{}

	var pluginMap = map[string]plugin.Plugin{
		"intent": &contract.IntentPlugin{Impl: intent},
	}

	plugin.Serve(&plugin.ServeConfig{
		HandshakeConfig: contract.HandshakeConfig,
		Plugins:         pluginMap,
	})
}
