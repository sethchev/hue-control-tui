package main

import (
	"crypto/tls"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/openhue/openhue-go"
)

type Light struct {
	ID          string  `json:"id"`
	Name        string  `json:"name"`
	Type        string  `json:"type"`
	Status      string  `json:"status"`
	Brightness  float32 `json:"brightness"`
	Reachable   bool    `json:"reachable"`
	DeviceOwner string  `json:"device_owner"` // Device ID for connectivity lookup
}

type Scene struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Type string `json:"type"`
}

// ZigbeeConnectivity represents the connectivity status of a Zigbee device
type ZigbeeConnectivity struct {
	ID    string `json:"id"`
	IDV1  string `json:"id_v1"`
	Owner struct {
		Rid   string `json:"rid"`
		Rtype string `json:"rtype"`
	} `json:"owner"`
	Status string `json:"status"` // "connected", "disconnected", etc.
	Type   string `json:"type"`
}

// ZigbeeConnectivityResponse wraps the API response
type ZigbeeConnectivityResponse struct {
	Errors []interface{}        `json:"errors"`
	Data   []ZigbeeConnectivity `json:"data"`
}

type SSEMsg struct {
	Data []byte
}

// Minimal SSE parsing types for filtering "light" and "zigbee_connectivity" events
type SSEDataItem struct {
	ID           string `json:"id"`
	IDV1         string `json:"id_v1"`
	Type         string `json:"type"`
	CreationTime string `json:"creationtime,omitempty"`
	On           *struct {
		On bool `json:"on,omitempty"`
	} `json:"on,omitempty"`
	Dimming *struct {
		Brightness float64 `json:"brightness"`
	} `json:"dimming,omitempty"`
	Owner *struct {
		Rid   string `json:"rid"`
		Rtype string `json:"rtype"`
	} `json:"owner,omitempty"`
	Status string `json:"status,omitempty"` // For zigbee_connectivity: "connected" or "disconnected"
}

type SSEUpdate struct {
	CreationTime string        `json:"creationtime"`
	Data         []SSEDataItem `json:"data"`
	ID           string        `json:"id"`
	Type         string        `json:"type"`
}

type lightModel struct {
	light       []Light
	cursor      int
	selected    map[int]struct{}
	sseChannel  chan []byte
	commandMode bool
	commandText string
}

func initialModel(lights []Light, sseChannel chan []byte) lightModel {
	var listLights []Light

	listLights = append(listLights, lights...)

	return lightModel{
		light:       listLights,
		selected:    make(map[int]struct{}),
		sseChannel:  sseChannel,
		commandMode: false,
		commandText: "",
	}
}

func (m lightModel) Init() tea.Cmd {
	return func() tea.Msg {
		data := <-m.sseChannel
		return SSEMsg{Data: data}
	}
}

func (m lightModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
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
				// Handle light events
				if item.Type == "light" {
					m = m.handleLightUpdate(item)
				} else if item.Type == "zigbee_connectivity" {
					// Handle connectivity events
					m = m.handleConnectivityUpdate(item)
				}
			}
		}

		return m, m.Init()
	case tea.KeyMsg:
		if m.commandMode {
			switch msg.String() {
			case "escape":
				m.commandMode = false
				m.commandText = ""
			case "enter":
				m.executeCommand(m.commandText)
				m.commandMode = false
				m.commandText = ""
			case "backspace":
				if len(m.commandText) > 0 {
					m.commandText = m.commandText[:len(m.commandText)-1]
				}
			default:
				// Add character to command text
				if len(msg.String()) == 1 {
					m.commandText += msg.String()
				}
			}
		} else {
			switch msg.String() {
			// These keys should exit the program.
			case "ctrl+c", "q":
				return m, tea.Quit

			// Open command mode
			case ":":
				m.commandMode = true
				m.commandText = ""

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
						if !m.light[index].Reachable {
							log.Printf("Skipping unreachable light %s", m.light[index].Name)
							continue
						}
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
						if !m.light[index].Reachable {
							log.Printf("Skipping unreachable light %s", m.light[index].Name)
							continue
						}
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
						if !m.light[index].Reachable {
							log.Printf("Skipping unreachable light %s", m.light[index].Name)
							continue
						}
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
	}

	return m, nil
}

func (m lightModel) View() string {
	const (
		nameWidth       = 30
		statusWidth     = 12
		brightnessWidth = 15
		totalWidth      = nameWidth + statusWidth + brightnessWidth + 10 // includes spacing and padding
	)

	// Styles
	headerStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#BD93F9"))
	dividerStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#44475A"))

	var rows []string

	// Header row — built exactly like data rows → perfect alignment
	header := lipgloss.NewStyle().Width(nameWidth).Render(headerStyle.Render("NAME")) + "  " +
		lipgloss.NewStyle().Width(statusWidth).Render(headerStyle.Render("STATUS")) + "  " +
		lipgloss.NewStyle().Width(brightnessWidth).MarginLeft(3).Render(headerStyle.Render("BRIGHTNESS"))

	rows = append(rows, "  "+header)

	// Horizontal divider
	divider := lipgloss.NewStyle().Width(nameWidth).Render(dividerStyle.Render(strings.Repeat("─", nameWidth))) + "  " +
		lipgloss.NewStyle().Width(statusWidth).Render(dividerStyle.Render(strings.Repeat("─", statusWidth))) + "  " +
		lipgloss.NewStyle().Width(brightnessWidth).MarginLeft(3).Render(dividerStyle.Render(strings.Repeat("─", brightnessWidth)))

	rows = append(rows, "  "+divider)

	// Data rows
	for i, light := range m.light {
		cursor := "  "
		if m.cursor == i {
			cursor = cursorStyle.Render("▶ ")
		}

		checkmark := "  "
		if _, ok := m.selected[i]; ok {
			checkmark = selectedStyle.Render("✓ ")
		}

		// Truncate long names/types
		name := light.Name
		if len(name) > nameWidth {
			name = name[:nameWidth-3] + "..."
		}

		status := "OFF"
		if !light.Reachable {
			status = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#FF8C00")).Render("UNREACHABLE")
		} else if light.Status == "on" {
			status = statusOnStyle.Render("ON")
		} else {
			status = statusOffStyle.Render("OFF")
		}

		bright := ""
		if !light.Reachable {
			bright = lipgloss.NewStyle().Faint(true).Render("N/A")
		} else {
			bright = fmt.Sprintf("%.0f%%", light.Brightness)
		}
		bright = lipgloss.NewStyle().Width(brightnessWidth).MarginLeft(3).Render(bright)

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
	footer := lipgloss.NewStyle().Faint(true).MarginTop(1).MarginLeft(2).Render(
		"• Space: select  • < >: brightness  • Enter: toggle  • :: commands  • q: quit\n" +
			"• Unreachable lights will be skipped  • :refresh to update connectivity status")

	// Always render command box area (static space)
	commandBox := m.renderCommandBox()

	result := title + "\n" + boxed + footer + "\n" + commandBox

	return result
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

		// Get device owner for connectivity check
		deviceOwner := ""
		if light.Owner != nil && light.Owner.Rid != nil {
			deviceOwner = *light.Owner.Rid
		}

		result = append(result, Light{
			ID:          id,
			Name:        *light.Metadata.Name,
			Type:        string(*light.Metadata.Archetype),
			Status:      status,
			Brightness:  *light.Dimming.Brightness,
			Reachable:   true, // Will be updated by checkConnectivity
			DeviceOwner: deviceOwner,
		})
	}

	// Check connectivity status for all lights
	checkConnectivity(result)

	return result, nil
}

// checkConnectivity queries the zigbee_connectivity endpoint and updates Light.Reachable
func checkConnectivity(lights []Light) {
	if home == nil {
		return
	}

	// Make direct API call to get zigbee_connectivity data
	connectivityMap, err := getZigbeeConnectivity()
	if err != nil {
		log.Printf("Warning: Failed to check connectivity: %v", err)
		return
	}

	// Update reachability for each light based on its device owner
	for i := range lights {
		if lights[i].DeviceOwner == "" {
			continue
		}

		status, exists := connectivityMap[lights[i].DeviceOwner]
		if exists {
			lights[i].Reachable = (status == "connected")
		}
	}
}

// getZigbeeConnectivity makes a direct API call to get connectivity status
func getZigbeeConnectivity() (map[string]string, error) {
	// Use global bridgeIP and apiKey
	if bridgeIP == "" || apiKey == "" {
		return nil, fmt.Errorf("bridge configuration not initialized")
	}

	// Build the API URL
	url := fmt.Sprintf("https://%s/clip/v2/resource/zigbee_connectivity", bridgeIP)

	// Create HTTP client with TLS skip verification (same as SSE client)
	client := &http.Client{
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{
				InsecureSkipVerify: true,
			},
		},
	}

	// Create request
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %v", err)
	}

	// Add authentication header
	req.Header.Set("hue-application-key", apiKey)

	// Make request
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to make request: %v", err)
	}
	defer resp.Body.Close()

	// Parse response
	var connectivityResp ZigbeeConnectivityResponse
	if err := json.NewDecoder(resp.Body).Decode(&connectivityResp); err != nil {
		return nil, fmt.Errorf("failed to decode response: %v", err)
	}

	// Build map of device ID -> connectivity status
	connectivityMap := make(map[string]string)
	for _, conn := range connectivityResp.Data {
		if conn.Owner.Rid != "" {
			connectivityMap[conn.Owner.Rid] = conn.Status
		}
	}

	return connectivityMap, nil
}

func getScenes() {
	scenes, err := home.GetScenes()
	if err != nil {
		log.Printf("error fetching scenes: %v", err)
	}

	var result []Scene
	for _, scene := range scenes {
		result = append(result, Scene{
			ID:   *scene.Id,
			Name: *scene.Metadata.Name,
		})
	}
	log.Printf("Scenes: %v", result)

}

func setScene(sceneName string) error {
	log.Printf("Setting scene %s", sceneName)
	// get sceneID from sceneName
	scenes, err := home.GetScenes()
	if err != nil {
		log.Printf("error fetching scenes: %v", err)
	}

	var sceneID string
	for _, scene := range scenes {
		log.Printf("Scene Name: %s", *scene.Metadata.Name)
		log.Printf("Scene being passed in: %v", sceneName)
		if *scene.Metadata.Name == sceneName {
			sceneID = *scene.Id
			break
		}
	}
	if sceneID == "" {
		return fmt.Errorf("scene not found: %s", sceneName)
	}
	log.Printf("Scene ID: %s", sceneID)
	action := openhue.SceneRecallActionActive
	return home.UpdateScene(sceneID, openhue.ScenePut{
		Recall: &openhue.SceneRecall{
			Action: &action,
		},
	})
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

func (m *lightModel) executeCommand(command string) {
	log.Printf("Executing command: %s", command)

	switch command {
	case "help":
		log.Println("Available commands: help, refresh, all_on, all_off, scene <name>")
		log.Println("refresh - Updates light status and checks connectivity")
	case "refresh":
		freshLights, err := returnLights()
		if err != nil {
			log.Printf("Error refreshing lights: %v", err)
		} else {
			m.light = freshLights
			log.Println("Lights refreshed with connectivity status")
		}
	case "all_on":
		for _, light := range m.light {
			if light.Reachable && light.Status == "off" {
				err := toggleLight(light.ID, false)
				if err != nil {
					log.Printf("Error turning on light %s: %v", light.Name, err)
				}
			}
		}
		// Refresh after toggling
		freshLights, err := returnLights()
		if err == nil {
			m.light = freshLights
		}
		log.Println("All lights turned on")
	case "all_off":
		for _, light := range m.light {
			if light.Reachable && light.Status == "on" {
				err := toggleLight(light.ID, true)
				if err != nil {
					log.Printf("Error turning off light %s: %v", light.Name, err)
				}
			}
		}
		// Refresh after toggling
		freshLights, err := returnLights()
		if err == nil {
			m.light = freshLights
		}
		log.Println("All lights turned off")
	default:
		log.Printf("Unknown command: %s", command)
	}
	parts := strings.SplitN(command, " ", 2)
	switch parts[0] {
	case "scene":
		if len(parts) < 2 {
			log.Println("Usage: scene <scene name>")
			return
		}
		sceneName := parts[1]
		setScene(sceneName)
	}
}

func (m lightModel) renderCommandBox() string {
	const totalWidth = 30 + 12 + 15 + 10 // matches table width
	commandBoxStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("#FF79C6")).
		Padding(0, 1).
		Margin(1, 0).
		Width(totalWidth).
		Height(3)

	if m.commandMode {
		prompt := lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#FF79C6")).
			Render(":")

		text := lipgloss.NewStyle().
			Foreground(lipgloss.Color("#F8F8F2")).
			Render(m.commandText)

		cursor := lipgloss.NewStyle().
			Background(lipgloss.Color("#F8F8F2")).
			Foreground(lipgloss.Color("#282A36")).
			Render(" ")

		commandLine := prompt + text + cursor

		help := lipgloss.NewStyle().
			Faint(true).
			Render("Commands: help, refresh, all_on, all_off • ESC to cancel • ENTER to execute")

		content := commandLine + "\n" + help
		return commandBoxStyle.Render(content)
	} else {
		// Show empty box with hint when not in command mode
		hint := lipgloss.NewStyle().
			Faint(true).
			Render("Press : to open command mode")

		content := "\n" + hint
		return commandBoxStyle.Render(content)
	}
}

// handleLightUpdate processes SSE updates for light events
func (m lightModel) handleLightUpdate(item SSEDataItem) lightModel {
	log.Printf("Entire light item: %+v", item)

	// Find the light in our list
	lightIndex := -1
	for i := range m.light {
		if m.light[i].ID == item.ID {
			lightIndex = i
			break
		}
	}

	// Skip if light not found
	if lightIndex == -1 {
		return m
	}

	// Log event for debugging
	brightnessVal := float64(-1)
	if item.Dimming != nil {
		brightnessVal = item.Dimming.Brightness
	}
	log.Printf("SSE light event: id=%s id_v1=%s on=%v brightness=%v",
		item.ID, item.IDV1, item.On, brightnessVal)

	// Update status if the On field was present in the JSON
	if item.On != nil {
		if item.On.On {
			m.light[lightIndex].Status = "on"
		} else {
			m.light[lightIndex].Status = "off"
		}
	}

	// Update brightness if present (including 0 for off lights)
	if item.Dimming != nil {
		m.light[lightIndex].Brightness = float32(item.Dimming.Brightness)
	}

	// If we received any update, the light is reachable
	m.light[lightIndex].Reachable = true

	return m
}

// handleConnectivityUpdate processes SSE updates for zigbee_connectivity events
func (m lightModel) handleConnectivityUpdate(item SSEDataItem) lightModel {
	log.Printf("SSE connectivity event: id=%s owner=%v status=%s",
		item.ID, item.Owner, item.Status)

	// Skip if no owner information
	if item.Owner == nil || item.Owner.Rid == "" {
		return m
	}

	// Find all lights that belong to this device
	deviceID := item.Owner.Rid
	isConnected := (item.Status == "connected")

	for i := range m.light {
		if m.light[i].DeviceOwner == deviceID {
			m.light[i].Reachable = isConnected
			log.Printf("Updated light %s reachability to %v", m.light[i].Name, isConnected)
		}
	}

	return m
}
