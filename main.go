package main

import (
	"crypto/tls"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"

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
