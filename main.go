package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// main is the entry point for the Rizoma application.
// It handles initialization, flag parsing, network management, and TUI execution.
func main() {
	// Ensure the working directory is set to the executable's location to find dependencies.
	exePath, err := os.Executable()
	if err == nil {
		exeDir := filepath.Dir(exePath)
		os.Chdir(exeDir)
	}

	// Parse command-line flags for hosting or connecting to services.
	hostMode := flag.Bool("host", false, "Host an onion service instance.")
	connectAddr := flag.String("connect", "", ".onion address to connect to.")
	secret := flag.String("secret", "", "Shared secret for handshake authentication")
	flag.Parse()

	// Load or generate the salt used for master key derivation.
	salt, err := LoadOrGenerateSalt()
	if err != nil {
		fmt.Printf("Critical Error: Failed to load/generate salt: %v\n", err)
		os.Exit(1)
	}

	// Create a context for clean resource termination.
	_, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Initialize the network manager for Tor connectivity.
	nm := NewNetworkManager()
	defer nm.Close()

	// Handle OS signals for graceful shutdown.
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-sigChan
		cancel()
		nm.Close()
		os.Exit(0)
	}()

	// Channel for communicating status updates from network to UI.
	progressChan := make(chan string, 10)

	// Initialize the Bubble Tea model and program.
	m := initialModel(nm, salt, *hostMode, *connectAddr, *secret, progressChan)
	p := tea.NewProgram(m, tea.WithAltScreen())

	// Bridge status messages to the Bubble Tea program.
	go func() {
		for msg := range progressChan {
			p.Send(statusMsg(msg))
		}
	}()

	// Start the UI loop.
	if _, err := p.Run(); err != nil {
		fmt.Printf("Error: %v\n", err)
		waitExit()
		os.Exit(1)
	}
}

// waitExit pauses execution until the user presses Enter, preventing immediate window closure on error.
func waitExit() {
	fmt.Println("\nPress Enter to exit...")
	bufio.NewReader(os.Stdin).ReadString('\n')
	time.Sleep(500 * time.Millisecond)
}
