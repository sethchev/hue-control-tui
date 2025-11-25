package main

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"strconv"

	tea "github.com/charmbracelet/bubbletea"
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
	s := "Your Hue Lights\n\n"

	for i, light := range m.light {
		// Is the cursor pointing at this choice?
		cursor := " " // no cursor
		if m.cursor == i {
			cursor = ">" // cursor!
		}

		// Is this choice selected?
		checked := " " // not selected
		if _, ok := m.selected[i]; ok {
			checked = "x" // selected!
		}

		// Render the row (show the light's name)
		s += fmt.Sprintf("%s [%s] %s %s %s\n", cursor, checked, light.Name, light.Type, light.Status)
	}
	// The footer
	s += "\nPress q to quit.\n"

	// Send the UI for rendering
	return s
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
