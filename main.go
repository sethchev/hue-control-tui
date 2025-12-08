package main

import (
	"crypto/tls"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/openhue/openhue-go"
	"github.com/r3labs/sse/v2"
)

var (
	cursorStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("#FF79C6"))
	selectedStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("#50FA7B"))
	statusOnStyle  = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#50FA7B"))
	statusOffStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#FF5555"))

	// Table border style
	tableStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("#6272A4")).
			Padding(0, 2).
			Margin(1, 0)

	// Openhue home instance
	home *openhue.Home
)

type Light struct {
	ID         string  `json:"id"`
	Name       string  `json:"name"`
	Type       string  `json:"type"`
	Status     string  `json:"status"`
	Brightness float32 `json:"brightness"`
}

type SSEMsg struct {
	Data []byte
}

// Minimal SSE parsing types for filtering "light" events
type SSEDataItem struct {
	ID           string `json:"id"`
	IDV1         string `json:"id_v1"`
	Type         string `json:"type"`
	CreationTime string `json:"creationtime,omitempty"`
	On           *struct {
		On bool `json:"on,omitempty"`
	} `json:"on,omitempty"`
	Dimming struct {
		Brightness float64 `json:"brightness"`
	}
}

type SSEUpdate struct {
	CreationTime string        `json:"creationtime"`
	Data         []SSEDataItem `json:"data"`
	ID           string        `json:"id"`
	Type         string        `json:"type"`
}

type model struct {
	light      []Light
	cursor     int
	selected   map[int]struct{}
	sseChannel chan []byte
}

func initialModel(lights []Light, sseChannel chan []byte) model {
	var listLights []Light

	listLights = append(listLights, lights...)

	return model{
		light:      listLights,
		selected:   make(map[int]struct{}),
		sseChannel: sseChannel,
	}
}

func (m model) Init() tea.Cmd {
	return func() tea.Msg {
		data := <-m.sseChannel
		return SSEMsg{Data: data}
	}
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case SSEMsg:
		// Parse SSE JSON and handle only inner items of type "light"
		var updates []SSEUpdate
		if err := json.Unmarshal(msg.Data, &updates); err != nil {
			log.Printf("SSE: failed to parse JSON: %v", err)
			log.Printf("raw: %s", string(msg.Data))
			return m, m.Init()
		}

		for _, upd := range updates {
			// top-level update.Type may be "update" etc.; iterate inner data
			for _, item := range upd.Data {
				if item.Type != "light" {
					// ignore non-light events
					continue
				}
				log.Printf("Entire item: %+v", item)
				// Log event for debugging
				log.Printf("SSE light event: id=%s id_v1=%s on=%v brightness=%v",
					item.ID, item.IDV1, item.On, item.Dimming.Brightness)

				// Update lights based on ID match
				// Only update status if the On field was present in the JSON
				if item.On != nil {
					for i := range m.light {
						if m.light[i].ID == item.ID {
							if item.On.On {
								m.light[i].Status = "on"
							} else {
								m.light[i].Status = "off"
							}
						}
					}
				}

				// Update brightness if present
				if item.Dimming.Brightness != 0 {
					for i := range m.light {
						if m.light[i].ID == item.ID {
							m.light[i].Brightness = float32(item.Dimming.Brightness)
						}
					}
				}
			}
		}

		return m, m.Init()
	case tea.KeyMsg:
		switch msg.String() {
		// These keys should exit the program.
		case "ctrl+c", "q":
			return m, tea.Quit

		// The "up" and "k" keys move the cursor up
		case "up", "k":
			if m.cursor > 0 {
				m.cursor--
			}

		// The "down" and "j" keys move the cursor down
		case "down", "j":
			if m.cursor < len(m.light)-1 {
				m.cursor++
			}

		case "right", "l":
			if len(m.selected) > 0 {
				for index := range m.selected {
					lightID := m.light[index].ID
					lightBright, err := setLightBrightness(lightID, 10)
					if err != nil {
						log.Printf("Error setting light brightness for %s: %v", lightID, err)
						continue
					}
					log.Printf("Increased brightness of light %s to %d", lightID, lightBright)
				}
			}

		case "left", "h":
			if len(m.selected) > 0 {
				for index := range m.selected {
					lightID := m.light[index].ID
					lightBright, err := setLightBrightness(lightID, -10)
					if err != nil {
						log.Printf("Error setting light brightness for %s: %v", lightID, err)
						continue
					}
					log.Printf("Decreasing brightness of light %s to %d", lightID, lightBright)
				}
			}

		// The spacebar toggles item for selection
		case " ":
			_, ok := m.selected[m.cursor]
			if ok {
				delete(m.selected, m.cursor)
			} else {
				m.selected[m.cursor] = struct{}{}
			}

		case "enter":
			// If something is selected
			if len(m.selected) > 0 {
				for index := range m.selected {
					lightID := m.light[index].ID
					lightStatus, err := getLightStatus(lightID)
					if err != nil {
						log.Printf("Error getting light status for %s: %v", lightID, err)
						continue
					}
					err = toggleLight(lightID, lightStatus)
					if err != nil {
						log.Printf("Error toggling light for %s: %v", lightID, err)
						continue
					}
					m.selected = make(map[int]struct{})
				}
				// Refresh the entire list
				freshLights, err := returnLights()
				if err != nil {
					log.Printf("Warning: Failed to refresh lights after toggle: %v", err)
				} else {
					m.light = freshLights
				}

				m.selected = make(map[int]struct{})
			}
		}
	}

	return m, nil
}

func (m model) View() string {
	const (
		nameWidth       = 16
		statusWidth     = 6
		brightnessWidth = 10
	)

	// Styles
	headerStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#BD93F9"))
	dividerStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#44475A"))

	var rows []string

	// Header row — built exactly like data rows → perfect alignment
	header := lipgloss.NewStyle().Width(nameWidth).Render(headerStyle.Render("NAME")) + "  " +
		lipgloss.NewStyle().Width(statusWidth).Render(headerStyle.Render("STATUS")) + "  " +
		lipgloss.NewStyle().Width(brightnessWidth).Render(headerStyle.Render("BRIGHTNESS"))

	rows = append(rows, "  "+header)

	// Horizontal divider
	divider := lipgloss.NewStyle().Width(nameWidth).Render(dividerStyle.Render(strings.Repeat("─", nameWidth))) + "  " +
		lipgloss.NewStyle().Width(statusWidth).Render(dividerStyle.Render(strings.Repeat("─", statusWidth))) + "  " +
		lipgloss.NewStyle().Width(brightnessWidth).Render(dividerStyle.Render(strings.Repeat("─", brightnessWidth)))

	rows = append(rows, "  "+divider)

	// Data rows
	for i, light := range m.light {
		cursor := "  "
		if m.cursor == i {
			cursor = cursorStyle.Render("▶ ")
		}

		checkmark := " "
		if _, ok := m.selected[i]; ok {
			checkmark = selectedStyle.Render("✓ ")
		}

		// Truncate long names/types
		name := light.Name
		if len(name) > nameWidth {
			name = name[:nameWidth-3] + "..."
		}

		status := "OFF"
		if light.Status == "on" {
			status = statusOnStyle.Render("ON ")
		} else {
			status = statusOffStyle.Render("OFF")
		}

		bright := fmt.Sprintf("%.0f%%", light.Brightness)
		bright = lipgloss.NewStyle().Width(brightnessWidth).Render(bright)

		row := cursor + checkmark +
			lipgloss.NewStyle().Width(nameWidth).Render(name) + "  " +
			lipgloss.NewStyle().Width(statusWidth).Render(status) + "  " +
			lipgloss.NewStyle().Width(brightnessWidth).Render(bright)
		rows = append(rows, "  "+row)
	}

	// Join everything
	tableContent := lipgloss.JoinVertical(lipgloss.Left, rows...)

	// Box with padding
	boxed := tableStyle.Render(tableContent)

	// Title & footer
	title := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#FF79C6")).MarginLeft(2).Render("Your Hue Lights")
	footer := lipgloss.NewStyle().Faint(true).MarginTop(1).MarginLeft(2).Render("• Press space to select • < > to adjust brightness \n• Enter to toggle on/off • q to quit.")

	return title + "\n" + boxed + footer
}

func returnLights() ([]Light, error) {
	lights, err := home.GetLights()
	if err != nil {
		return nil, fmt.Errorf("error fetching lights: %v", err)
	}

	// Extract IDs and sort them to maintain consistent order
	ids := make([]string, 0, len(lights))
	for id := range lights {
		ids = append(ids, id)
	}
	sort.Strings(ids)

	var result []Light
	for _, id := range ids {
		light := lights[id]
		status := "off"
		if light.IsOn() {
			status = "on"
		}
		result = append(result, Light{
			ID:         id,
			Name:       *light.Metadata.Name,
			Type:       string(*light.Metadata.Archetype),
			Status:     status,
			Brightness: *light.Dimming.Brightness,
		})
	}
	return result, nil
}

func getLightStatus(lightID string) (bool, error) {
	lights, err := home.GetLights()
	if err != nil {
		return false, fmt.Errorf("error fetching lights: %v", err)
	}
	light, ok := lights[lightID]
	if !ok {
		return false, fmt.Errorf("light not found: %s", lightID)
	}
	return light.IsOn(), nil
}

func toggleLight(lightID string, currentStatus bool) error {
	newStatus := !currentStatus
	log.Printf("Toggling light %s from %t to %t", lightID, currentStatus, newStatus)
	return home.UpdateLight(lightID, openhue.LightPut{
		On: &openhue.On{On: &newStatus},
	})
}

func setLightBrightness(lightID string, change int) (int, error) {
	lights, err := home.GetLights()
	if err != nil {
		return 0, fmt.Errorf("error fetching lights: %v", err)
	}
	light, ok := lights[lightID]
	if !ok {
		return 0, fmt.Errorf("light not found: %s", lightID)
	}
	currentBrightness := int(*light.Dimming.Brightness)
	newBrightness := currentBrightness + change
	if newBrightness < 0 {
		newBrightness = 0
	} else if newBrightness > 100 {
		newBrightness = 100
	}
	log.Printf("Setting brightness of light %s from %d to %d", lightID, currentBrightness, newBrightness)
	brightnessFinal := openhue.Brightness(newBrightness)
	err = home.UpdateLight(lightID, openhue.LightPut{
		Dimming: &openhue.Dimming{Brightness: &brightnessFinal},
	})
	if err != nil {
		return currentBrightness, fmt.Errorf("error updating brightness: %v", err)
	}
	return newBrightness, nil
}

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

func main() {
	bridge_ip := flag.String("bridge_ip", "", "IP address of the Hue Bridge")
	hue_application_key := flag.String("key", "", "Hue application key")
	flag.Parse()

	// Set up logging to file
	f, err := tea.LogToFile("debug.log", "debug")
	if err != nil {
		fmt.Println("fatal:", err)
		os.Exit(1)
	}
	defer f.Close()

	var bridgeIP, apiKey string

	// Try flags first
	if *bridge_ip != "" && *hue_application_key != "" {
		bridgeIP = *bridge_ip
		apiKey = *hue_application_key
		log.Println("Using flags for bridge connection")
	} else {
		// Try config file
		log.Println("Flags not found, checking config file...")
		_, err := openhue.LoadConf()
		if err != nil {
			// No config file, start bridge setup TUI
			log.Println("No config file found, starting bridge setup...")
			setupModel := bridgeSetupModel{step: 0}
			p := tea.NewProgram(setupModel)

			if _, err := p.Run(); err != nil {
				fmt.Printf("Error during setup: %v", err)
				os.Exit(1)
			}

			// After setup, try loading config again
			_, err = openhue.LoadConf()
			if err != nil {
				fmt.Println("Setup was cancelled or failed")
				os.Exit(1)
			}
		}

		// Load from config
		bridgeIP, apiKey = openhue.LoadConfNoError()
	}

	// Initialize openhue home instance
	home, err = openhue.NewHome(bridgeIP, apiKey)
	if err != nil {
		log.Fatalf("Failed to create openhue home: %v", err)
	}

	// Create channel for SSE events
	sseChannel := make(chan []byte)

	// Start SSE client in a goroutine so it doesn't block the TUI
	go func() {
		sse_client := sse.NewClient("https://" + bridgeIP + "/eventstream/clip/v2")
		sse_client.Connection.Transport = &http.Transport{
			TLSClientConfig: &tls.Config{
				InsecureSkipVerify: true,
			},
		}
		sse_client.Headers["hue-application-key"] = apiKey
		err := sse_client.SubscribeRaw(func(msg *sse.Event) {
			sseChannel <- msg.Data
		})
		if err != nil {
			log.Printf("Error subscribing to SSE: %v", err)
		}
	}()

	p := tea.NewProgram(initialModel(func() []Light {
		lights, err := returnLights()
		if err != nil {
			log.Fatalf("Error returning lights: %v", err)
		}
		return lights
	}(), sseChannel))

	if _, err := p.Run(); err != nil {
		fmt.Printf("Alas, there's been an error: %v", err)
		os.Exit(1)
	}
}
