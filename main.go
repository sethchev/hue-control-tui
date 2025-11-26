package main

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

var (
	headerStyle    = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#BD93F9")).Underline(true)
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
	Name   string `json:"name"`
	Type   string `json:"type"`
	Status string `json:"status"`
}

type model struct {
	light    []Light
	cursor   int
	selected map[int]struct{}
}

func initialModel(lights []Light) model {
	var listLights []Light

	listLights = append(listLights, lights...)

	return model{
		light:    listLights,
		selected: make(map[int]struct{}),
	}
}

func (m model) Init() tea.Cmd {
	return nil
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
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

		// The "enter" key and the spacebar (a literal space) toggle
		// the selected state for the item that the cursor is pointing at.
		case " ":
			_, ok := m.selected[m.cursor]
			if ok {
				delete(m.selected, m.cursor)
			} else {
				m.selected[m.cursor] = struct{}{}
			}

		case "enter":
			// Clear all selections (unselect everything)
			if len(m.selected) > 0 {
				for index := range m.selected {
					lightName := m.light[index].Name
					log.Printf("Toggling light: %s", lightName)
					lightStatus, err := getLightStatus(lightName)
					log.Printf("Current status of %s: %t", lightName, lightStatus)
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
				// Refresh the entire list from the system (most reliable)
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
	light_command := "openhue get lights -j|jq 'map({name: .Name, type: .HueData.metadata.archetype, status: .HueData.on.on})' | jq 'map(if .status == true then .status = \"on\" else .status = \"off\" end)'"

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
	p := tea.NewProgram(initialModel(func() []Light {
		lights, err := returnLights()
		if err != nil {
			log.Fatalf("Error returning lights: %v", err)
		}
		return lights
	}()))
	if _, err := p.Run(); err != nil {
		fmt.Printf("Alas, there's been an error: %v", err)
		os.Exit(1)
	}
}
