package main

import (
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/openhue/openhue-go"
)

// bridgeSetupModel represents the TUI state for bridge setup
type bridgeSetupModel struct {
	showDiscovery bool
	discovering   bool
	bridgeIP      string
	apiKey        string
	error         string
	step          int // 0: prompt, 1: discovering, 2: press button, 3: complete
}

func (m bridgeSetupModel) Init() tea.Cmd {
	return nil
}

func (m bridgeSetupModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch m.step {
		case 0: // Initial prompt
			if msg.String() == "y" || msg.String() == "Y" {
				m.step = 1
				m.discovering = true
				return m, tea.Cmd(func() tea.Msg {
					bridge, err := openhue.NewBridgeDiscovery().Discover()
					if err != nil {
						return bridgeDiscoveryResult{err: err}
					}
					return bridgeDiscoveryResult{bridge: bridge}
				})
			} else if msg.String() == "n" || msg.String() == "N" {
				return m, tea.Quit
			}
		case 2: // Press button step
			if msg.String() == " " || msg.String() == "enter" {
				return m, tea.Cmd(func() tea.Msg {
					authenticator, err := openhue.NewAuthenticator(m.bridgeIP)
					if err != nil {
						return authResult{err: err}
					}
					apiKey, retry, err := authenticator.Authenticate()
					return authResult{apiKey: apiKey, retry: retry, err: err}
				})
			}
		case 3: // Complete
			if msg.String() == "enter" {
				return m, tea.Quit
			}
		}
	case bridgeDiscoveryResult:
		if msg.err != nil {
			m.error = msg.err.Error()
			m.step = 0
		} else {
			m.bridgeIP = msg.bridge.IpAddress
			m.step = 2
		}
		m.discovering = false
	case authResult:
		if msg.err != nil && !msg.retry {
			m.error = msg.err.Error()
		} else if msg.retry {
			m.error = "Link button not pressed. Press spacebar to try again."
		} else {
			m.apiKey = msg.apiKey
			m.step = 3
			// Save config
			saveConfig(m.bridgeIP, m.apiKey)
		}
	}
	return m, nil
}

func (m bridgeSetupModel) View() string {
	switch m.step {
	case 0:
		s := "No Hue Bridge configuration found.\n\n"
		if m.error != "" {
			s += fmt.Sprintf("Error: %s\n\n", m.error)
		}
		s += "Would you like to discover your Hue Bridge? (y/n): "
		return s
	case 1:
		return "Discovering Hue Bridge on your network...\nPlease wait..."
	case 2:
		s := fmt.Sprintf("Found Hue Bridge at: %s\n\n", m.bridgeIP)
		s += "Please press the link button on your Hue Bridge, then press SPACEBAR to continue.\n"
		if m.error != "" {
			s += fmt.Sprintf("\n%s", m.error)
		}
		return s
	case 3:
		s := "Setup complete!\n\n"
		s += fmt.Sprintf("Bridge IP: %s\n", m.bridgeIP)
		s += fmt.Sprintf("API Key: %s\n\n", m.apiKey)
		s += "Configuration saved to ~/.openhue/config.yaml\n"
		s += "Press ENTER to start the application..."
		return s
	}
	return ""
}

type bridgeDiscoveryResult struct {
	bridge *openhue.BridgeInfo
	err    error
}

type authResult struct {
	apiKey string
	retry  bool
	err    error
}

func saveConfig(bridgeIP, apiKey string) error {
	homedir, err := os.UserHomeDir()
	if err != nil {
		return err
	}

	configDir := homedir + "/.openhue"
	err = os.MkdirAll(configDir, 0755)
	if err != nil {
		return err
	}

	config := fmt.Sprintf("bridge: %s\nkey: %s\n", bridgeIP, apiKey)
	return os.WriteFile(configDir+"/config.yaml", []byte(config), 0644)
}
