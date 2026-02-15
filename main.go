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

func main() {
	 
	 
	exePath, err := os.Executable()
	if err == nil {
		exeDir := filepath.Dir(exePath)
		os.Chdir(exeDir)
	}

	hostMode := flag.Bool("host", false, "Host an onion servie instance.")
	connectAddr := flag.String("connect", "", ".onion address to connect to.")
	secret := flag.String("secret", "", "Shared secret for handshake authentication")
	flag.Parse()

	 

	 
	 
	salt, err := LoadOrGenerateSalt()
	if err != nil {
		fmt.Printf("Critical Error: Failed to load/generate salt: %v\n", err)
		os.Exit(1)
	}

	 
	_, cancel := context.WithCancel(context.Background())
	defer cancel()

	 
	nm := NewNetworkManager()
	defer nm.Close()

	 
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-sigChan
		cancel()
		nm.Close()
		os.Exit(0)
	}()

	 
	progressChan := make(chan string, 10)

	 
	m := initialModel(nm, salt, *hostMode, *connectAddr, *secret, progressChan)
	p := tea.NewProgram(m, tea.WithAltScreen())

	go func() {
		for msg := range progressChan {
			p.Send(statusMsg(msg))
		}
	}()

	 
	 
	 

	 
	if _, err := p.Run(); err != nil {
		fmt.Printf("Alas, there's been an error: %v\n", err)
		waitExit()
		os.Exit(1)
	}
}

func waitExit() {
	fmt.Println("\nPress Enter to exit...")
	bufio.NewReader(os.Stdin).ReadString('\n')
	 
	time.Sleep(500 * time.Millisecond)
}
