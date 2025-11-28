package main

import (
	"crypto/tls"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
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
)

type Light struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Type   string `json:"type"`
	Status string `json:"status"`
}

type SSEMsg struct {
	Data []byte
}

// Minimal SSE parsing types for filtering "light" events
type SSEDataItem struct {
	ID           string `json:"id"`
	IDV1         string `json:"id_v1"`
	Type         string `json:"type"`
	CreationTime string `json:"creationtime"`
	On           struct {
		On bool `json:"on"`
	} `json:"on,omitempty"`
	Dimming struct {
		Brightness float64 `json:"brightness"`
	} `json:"dimming,omitempty"`
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

				// Log event for debugging
				log.Printf("SSE light event: id=%s id_v1=%s on=%v brightness=%v",
					item.ID, item.IDV1, item.On.On, item.Dimming.Brightness)

				// Update lights based on ID match
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
					lightName := m.light[index].Name
					lightStatus, err := getLightStatus(lightName)
					if err != nil {
						log.Printf("Error getting light status for %s: %v", lightName, err)
						continue
					}
					err = toggleLight(lightName, lightStatus)
					if err != nil {
						log.Printf("Error toggling light for %s: %v", lightName, err)
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
		nameWidth   = 30
		typeWidth   = 18
		statusWidth = 6
	)

	// Styles
	headerStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#BD93F9"))
	dividerStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#44475A"))

	var rows []string

	// Header row — built exactly like data rows → perfect alignment
	header := lipgloss.NewStyle().Width(nameWidth).Render(headerStyle.Render("NAME")) + "  " +
		lipgloss.NewStyle().Width(typeWidth).Render(headerStyle.Render("TYPE")) + "  " +
		lipgloss.NewStyle().Width(statusWidth).Render(headerStyle.Render("STATUS"))

	rows = append(rows, "  "+header)

	// Horizontal divider
	divider := lipgloss.NewStyle().Width(nameWidth).Render(dividerStyle.Render(strings.Repeat("─", nameWidth))) + "  " +
		lipgloss.NewStyle().Width(typeWidth).Render(dividerStyle.Render(strings.Repeat("─", typeWidth))) + "  " +
		lipgloss.NewStyle().Width(statusWidth).Render(dividerStyle.Render(strings.Repeat("─", statusWidth)))

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
		typ := light.Type
		if len(typ) > typeWidth {
			typ = typ[:typeWidth-3] + "..."
		}

		status := "OFF"
		if light.Status == "on" {
			status = statusOnStyle.Render("ON ")
		} else {
			status = statusOffStyle.Render("OFF")
		}

		row := cursor + checkmark +
			lipgloss.NewStyle().Width(nameWidth).Render(name) + "  " +
			lipgloss.NewStyle().Width(typeWidth).Render(typ) + "  " +
			lipgloss.NewStyle().Width(statusWidth).Render(status)

		rows = append(rows, "  "+row)
	}

	// Join everything
	tableContent := lipgloss.JoinVertical(lipgloss.Left, rows...)

	// Box with padding
	boxed := tableStyle.Render(tableContent)

	// Title & footer
	title := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#FF79C6")).MarginLeft(2).Render("Your Hue Lights")
	footer := lipgloss.NewStyle().Faint(true).MarginTop(1).MarginLeft(2).Render("Press space to select • enter to toggle • q to quit.")

	return title + "\n\n" + boxed + "\n" + footer
}

func returnLights() ([]Light, error) {
	light_command := "openhue get lights -j|jq 'map({id: .Id, name: .Name, type: .HueData.metadata.archetype, status: .HueData.on.on})' | jq 'map(if .status == true then .status = \"on\" else .status = \"off\" end)'"

	cmd := exec.Command("bash", "-c", light_command)
	output, err := cmd.Output()

	if err != nil {
		log.Fatalf("Error executing command: %v", err)
	}

	var lights []Light
	err = json.Unmarshal(output, &lights)
	if err != nil {
		log.Fatalf("Error parsing JSON: %v", err)
	}

	return lights, nil
}

func getLightStatus(lightName string) (bool, error) {
	light_command := "openhue get light \"" + lightName + "\" -j|jq -r '.HueData.on.on'"

	cmd := exec.Command("bash", "-c", light_command)
	output, err := cmd.Output()

	if err != nil {
		return false, fmt.Errorf("error getting light status for %s: %v", lightName, err)
	}
	string_output := string(output)
	bool_output, err := strconv.ParseBool(string_output[:len(string_output)-1])
	if err != nil {
		return false, fmt.Errorf("error parsing light status for %s: %v", lightName, err)
	}
	return bool_output, nil
}

func toggleLight(lightName string, lightStatus bool) error {
	log.Printf("Toggling light %s with current status %t", lightName, lightStatus)
	if lightStatus {
		light_command := "openhue set light \"" + lightName + "\" --off"

		cmd := exec.Command("bash", "-c", light_command)
		_, err := cmd.Output()
		if err != nil {
			return fmt.Errorf("error turning light off for %s: %v", lightName, err)
		}
	} else {
		light_command := "openhue set light \"" + lightName + "\" --on"
		cmd := exec.Command("bash", "-c", light_command)
		_, err := cmd.Output()
		if err != nil {
			return fmt.Errorf("error turning light on for %s: %v", lightName, err)
		}
	}
	return nil
}

func main() {
	f, err := tea.LogToFile("debug.log", "debug")
	if err != nil {
		fmt.Println("fatal:", err)
		os.Exit(1)
	}
	defer f.Close()

	// Create channel for SSE events
	sseChannel := make(chan []byte)

	// Start SSE client in a goroutine so it doesn't block the TUI
	go func() {
		sse_client := sse.NewClient("https://192.168.1.14/eventstream/clip/v2")
		sse_client.Connection.Transport = &http.Transport{
			TLSClientConfig: &tls.Config{
				InsecureSkipVerify: true,
			},
		}
		sse_client.Headers["hue-application-key"] = "rk6XE6tH06SNw8k4CMom57T4-CHcPZAdNrzo7xSe"
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
