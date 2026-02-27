package main

import (
	"bufio"
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"math/rand"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/atotto/clipboard"
	"github.com/charmbracelet/bubbles/progress"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/skip2/go-qrcode"
	"golang.org/x/image/font"
	"golang.org/x/image/font/basicfont"
	"golang.org/x/image/math/fixed"
)

// sessionState represents the current view or workflow active in the UI.
type sessionState int

const (
	statePassword              sessionState = iota // Initial authentication screen
	stateMenu                                      // Main dashboard/menu
	stateHost                                      // Hosting a personal onion service
	stateConnect                                   // Connection selection or entry
	stateConnectAddress                            // Manual address entry
	stateConnectSecret                             // Manual secret entry
	stateChat                                      // Active P2P chat room
	stateConfigName                                // Configuration: Change display name
	stateConfigColor                               // Configuration: Change visual color
	stateConfigPasswordCheck                       // Configuration: Verify old password
	stateConfigPasswordNew                         // Configuration: Set new password
	stateError                                     // Terminal error display
	stateNewContactMethod                          // Prompt for new contact (Secret or Address)
	stateNewContactAddress                         // Input for contact address
	stateNewContactSecretEntry                     // Input for contact secret
	stateNewContactAlias                           // Input for contact alias naming
	stateNewContactWait                            // Connection attempt in progress
	stateConnectedAskSave                          // Post-connection prompt to save contact
	stateDeleteContact                             // Contact deletion prompt
)

// incomingMsg wraps a raw network message for Bubble Tea processing.
type incomingMsg string

// errMsg wraps an error for display within the TUI.
type errMsg error

// statusMsg provides non-blocking status updates to the UI.
type statusMsg string

// ChatMessage defines the JSON schema for P2P message exchange.
type ChatMessage struct {
	Content string `json:"content"`
	Name    string `json:"name"`
	Color   string `json:"color"`
	Whisper bool   `json:"whisper,omitempty"`
}

// PingResult encapsulates the availability status of a remote onion contact.
type PingResult struct {
	Alias  string
	Status string // "ONLN" (Online) or "OFFLN" (Offline)
}

// pingMsg signals a completed background ping check.
type pingMsg PingResult

// model represents the global application state and TUI components.
type model struct {
	state          sessionState    // Current UI screen
	chatViewport   viewport.Model  // Main message display scroll area
	statusViewport viewport.Model  // Sidebar/Log display area
	textInput      textinput.Model // Active user input field
	passwordInput  textinput.Model // Specialized secure input field
	passwordMsg    string          // Authentication feedback message

	netManager *NetworkManager // Lifecycle management for Tor and connections
	config     Config          // Persisted application settings
	salt       []byte          // Cryptographic salt for key derivation
	masterKey  []byte          // Local encryption key (derived from password)
	status     string          // High-level system status message
	logs       []string        // History of system events
	err        error           // Latched fatal error state
	ready      bool            // Ready signal for terminal sizing
	width      int             // Terminal width in columns
	height     int             // Terminal height in rows

	contactStatus  map[string]string // Real-time pulse of saved contacts
	sortedContacts []string          // Deterministic list for indexed UI access
	pingChan       chan PingResult   // Asynchronous results from periodic connectivity checks
	lastPing       time.Time         // Last global contact refresh timestamp

	tempConfig Config // Staging area for uncommitted settings changes

	tick      int       // Global UI animation frame counter
	rose      RoseModel // Decorative rose component animator
	startTime time.Time // Process launch timestamp
	msgTimer  int       // Flash message duration tracker
	easterEgg bool      // Hidden visual variant flag

	globalInput textinput.Model // Lower-priority command-bar input
	progress    progress.Model  // File or bootstrap progress indicator

	connectAddrInput   textinput.Model // Dedicated connect-by-address field
	connectSecretInput textinput.Model // Dedicated connect-secret field

	reqHost    bool   // CLI-overridden hosting request
	reqConnect string // CLI-overridden connection target
	reqSecret  string // CLI-overridden shared secret

	progressChan chan string // Stream for background task log injection
	currentToken string      // Active sharing token or connection string

	fileTransfers   map[string]*FileTransferState // Tracking for active data streams
	folderSendCount int                           // Current progress of multi-file batch
	folderSendTotal int                           // Total items in active multi-file batch

	timestampsEnabled bool      // Configuration for message header display
	tempNick          string    // Ephemeral identity override
	tempColor         string    // Ephemeral visual styling override
	connectionStart   time.Time // Timestamp of current peer link establishment
	confirmLeave      bool      // Interrupt state for destructive navigation

	pendingFileOffer        *FileMessage // Awaiting user acceptance for incoming file
	pendingFileOfferFrom    string       // Identity of the remote file sender
	awaitingFileConfirm     bool         // Logic gate for file accept/deny input
	awaitingBradburyConfirm bool         // Logic gate for specialized PDF reveal
}

// GenerateStyledQR creates a high-recovery QR code image with the project's signature aesthetic:
// a black background with high-contrast crimson module styling.
func GenerateStyledQR(encryptedKey, alias, secret string) (string, error) {
	// Create qr_codes directory if it doesn't exist
	qrDir := "qr_codes"
	if err := os.MkdirAll(qrDir, 0755); err != nil {
		return "", fmt.Errorf("failed to create qr_codes directory: %v", err)
	}

	// Generate QR code with max recovery level for better scanning
	qr, err := qrcode.New(encryptedKey, qrcode.High)
	if err != nil {
		return "", fmt.Errorf("failed to create QR code: %v", err)
	}

	// Use large size for chunky pixels - match canvas width for edge-to-edge display
	qrSize := 500
	qr.DisableBorder = true // Disable border for edge-to-edge effect

	// Generate the QR code image with default colors (we'll recolor it)
	qrImg := qr.Image(qrSize)

	// Create canvas with black background and minimal space for text
	canvasWidth := 500
	canvasHeight := 540 // Reduced height to remove extra space below
	canvas := image.NewRGBA(image.Rect(0, 0, canvasWidth, canvasHeight))

	// Fill with black background
	black := color.RGBA{0, 0, 0, 255}
	draw.Draw(canvas, canvas.Bounds(), &image.Uniform{black}, image.Point{}, draw.Src)

	// Red color for QR and text (#FF0000)
	red := color.RGBA{255, 0, 0, 255}

	// Convert QR code to red on black
	qrBounds := qrImg.Bounds()
	offsetX := 0 // Align to left edge for edge-to-edge display
	offsetY := 0 // Minimal top padding

	for y := qrBounds.Min.Y; y < qrBounds.Max.Y; y++ {
		for x := qrBounds.Min.X; x < qrBounds.Max.X; x++ {
			// Get original pixel color
			originalColor := qrImg.At(x, y)
			r, g, b, _ := originalColor.RGBA()

			// If pixel is dark (QR module), make it red, otherwise keep black
			if r < 32768 && g < 32768 && b < 32768 {
				canvas.Set(x+offsetX, y+offsetY, red)
			}
		}
	}

	// Add text labels below QR code with minimal spacing
	textY := offsetY + qrBounds.Dy() + 20

	// Draw alias on left edge: //ALIAS
	aliasText := "//" + alias
	addLabel(canvas, aliasText, 10, textY, red) // Left edge at x=10

	// Draw secret on right edge: [SECRET]
	secretText := "[" + secret + "]"
	secretWidth := len(secretText) * 7                                   // Approximate width
	addLabel(canvas, secretText, canvasWidth-secretWidth-10, textY, red) // Right edge at x=canvasWidth-width-10

	// Save image with timestamp
	timestamp := time.Now().Format("20060102_150405")
	filename := filepath.Join(qrDir, fmt.Sprintf("qr_%s.png", timestamp))

	file, err := os.Create(filename)
	if err != nil {
		return "", fmt.Errorf("failed to create file: %v", err)
	}
	defer file.Close()

	if err := png.Encode(file, canvas); err != nil {
		return "", fmt.Errorf("failed to encode PNG: %v", err)
	}

	return filename, nil
}

// addLabel renders a text string onto an image at the specified coordinates using a fixed-width font.
func addLabel(img *image.RGBA, label string, x, y int, col color.Color) {
	point := fixed.Point26_6{X: fixed.I(x), Y: fixed.I(y)}

	d := &font.Drawer{
		Dst:  img,
		Src:  image.NewUniform(col),
		Face: basicfont.Face7x13,
		Dot:  point,
	}
	d.DrawString(label)
}

// initialModel constructs the foundation of the TUI, configuring all input fields and sub-models.
func initialModel(nm *NetworkManager, salt []byte, reqHost bool, reqConnect string, reqSecret string, progressChan chan string) model {
	// Chat Input
	ti := textinput.New()
	ti.Placeholder = "Enter command..."
	ti.Focus()
	// No character limit - allow unlimited message length
	// Match Main Menu Styles
	ti.TextStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("255")) // White text
	// Prompt? Chat mode usually has no prompt or "> ". Let's keep it clean or minimal.
	// But User wants "colors that it uses in main menu view".
	// Main menu has white text, crimson cursor.
	// ti.PromptStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("205")) // REMOVING to match GI (which has default/white prompt)
	ti.Cursor.Style = lipgloss.NewStyle().Foreground(lipgloss.Color("196")).Bold(true) // Crimson Red Cursor

	// Password Input
	pi := textinput.New()
	pi.Placeholder = ""
	pi.EchoMode = textinput.EchoPassword
	pi.Prompt = "Password: "
	pi.Focus()
	pi.TextStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("196")).Bold(true)    // Crimson Red
	pi.PromptStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("196")).Bold(true)  // Crimson Red
	pi.Cursor.Style = lipgloss.NewStyle().Foreground(lipgloss.Color("196")).Bold(true) // Crimson Red
	pi.Cursor.Style = lipgloss.NewStyle().Foreground(lipgloss.Color("196")).Bold(true).Reverse(true)
	// pi.Cursor.SetMode(cursor.Block) // Block constant unavailable
	pi.Cursor.Blink = true

	// Global Input (Bottom)
	gi := textinput.New()
	gi.Placeholder = "Enter command..."
	gi.CharLimit = 10000 // Increased to support very long messages
	gi.TextStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("255"))
	gi.Cursor.Style = lipgloss.NewStyle().Foreground(lipgloss.Color("196")).Bold(true)

	// Connect Inputs
	cai := textinput.New()
	cai.Placeholder = "onion_address.onion"
	cai.CharLimit = 2048                                                  // Increased to support long tokens and addresses
	cai.TextStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("205")) // Pink
	cai.Prompt = "Peer Address: "
	cai.Cursor.Style = lipgloss.NewStyle().Foreground(lipgloss.Color("205"))

	csi := textinput.New()
	csi.Placeholder = "Shared Secret"
	csi.CharLimit = 1000                  // Increased to support longer secrets
	csi.EchoMode = textinput.EchoPassword // Fix: Hidden input
	csi.Prompt = "Secret: "
	csi.Cursor.Style = lipgloss.NewStyle().Foreground(lipgloss.Color("205"))

	// Progress Bar
	prog := progress.New(progress.WithDefaultGradient())
	prog.Width = 30 // Initial width, will resize

	// Animation Models
	rose := NewRoseModel()

	// Easter Egg Calculation (1/1000 chance)
	isEasterEgg := false
	if rand.Intn(1000) == 0 {
		isEasterEgg = true
	}

	return model{
		startTime:     time.Now(),
		state:         statePassword,
		textInput:     ti,
		passwordInput: pi,
		globalInput:   gi,
		netManager:    nm,
		salt:          salt,
		reqHost:       reqHost,
		reqConnect:    reqConnect,
		reqSecret:     reqSecret,
		rose:          rose,
		easterEgg:     isEasterEgg,
		status:        "Awaiting Authentication...",
		logs:          []string{"System Initialized."},
		progressChan:  progressChan,

		connectAddrInput:   cai,
		connectSecretInput: csi,
		contactStatus:      make(map[string]string),
		pingChan:           make(chan PingResult),
		fileTransfers:      make(map[string]*FileTransferState),
	}
}

// hashColor generates a consistent, aesthetically pleasing color code for a given string (e.g., an alias).
func hashColor(s string) string {
	sum := 0
	for _, c := range s {
		sum += int(c)
	}
	// Palette of bright neon/hackery colors
	colors := []string{"196", "205", "46", "39", "51", "226", "208", "201", "165", "118"}
	return colors[sum%len(colors)]
}

// checkContacts performs background connectivity checks for a map of onion addresses.
func checkContacts(contacts map[string]string, nm *NetworkManager, ch chan PingResult) {
	// Limit concurrency
	sem := make(chan struct{}, 3) // Max 3 concurrent pings // WaitGroup needs sync package
	// We need to import sync.
	// Actually, just loop sequentially for MVP robustness if sync is missing?
	// Or just fire and forget without wait group since it's a daemon?
	// Better: Use semaphore to limit, but don't wait.

	for alias, addr := range contacts {
		go func(a, ad string) {
			sem <- struct{}{}        // Acquire
			defer func() { <-sem }() // Release

			online := nm.CheckConnection(ad)
			status := "OFFLN"
			if online {
				status = "ONLN"
			}
			ch <- PingResult{Alias: a, Status: status}
		}(alias, addr)
	}
}

// cryptoQuotes removed in favor of quotes.go

type tickMsg time.Time

func tick() tea.Cmd {
	return tea.Tick(time.Millisecond*50, func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
}

func (m model) Init() tea.Cmd {
	return tea.Batch(
		textinput.Blink,
		waitForIncoming(m.netManager),
		waitForPing(m.pingChan),
	)
}

func waitForPing(ch chan PingResult) tea.Cmd {
	return func() tea.Msg {
		return pingMsg(<-ch)
	}
}

// handleChatCommand parses and executes internal slash commands within the active chat session.
// It returns true if the input was handled as a command, along with any associated tea.Cmd.
func (m *model) handleChatCommand(input string) (bool, tea.Cmd) {
	if !strings.HasPrefix(input, "/") {
		return false, nil
	}

	parts := strings.Fields(input)
	if len(parts) == 0 {
		return false, nil
	}

	cmd := strings.ToLower(parts[0])
	args := parts[1:]

	sysStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("240")).Italic(true)
	errorStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("196"))

	switch cmd {
	// Utility Commands
	case "/help":
		// If no args, show brief list
		if len(args) == 0 {
			helpText := `Available Commands:

/host /connect /contact /preheat /leave /help /clear /timestamp /color /nick
/online /loadingbay /whisper /coinflip /dice /quote /ping /reconnect /rehost /qr /rizoma

Shortcuts:
CTRL + Y: Copy encrypted key
CTRL + L: Copy logs

Type /help <command> for details on a specific command.`

			m.logs = append(m.logs, helpText)
			m.statusViewport.SetContent(m.renderLogs(m.statusViewport.Width))
			m.statusViewport.GotoBottom()
			return true, nil
		}

		// Show detailed help for specific command
		helpCmd := strings.ToLower(args[0])
		var detailedHelp string

		switch helpCmd {
		case "host":
			detailedHelp = "/host <secret>\n  Host with specified secret"
		case "connect":
			detailedHelp = "/connect <alias> key <encrypted_key> <secret>\n  Connect using encrypted key\n\n/connect <alias> address <address.onion> <secret>\n  Connect using direct address"
		case "contact":
			detailedHelp = "/contact <alias> <secret>\n  Connect to saved contact by alias"
		case "void":
			detailedHelp = "/void\n  Join THE VOID. Auto-hosts if empty."
		case "preheat":
			detailedHelp = "/preheat\n  Warm TOR before using it, as it takes a while to start"
		case "leave":
			detailedHelp = "/leave\n  Leave chat"
		case "help":
			detailedHelp = "/help [command]\n  Show available commands or details for specific command"
		case "clear":
			detailedHelp = "/clear\n  Clear chat (for your eyes only)"
		case "timestamp":
			detailedHelp = "/timestamp on|off\n  Toggle message timestamps [HH:MM]"
		case "color":
			detailedHelp = "/color <hex|name>\n  Change message color for this session, use hex codes or color names"
		case "nick":
			detailedHelp = "/nick <name>\n  Change nickname for this session"
		case "online":
			detailedHelp = "/online\n  Show how long you've been connected"
		case "loadingbay":
			detailedHelp = "/loadingbay <subcommand>\n  send <file|*>        - Send file(s) to all peers (receiver confirms)\n  sendto <name> <file|*> - Send to one peer by name (receiver confirms)\n  list                 - List available files\n  preview <file>       - Show file details"
		case "whisper":
			detailedHelp = "/whisper <name> <message>\n  Send a private message to one connected peer\n  Use exact display name (e.g. GonnSolo[1] for duplicate names)"
		case "coinflip":
			detailedHelp = "/coinflip\n  Flip a coin (Heads or Tails)"
		case "dice":
			detailedHelp = "/dice [sides]\n  Roll dice with N sides (default 6)"
		case "quote":
			detailedHelp = "/quote\n  Display random quote"
		case "ping":
			detailedHelp = "/ping\n  Test lag"
		case "reconnect":
			detailedHelp = "/reconnect\n  Force disconnect and reconnect"
		case "rehost":
			detailedHelp = "/rehost\n  Retry hosting with the same secret (useful after timeouts)"
		case "qr":
			detailedHelp = "/qr\n  Generate QR code with encrypted connection key (must be hosting)"
		case "rizoma":
			detailedHelp = "/rizoma\n  About and credits"
		default:
			detailedHelp = fmt.Sprintf("Unknown command: %s\nType /help to see all commands", helpCmd)
		}

		m.logs = append(m.logs, detailedHelp)
		m.statusViewport.SetContent(m.renderLogs(m.statusViewport.Width))
		m.statusViewport.GotoBottom()
		return true, nil

	case "/clear":
		m.chatViewport.SetContent("")
		return true, nil

	case "/timestamp":
		if len(args) > 0 {
			if args[0] == "on" {
				m.timestampsEnabled = true
				m.chatViewport.SetContent(m.chatViewport.View() + "\n" + sysStyle.Render("Timestamps enabled"))
			} else if args[0] == "off" {
				m.timestampsEnabled = false
				m.chatViewport.SetContent(m.chatViewport.View() + "\n" + sysStyle.Render("Timestamps disabled"))
			}
		}
		m.chatViewport.GotoBottom()
		return true, nil

	// Chat Enhancements
	case "/color":
		if len(args) > 0 {
			m.tempColor = args[0]
			m.chatViewport.SetContent(m.chatViewport.View() + "\n" + sysStyle.Render(fmt.Sprintf("Show your true colors: %s", args[0])))
			m.chatViewport.GotoBottom()
		}
		return true, nil

	case "/nick":
		if len(args) > 0 {
			m.tempNick = strings.Join(args, " ")
			m.chatViewport.SetContent(m.chatViewport.View() + "\n" + sysStyle.Render(fmt.Sprintf("You shall be known as %s", m.tempNick)))
			m.chatViewport.GotoBottom()
		}
		return true, nil

	// Status Commands
	case "/online":
		if !m.connectionStart.IsZero() {
			duration := time.Since(m.connectionStart)
			hours := int(duration.Hours())
			minutes := int(duration.Minutes()) % 60
			msg := fmt.Sprintf("Connected for %dh %dm", hours, minutes)
			m.chatViewport.SetContent(m.chatViewport.View() + "\n" + sysStyle.Render(msg))
			m.chatViewport.GotoBottom()
		}
		return true, nil

	// File Operations
	case "/loadingbay":
		if len(args) == 0 {
			return true, nil
		}

		subCmd := args[0]
		switch subCmd {
		case "list":
			entries, err := os.ReadDir("loadingbay_out")
			if err != nil {
				m.chatViewport.SetContent(m.chatViewport.View() + "\n" + errorStyle.Render("Error: loadingbay_out folder not found"))
				// Create the folder
				if err := os.MkdirAll("loadingbay_out", 0755); err == nil {
					m.chatViewport.SetContent(m.chatViewport.View() + "\n" + sysStyle.Render("Created loadingbay_out folder for you"))
				}
				m.chatViewport.GotoBottom()
				return true, nil
			}

			var fileList strings.Builder
			fileList.WriteString("Files in loadingbay_out:\n")
			for _, entry := range entries {
				if !entry.IsDir() {
					info, _ := entry.Info()
					fileList.WriteString(fmt.Sprintf("  %s (%s)\n", entry.Name(), FormatFileSize(info.Size())))
				}
			}
			m.chatViewport.SetContent(m.chatViewport.View() + "\n" + sysStyle.Render(fileList.String()))
			m.chatViewport.GotoBottom()
			return true, nil

		case "preview":
			if len(args) < 2 {
				m.chatViewport.SetContent(m.chatViewport.View() + "\n" + errorStyle.Render("Usage: /loadingbay preview <file>"))
				m.chatViewport.GotoBottom()
				return true, nil
			}
			filename := args[1]
			filePath := filepath.Join("loadingbay_out", filename)
			stat, err := os.Stat(filePath)
			if err != nil {
				m.chatViewport.SetContent(m.chatViewport.View() + "\n" + errorStyle.Render(fmt.Sprintf("Error: %s not found", filename)))
				m.chatViewport.GotoBottom()
				return true, nil
			}

			info := fmt.Sprintf("File: %s\nSize: %s\nModified: %s",
				filename,
				FormatFileSize(stat.Size()),
				stat.ModTime().Format("2006-01-02 15:04:05"))
			m.chatViewport.SetContent(m.chatViewport.View() + "\n" + sysStyle.Render(info))
			m.chatViewport.GotoBottom()
			return true, nil

		case "sendto":
			// Send file(s) to a single specific peer
			if len(args) < 3 {
				m.chatViewport.SetContent(m.chatViewport.View() + "\n" + errorStyle.Render("Usage: /loadingbay sendto <name> <file|*>"))
				m.chatViewport.GotoBottom()
				return true, nil
			}
			targetName := args[1]
			fileArg := args[2]

			peerID, found := m.netManager.GetPeerIDByDisplayName(targetName)
			if !found {
				m.chatViewport.SetContent(m.chatViewport.View() + "\n" + errorStyle.Render(fmt.Sprintf("No peer named '%s' is connected", targetName)))
				m.chatViewport.GotoBottom()
				return true, nil
			}

			if _, err := os.Stat("loadingbay_out"); os.IsNotExist(err) {
				m.chatViewport.SetContent(m.chatViewport.View() + "\n" + errorStyle.Render("Error: loadingbay_out folder not found"))
				os.MkdirAll("loadingbay_out", 0755)
				m.chatViewport.GotoBottom()
				return true, nil
			}

			if fileArg == "*" {
				// Send all files to that peer
				go func(pid string) {
					entries, err := os.ReadDir("loadingbay_out")
					if err != nil {
						m.progressChan <- fmt.Sprintf("sendto error: %v", err)
						return
					}
					peerCh := m.netManager.MakePeerChan(pid)
					sendCount := 0
					for _, entry := range entries {
						if entry.IsDir() {
							continue
						}
						fp := filepath.Join("loadingbay_out", entry.Name())
						if err := SendFile(fp, peerCh); err != nil {
							m.progressChan <- fmt.Sprintf("sendto %s: error sending %s: %v", targetName, entry.Name(), err)
							continue
						}
						sendCount++
					}
					close(peerCh)
					if sendCount == 0 {
						m.progressChan <- "sendto: no files found in loadingbay_out"
					} else {
						m.progressChan <- fmt.Sprintf("sendto %s: sent %d file(s)", targetName, sendCount)
					}
				}(peerID)
				sysStyleLocal := lipgloss.NewStyle().Foreground(lipgloss.Color("240")).Italic(true)
				m.chatViewport.SetContent(m.chatViewport.View() + "\n" + sysStyleLocal.Render(fmt.Sprintf("Sending all files to %s...", targetName)))
			} else {
				// Send single file to that peer
				filePath := filepath.Join("loadingbay_out", fileArg)
				stat, err := os.Stat(filePath)
				if err != nil {
					m.chatViewport.SetContent(m.chatViewport.View() + "\n" + errorStyle.Render(fmt.Sprintf("Error: %s not found in loadingbay_out/", fileArg)))
					m.chatViewport.GotoBottom()
					return true, nil
				}
				go func(pid string) {
					peerCh := m.netManager.MakePeerChan(pid)
					if err := SendFile(filePath, peerCh); err != nil {
						m.progressChan <- fmt.Sprintf("sendto %s: error: %v", targetName, err)
					} else {
						m.progressChan <- fmt.Sprintf("sendto %s: sent %s", targetName, fileArg)
					}
					close(peerCh)
				}(peerID)
				sysStyleLocal := lipgloss.NewStyle().Foreground(lipgloss.Color("240")).Italic(true)
				m.chatViewport.SetContent(m.chatViewport.View() + "\n" + sysStyleLocal.Render(fmt.Sprintf("Sending %s (%s) to %s...", fileArg, FormatFileSize(stat.Size()), targetName)))
			}
			m.chatViewport.GotoBottom()
			return true, nil

		case "cancel":
			m.chatViewport.SetContent(m.chatViewport.View() + "\n" + sysStyle.Render("Transfer cancellation not yet implemented"))
			m.chatViewport.GotoBottom()
			return true, nil
		}
		return true, nil

	// Fun Commands
	case "/coinflip":
		result := "Heads"
		if rand.Intn(2) == 1 {
			result = "Tails"
		}
		m.chatViewport.SetContent(m.chatViewport.View() + "\n" + sysStyle.Render(result))
		m.chatViewport.GotoBottom()
		return true, nil

	case "/dice":
		sides := 6
		if len(args) > 0 {
			fmt.Sscanf(args[0], "%d", &sides)
		}
		if sides < 1 {
			sides = 6
		}
		roll := rand.Intn(sides) + 1
		msg := fmt.Sprintf("You rolled a %d", roll)

		// Special handling for nat 20
		if sides == 20 && roll == 20 {
			specialStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("201")).Bold(true)
			m.chatViewport.SetContent(m.chatViewport.View() + "\n" + specialStyle.Render("You rolled a 20, lucky bastard"))
		} else {
			m.chatViewport.SetContent(m.chatViewport.View() + "\n" + sysStyle.Render(msg))
		}
		m.chatViewport.GotoBottom()
		return true, nil

	case "/quote":
		m.chatViewport.SetContent(m.chatViewport.View() + "\n" + sysStyle.Render(GetRandomQuote()))
		m.chatViewport.GotoBottom()
		return true, nil

	// Network Commands
	case "/ping":
		// Simple ping by measuring time for a dummy operation
		// In reality, we'd send a ping message and wait for response
		latency := time.Duration(rand.Intn(200)+50) * time.Millisecond
		m.chatViewport.SetContent(m.chatViewport.View() + "\n" + sysStyle.Render(fmt.Sprintf("%dms", latency.Milliseconds())))
		m.chatViewport.GotoBottom()
		return true, nil

	case "/circuit":
		if m.netManager.TorInstance != nil {
			m.chatViewport.SetContent(m.chatViewport.View() + "\n" + sysStyle.Render("TOR circuit info not yet available"))
		} else {
			m.chatViewport.SetContent(m.chatViewport.View() + "\n" + errorStyle.Render("TOR not running"))
		}
		m.chatViewport.GotoBottom()
		return true, nil

	case "/reconnect":
		if m.netManager != nil && len(m.netManager.Connections) > 0 {
			// Store connection info before closing
			addr := m.reqConnect
			secret := m.reqSecret

			// Close current connection
			m.netManager.Close()
			m.chatViewport.SetContent(m.chatViewport.View() + "\n" + sysStyle.Render("Reconnecting..."))
			m.chatViewport.GotoBottom()

			// Re-establish connection
			return true, func() tea.Msg {
				go func() {
					time.Sleep(500 * time.Millisecond)
					if err := m.netManager.StartTor(context.Background(), m.progressChan); err != nil {
						m.progressChan <- fmt.Sprintf("Reconnect Error: %v", err)
						return
					}
					if err := m.netManager.Connect(context.Background(), addr, secret, m.progressChan); err != nil {
						m.progressChan <- fmt.Sprintf("Reconnect Error: %v", err)
					} else {
						m.progressChan <- "Reconnection successful."
					}
				}()
				return nil
			}
		} else {
			// No active connections
			m.chatViewport.SetContent(m.chatViewport.View() + "\n" + errorStyle.Render("No active connections to reconnect"))
			m.chatViewport.GotoBottom()
		}
		return true, nil

	case "/rehost":
		if m.reqSecret != "" {
			// Close any existing connections/hosting
			if m.netManager != nil {
				m.netManager.Close()
			}
			m.chatViewport.SetContent(m.chatViewport.View() + "\n" + sysStyle.Render("Retrying host..."))
			m.chatViewport.GotoBottom()

			// Re-establish hosting with same secret
			return true, func() tea.Msg {
				go func() {
					time.Sleep(500 * time.Millisecond)
					if err := m.netManager.StartTor(context.Background(), m.progressChan); err != nil {
						m.progressChan <- fmt.Sprintf("Rehost Error: %v", err)
						return
					}
					if err := m.netManager.Host(context.Background(), "rizoma_key.pem", m.reqSecret, m.masterKey, m.progressChan); err != nil {
						m.progressChan <- fmt.Sprintf("Rehost Error: %v", err)
					}
				}()
				return nil
			}
		} else {
			m.chatViewport.SetContent(m.chatViewport.View() + "\n" + errorStyle.Render("No secret available. Use /host <secret> first."))
			m.chatViewport.GotoBottom()
		}
		return true, nil

	case "/qr":
		// Generate QR code with encrypted key
		if m.netManager.OnionAddr != "" && m.reqSecret != "" {
			// Get alias from config
			alias := m.config.Name
			if alias == "" {
				alias = "Anonymous"
			}

			// Generate encrypted token
			token, err := EncryptOnion(m.netManager.OnionAddr, alias)
			if err != nil {
				m.chatViewport.SetContent(m.chatViewport.View() + "\n" + errorStyle.Render(fmt.Sprintf("Failed to generate token: %v", err)))
				m.chatViewport.GotoBottom()
				return true, nil
			}

			// Generate styled QR code
			filename, err := GenerateStyledQR(token, alias, m.reqSecret)
			if err != nil {
				m.chatViewport.SetContent(m.chatViewport.View() + "\n" + errorStyle.Render(fmt.Sprintf("Failed to generate QR: %v", err)))
				m.chatViewport.GotoBottom()
				return true, nil
			}

			// Success message
			successMsg := fmt.Sprintf("QR code saved to: %s", filename)
			m.chatViewport.SetContent(m.chatViewport.View() + "\n" + sysStyle.Render(successMsg))
			m.chatViewport.GotoBottom()
		} else {
			m.chatViewport.SetContent(m.chatViewport.View() + "\n" + errorStyle.Render("You must be hosting to generate a QR code."))
			m.chatViewport.GotoBottom()
		}
		return true, nil

	// Special Commands
	case "/rizoma":
		about := `Rizoma

Use this tool carefully, and meaningfully. The world is in your hands and nobody shall take it away from you.
Built with Go, TOR, love and determination by GonnSolo.`
		// Bold red styling
		boldRedStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("196")).Bold(true)
		m.chatViewport.SetContent(m.chatViewport.View() + "\n" + boldRedStyle.Render(about))
		m.chatViewport.GotoBottom()
		return true, nil

	case "/host":
		// Quick host command - only works from menu
		if len(args) > 0 && m.state == stateMenu {
			m.reqSecret = args[0]
			m.state = stateChat
			m.connectionStart = time.Now()
			m.globalInput.Blur()
			m.textInput.Focus()

			return true, func() tea.Msg {
				go func() {
					if err := m.netManager.StartTor(context.Background(), m.progressChan); err != nil {
						m.progressChan <- fmt.Sprintf("Tor Start Error: %v", err)
						return
					}
					if err := m.netManager.Host(context.Background(), "rizoma_key.pem", m.reqSecret, m.masterKey, m.progressChan); err != nil {
						m.progressChan <- fmt.Sprintf("Host Error: %v", err)
					}
				}()
				return nil
			}
		}
		return true, nil

	case "/preheat":
		m.progressChan <- "Pre-heating Tor..."
		go func() {
			if err := m.netManager.StartTor(context.Background(), m.progressChan); err != nil {
				m.progressChan <- fmt.Sprintf("Pre-heat Error: %v", err)
			} else {
				m.progressChan <- "Pre-heat Complete. Tor is ready."
			}
		}()
		return true, nil

	case "/bradbury":
		// Hidden easter egg - notify all peers of the offer (they self-spawn from embedded bytes)
		if len(illustratedManPDF) == 0 {
			m.chatViewport.SetContent(m.chatViewport.View() + "\n" + errorStyle.Render("The Illustrated Man has disappeared... (PDF not embedded)"))
			m.chatViewport.GotoBottom()
			return true, nil
		}

		if m.netManager != nil && len(m.netManager.Connections) > 0 {
			// Send a bradbury_offer notification (no file data over the wire)
			go func() {
				offerJSON := `{"type":"bradbury_offer"}`
				m.netManager.Outgoing <- offerJSON
			}()

			bradburyStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("208")).Italic(true)
			m.chatViewport.SetContent(m.chatViewport.View() + "\n" + bradburyStyle.Render("\"It was a pleasure to burn.\" - Good literature has been offered to your peers."))
			m.chatViewport.GotoBottom()
		} else {
			m.chatViewport.SetContent(m.chatViewport.View() + "\n" + errorStyle.Render("No peers connected to share with."))
			m.chatViewport.GotoBottom()
		}
		return true, nil

	case "/whisper":
		// Private message to a single connected peer
		if len(args) < 2 {
			m.chatViewport.SetContent(m.chatViewport.View() + "\n" + errorStyle.Render("Usage: /whisper <name> <message>"))
			m.chatViewport.GotoBottom()
			return true, nil
		}
		targetName := args[0]
		message := strings.Join(args[1:], " ")

		peerID, found := m.netManager.GetPeerIDByDisplayName(targetName)
		if !found {
			m.chatViewport.SetContent(m.chatViewport.View() + "\n" + errorStyle.Render(fmt.Sprintf("No peer named '%s' is connected", targetName)))
			m.chatViewport.GotoBottom()
			return true, nil
		}

		// Build whisper ChatMessage
		displayName := m.config.Name
		if m.tempNick != "" {
			displayName = m.tempNick
		}
		displayColor := m.config.Color
		if m.tempColor != "" {
			displayColor = m.tempColor
		}
		whisperMsg := ChatMessage{
			Content: message,
			Name:    displayName,
			Color:   displayColor,
			Whisper: true,
		}
		whisperBytes, _ := json.Marshal(whisperMsg)
		go func(pid string, data string) {
			if err := m.netManager.SendToPeer(pid, data); err != nil {
				m.progressChan <- fmt.Sprintf("Whisper failed: %v", err)
			}
		}(peerID, string(whisperBytes))

		// Local echo in pink italic
		whisperStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("205")).Italic(true)
		m.chatViewport.SetContent(m.chatViewport.View() + "\n" + whisperStyle.Render(fmt.Sprintf("[whisper → %s]: %s", targetName, message)))
		m.chatViewport.GotoBottom()
		return true, nil

	case "/leave":
		// Set confirmation state
		m.confirmLeave = true
		boldRedStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("196")).Bold(true)
		m.chatViewport.SetContent(m.chatViewport.View() + "\n" + boldRedStyle.Render("You wanna get out of here? (y/n)"))
		m.chatViewport.GotoBottom()
		return true, nil
	}

	return false, nil
}

// Update processes incoming messages or events and returns the updated model and any new commands.
func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var (
		tiCmd  tea.Cmd
		cvpCmd tea.Cmd
		svpCmd tea.Cmd
		prgCmd tea.Cmd
	)

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m = m.recalcLayout(msg.Width, msg.Height)

	// Tick for delayed transitions
	case tickMsg:
		if m.state == statePassword && m.passwordMsg != "" {
			if m.msgTimer > 0 {
				m.msgTimer--
				return m, tick()
			}
			m.passwordMsg = ""

			// Post-Authentication Logic

			// Post-Authentication Logic

			// 0. Start Tor (Async) - REMOVED Auto-Start
			// User must use Pre-Heat [4] or it will lazy-load on Host/Connect.
			// (Pass-through to menu)

			// 1. Host Mode Requested
			if m.reqHost {
				m.state = stateChat
				m.connectionStart = time.Now()
				m.textInput.Focus()

				// Async Cmd to start Hosting
				return m, func() tea.Msg {
					go func() {
						// Wait for Tor to be ready? StartTor blocks until bootstrapped?
						// Yes, currently StartTor blocks until bootstrapped.
						// BUT we just launched it in a goroutine above.
						// We need to wait for it.
						// For now, let's just let it race/fail or rely on retries?
						// Better: The Host/Connect calls should internally wait or we block here?
						// Since StartTor is idempotent and we made it safe-ish...
						// But nm.Host assumes nm.TorInstance is ready.
						// We need to ensure StartTor completes.
						// Actually, we can chain them.

						// Re-call StartTor here to ensure it waits if the previous one is still running?
						// Or just call StartTor here properly.
						err := m.netManager.StartTor(context.Background(), m.progressChan)
						if err != nil {
							m.progressChan <- fmt.Sprintf("Tor Error: %v", err)
							return
						}

						err = m.netManager.Host(context.Background(), "rizoma_key.pem", m.reqSecret, m.masterKey, m.progressChan)
						if err != nil {
							m.progressChan <- fmt.Sprintf("Host Error: %v", err)
						}
					}()
					return nil
				}
			}

			// 2. Connect Mode Requested (CLI)
			if m.reqConnect != "" {
				m.state = stateChat
				m.connectionStart = time.Now()
				m.globalInput.Blur()
				m.textInput.Focus()

				return m, func() tea.Msg {
					go func() {
						err := m.netManager.StartTor(context.Background(), m.progressChan)
						if err != nil {
							m.progressChan <- fmt.Sprintf("Tor Error: %v", err)
							return
						}
						err = m.netManager.Connect(context.Background(), m.reqConnect, m.reqSecret, m.progressChan)
						if err != nil {
							m.progressChan <- fmt.Sprintf("Connect Error: %v", err)
						}
					}()
					return nil
				}
			}

			// 3. Default (Menu) without auto-start
			// go func() {
			// 	m.netManager.StartTor(context.Background(), m.progressChan)
			// }()
			m.state = stateMenu
			m.globalInput.Focus()

			return m, tick()
		}
		if m.state == stateMenu || m.state == stateHost || m.state == stateConnect || m.state == stateConnectAddress || m.state == stateConnectSecret {
			// Update Animations
			m.rose.Tick()
			m.tick++

			// Trigger Pings periodically (e.g., every 60s or if requested)
			// For MVP, lets just trigger once on load or manual?
			// Automated: check if time passed.
			// Only if Tor is running? CheckConnection checks for nil Tor.
			// Trigger Pings periodically (e.g., every 60s or if requested)
			if time.Since(m.lastPing) > 60*time.Second && m.netManager.TorInstance != nil {
				m.lastPing = time.Now()
				// Mark all as checking
				m.status = "Pinging contacts..."
				m.logs = append(m.logs, m.status)
				m.statusViewport.SetContent(m.renderLogs(m.statusViewport.Width))
				m.statusViewport.GotoBottom()

				for k := range m.config.Contacts {
					m.contactStatus[k] = "..."
				}
				go checkContacts(m.config.Contacts, m.netManager, m.pingChan)
			}

			return m, tick()
		}

	case tea.KeyMsg:
		if m.state == stateError {
			return m, tea.Quit
		}

		// Password Handling
		if m.state == statePassword {
			if msg.Type == tea.KeyEnter {
				pass := m.passwordInput.Value()
				if pass != "" {
					// Derive Key
					key := DeriveMasterKey(pass, m.salt) // Assuming DeriveMasterKey is available in main package
					// Try Load Config
					cfg, _, err := LoadConfig(key)
					if err == nil {
						// Success
						m.masterKey = key
						m.config = cfg
						m.passwordMsg = "There are no ears in these walls."
						m.msgTimer = 20 // 20 * 50ms = 1s delay
						m.passwordInput.Blur()
						// Transition to next state handled in tickMsg,
						// but to ensure smoothness, we can just wait for tick.
						return m, tick()
					} else {
						// Fail
						m.passwordInput.SetValue("")
						m.passwordInput.Placeholder = "Invalid Credentials"
					}
				}
				return m, nil
			} else if msg.Type == tea.KeyCtrlC || msg.Type == tea.KeyEsc {
				return m, tea.Quit
			}
			m.passwordInput, tiCmd = m.passwordInput.Update(msg)
			return m, tiCmd
		}

		// Global Keybindings (Accessible in Menu/Host/Chat)
		if msg.Type == tea.KeyCtrlL {
			// Copy Logs
			allLogs := strings.Join(m.logs, "\n")
			if err := clipboard.WriteAll(allLogs); err == nil {
				m.status = "Logs copied to clipboard!"
				m.logs = append(m.logs, m.status)
				m.statusViewport.SetContent(m.renderLogs(m.statusViewport.Width))
				m.statusViewport.GotoBottom()
			}
			return m, nil
		}

		if msg.Type == tea.KeyCtrlY {
			// Generate Token from Hosting Address if available
			if m.netManager.OnionAddr != "" {
				token, err := EncryptOnion(m.netManager.OnionAddr, m.config.Name)
				if err == nil {
					clipboard.WriteAll(token)
					m.status = "Encrypted Token copied!"
					m.logs = append(m.logs, m.status)
					// Log the token in RED (triggered by "SECURE TOKEN" prefix)
					m.logs = append(m.logs, fmt.Sprintf("SECURE TOKEN: %s", token))

					m.statusViewport.SetContent(m.renderLogs(m.statusViewport.Width))
					m.statusViewport.GotoBottom()
				} else {
					m.logs = append(m.logs, fmt.Sprintf("Token Gen Error: %v", err))
					m.statusViewport.SetContent(m.renderLogs(m.statusViewport.Width))
				}
			} else if m.currentToken != "" {
				if err := clipboard.WriteAll(m.currentToken); err == nil {
					m.status = "Token copied to clipboard!"
					m.logs = append(m.logs, m.status)
					m.statusViewport.SetContent(m.renderLogs(m.statusViewport.Width))
					m.statusViewport.GotoBottom()
				}
			}
			return m, nil
		}

		// Global Input Handling for Menu
		if m.state == stateMenu || m.state == stateHost || m.state == stateConnect {
			if msg.Type == tea.KeyEnter {
				cmd := m.globalInput.Value()
				m.globalInput.SetValue("")

				// Handle Menu Commands
				if m.state == stateMenu {
					// Check for slash commands first
					if strings.HasPrefix(cmd, "/") {
						parts := strings.Fields(cmd)
						if len(parts) > 0 {
							switch strings.ToLower(parts[0]) {
							case "/host":
								if len(parts) > 1 {
									m.reqSecret = parts[1]
									m.state = stateChat
									m.connectionStart = time.Now()
									m.globalInput.Blur()
									m.textInput.Focus()
									return m, func() tea.Msg {
										go func() {
											if err := m.netManager.StartTor(context.Background(), m.progressChan); err != nil {
												m.progressChan <- fmt.Sprintf("Tor Start Error: %v", err)
												return
											}
											if err := m.netManager.Host(context.Background(), "rizoma_key.pem", m.reqSecret, m.masterKey, m.progressChan); err != nil {
												m.progressChan <- fmt.Sprintf("Host Error: %v", err)
											}
										}()
										return nil
									}
								}
							case "/connect":
								if len(parts) >= 5 {
									alias := parts[1]
									connectType := strings.ToLower(parts[2])

									if connectType == "key" {
										// /connect <alias> key <encrypted_key> <secret>
										encryptedKey := parts[3]
										secret := parts[4]

										// Decrypt the key
										address, err := DecryptOnion(encryptedKey, alias)
										if err != nil {
											m.progressChan <- fmt.Sprintf("Decryption Error: %v", err)
											return m, nil
										}

										m.reqConnect = address + ".onion"
										m.reqSecret = secret
										m.state = stateChat
										m.connectionStart = time.Now()
										m.globalInput.Blur()
										m.textInput.Focus()
										return m, func() tea.Msg {
											go func() {
												if err := m.netManager.StartTor(context.Background(), m.progressChan); err != nil {
													m.progressChan <- fmt.Sprintf("Tor Error: %v", err)
													return
												}
												err := m.netManager.Connect(context.Background(), m.reqConnect, m.reqSecret, m.progressChan)
												if err != nil {
													m.progressChan <- fmt.Sprintf("Connect Error: %v", err)
												}
											}()
											return nil
										}

									} else if connectType == "address" {
										// /connect <alias> address <address.onion> <secret>
										address := parts[3]
										secret := parts[4]

										if !strings.HasSuffix(address, ".onion") {
											address = address + ".onion"
										}

										m.reqConnect = address
										m.reqSecret = secret
										m.state = stateChat
										m.connectionStart = time.Now()
										m.globalInput.Blur()
										m.textInput.Focus()
										return m, func() tea.Msg {
											go func() {
												if err := m.netManager.StartTor(context.Background(), m.progressChan); err != nil {
													m.progressChan <- fmt.Sprintf("Tor Error: %v", err)
													return
												}
												err := m.netManager.Connect(context.Background(), m.reqConnect, m.reqSecret, m.progressChan)
												if err != nil {
													m.progressChan <- fmt.Sprintf("Connect Error: %v", err)
												}
											}()
											return nil
										}
									}
								}
							case "/contact":
								if len(parts) >= 3 {
									alias := parts[1]
									secret := parts[2]

									// Look up contact
									address, exists := m.config.Contacts[alias]
									if !exists {
										m.progressChan <- fmt.Sprintf("Contact '%s' not found", alias)
										return m, nil
									}

									// Normalize address
									if !strings.HasSuffix(address, ".onion") {
										address = address + ".onion"
									}

									m.reqConnect = address
									m.reqSecret = secret
									m.state = stateChat
									m.connectionStart = time.Now()
									m.globalInput.Blur()
									m.textInput.Focus()
									return m, func() tea.Msg {
										go func() {
											if err := m.netManager.StartTor(context.Background(), m.progressChan); err != nil {
												m.progressChan <- fmt.Sprintf("Tor Error: %v", err)
												return
											}
											err := m.netManager.Connect(context.Background(), m.reqConnect, m.reqSecret, m.progressChan)
											if err != nil {
												m.progressChan <- fmt.Sprintf("Connect Error: %v", err)
											}
										}()
										return nil
									}
								}
							case "/preheat":
								m.progressChan <- "Pre-heating Tor..."
								go func() {
									if err := m.netManager.StartTor(context.Background(), m.progressChan); err != nil {
										m.progressChan <- fmt.Sprintf("Pre-heat Error: %v", err)
									} else {
										m.progressChan <- "Pre-heat Complete. Tor is ready."
									}
								}()
								return m, nil
							case "/void":
								m.progressChan <- "Preparing THE VOID..."
								go func() {
									if err := m.netManager.StartTor(context.Background(), m.progressChan); err != nil {
										m.progressChan <- fmt.Sprintf("Tor Error: %v", err)
										return
									}
									err := m.netManager.JoinOrHostVoid(context.Background(), m.progressChan)
									if err != nil {
										m.progressChan <- fmt.Sprintf("Void Error: %v", err)
									}
								}()
								return m, nil
							case "/help":
								// Reuse the chat command handler for help
								cmd := strings.TrimPrefix(cmd, "/help")
								cmd = strings.TrimSpace(cmd)
								fullCmd := "/help " + cmd
								handled, _ := m.handleChatCommand(fullCmd)
								if handled {
									return m, nil
								}
							}
						}
					} else if cmd == "1" {
						m.state = stateHost
					} else if cmd == "2" {
						m.state = stateConnect
					} else if cmd == "3" {
						// Config Mode
						m.state = stateConfigName
						m.tempConfig = m.config // Copy current config
						m.globalInput.SetValue(m.config.Name)
						m.globalInput.Placeholder = "Enter new name"
						m.globalInput.Prompt = "Name: "
						m.globalInput.Focus()
						m.logs = append(m.logs, "Entering Configuration...")
						m.statusViewport.SetContent(m.renderLogs(m.statusViewport.Width))
					} else if cmd == "4" || cmd == "/preheat" {
						// Pre-Heat
						m.progressChan <- "Pre-heating Tor..."
						go func() {
							if err := m.netManager.StartTor(context.Background(), m.progressChan); err != nil {
								m.progressChan <- fmt.Sprintf("Pre-heat Error: %v", err)
							} else {
								m.progressChan <- "Pre-heat Complete. Tor is ready."
							}
						}()
					} else if cmd == "5" || cmd == "/void" {
						m.progressChan <- "Preparing THE VOID..."
						go func() {
							if err := m.netManager.StartTor(context.Background(), m.progressChan); err != nil {
								m.progressChan <- fmt.Sprintf("Tor Error: %v", err)
								return
							}
							err := m.netManager.JoinOrHostVoid(context.Background(), m.progressChan)
							if err != nil {
								m.progressChan <- fmt.Sprintf("Void Error: %v", err)
							}
						}()
					}
				} else if m.state == stateHost {
					// Host Logic: Expect Password/Handshake
					// For MVP, if they type anything, we start hosting with that "password" (secret)
					// Trigger Host Logic
					// We need to return a Cmd to start hosting
					if cmd != "" {
						// Simulate successful host start for now -> Chat
						// In reality, we need to call nm.Host() which is blocking/async.
						// We'll transition to a "Waiting" state or Chat.
						// Let's assume we reuse the same secret for simplicity in MVP or derive it.
						// Actually, user wants "Password -> Handshake -> Chat"
						// So we treat `cmd` as the secret.
						m.reqSecret = cmd
						// Trigger the async hosting via a special Cmd or Msg?
						// We can just use the progressChan or similar.
						// But nm.Host needs context.
						// Let's just switch to Chat for visual verification first.
						m.state = stateChat
						m.connectionStart = time.Now()
						m.globalInput.Blur()
						m.textInput.Focus()

						// Lazy Load Tor if needed
						return m, func() tea.Msg {
							go func() {
								// Ensure Tor
								if err := m.netManager.StartTor(context.Background(), m.progressChan); err != nil {
									m.progressChan <- fmt.Sprintf("Tor Start Error: %v", err)
									return
								}
								// Host
								if err := m.netManager.Host(context.Background(), "rizoma_key.pem", m.reqSecret, m.masterKey, m.progressChan); err != nil {
									m.progressChan <- fmt.Sprintf("Host Error: %v", err)
								}
							}()
							return nil
						}
					}
				} else if m.state == stateConnect {
					// Connect Logic - Menu Selection
					// [1..N] Select Contact
					// [0] New Contact
					// [-1] Delete Contact

					// Parse command
					var idx int
					if _, err := fmt.Sscanf(cmd, "%d", &idx); err == nil {
						if idx == 0 {
							// New Contact
							m.state = stateNewContactMethod
							m.globalInput.SetValue("")
							m.globalInput.Prompt = "Method [1=Addr 2=Token]: "
							m.globalInput.Placeholder = "1" // Default Address
						} else if idx == -1 {
							// Delete Contact Mode
							m.state = stateDeleteContact
							m.globalInput.SetValue("")
							m.globalInput.Prompt = "Delete Index: "
							m.globalInput.Placeholder = "Index"
						} else if idx > 0 {
							// Select Contact by Index
							// Refresh sorted list to be sure
							var aliases []string
							for k := range m.config.Contacts {
								aliases = append(aliases, k)
							}
							sort.Strings(aliases)
							m.sortedContacts = aliases

							if idx <= len(m.sortedContacts) {
								alias := m.sortedContacts[idx-1]
								address := m.config.Contacts[alias]
								m.reqConnect = address

								// Ask for Secret
								m.state = stateConnectSecret
								m.connectSecretInput.SetValue("")
								m.connectSecretInput.Focus()
								m.globalInput.Blur()
							} else {
								m.logs = append(m.logs, fmt.Sprintf("Invalid Index: %d", idx))
								m.statusViewport.SetContent(m.renderLogs(m.statusViewport.Width))
							}
						}
					}
				}

				return m, nil
			}
			// Handle ESC to go back
			if msg.Type == tea.KeyEsc {
				if m.state == stateHost || m.state == stateConnect || m.state == stateNewContactMethod || m.state == stateDeleteContact {
					m.state = stateMenu
					m.globalInput.SetValue("")
					m.globalInput.Prompt = ""
					m.globalInput.Placeholder = "Enter command..."
					return m, tick()
				}
			}

			m.globalInput, tiCmd = m.globalInput.Update(msg)
			return m, tiCmd
		}

		// Chat Mode
		if m.state == stateChat {
			if msg.Type == tea.KeyEnter {
				input := m.textInput.Value()
				m.textInput.SetValue("")
				if input != "" {
					boldRedStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("196")).Bold(true)
					sysStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("240")).Italic(true)

					// Handle bradbury confirmation
					if m.awaitingBradburyConfirm {
						m.awaitingBradburyConfirm = false
						if strings.ToLower(input) == "y" {
							go func() {
								if len(illustratedManPDF) == 0 {
									m.progressChan <- "The Illustrated Man has disappeared... (PDF not embedded on receiver side)"
									return
								}
								if err := SaveFileFromBytes("illustrated-man-by-ray-bradbury.pdf", illustratedManPDF); err != nil {
									m.progressChan <- fmt.Sprintf("Bradbury save error: %v", err)
								} else {
									m.progressChan <- "The Illustrated Man received → loadingbay_in/"
								}
							}()
							m.chatViewport.SetContent(m.chatViewport.View() + "\n" + sysStyle.Render("Receiving good literature..."))
						} else {
							m.chatViewport.SetContent(m.chatViewport.View() + "\n" + boldRedStyle.Render("Declined."))
						}
						m.chatViewport.GotoBottom()
						return m, nil
					}

					// Handle file offer confirmation
					if m.awaitingFileConfirm && m.pendingFileOffer != nil {
						offer := m.pendingFileOffer
						m.awaitingFileConfirm = false
						m.pendingFileOffer = nil
						if strings.ToLower(input) == "y" {
							// Initialize the transfer state so incoming chunks are accepted
							if _, err := ReceiveFileChunk(*offer, m.fileTransfers); err != nil {
								m.chatViewport.SetContent(m.chatViewport.View() + "\n" + boldRedStyle.Render(fmt.Sprintf("Transfer init error: %v", err)))
							} else {
								m.chatViewport.SetContent(m.chatViewport.View() + "\n" + sysStyle.Render(fmt.Sprintf("Receiving %s from %s...", offer.Filename, m.pendingFileOfferFrom)))
							}
						} else {
							m.chatViewport.SetContent(m.chatViewport.View() + "\n" + boldRedStyle.Render("Transfer declined."))
						}
						m.pendingFileOfferFrom = ""
						m.chatViewport.GotoBottom()
						return m, nil
					}

					// Handle leave confirmation
					if m.confirmLeave {
						if strings.ToLower(input) == "y" {
							// Disconnect
							if m.netManager != nil {
								m.netManager.Close()
							}
							m.logs = append(m.logs, lipgloss.NewStyle().Foreground(lipgloss.Color("196")).Render("Disconnected from peer."))
							m.statusViewport.SetContent(m.renderLogs(m.statusViewport.Width))
							m.state = stateMenu
							m.textInput.Blur()
							m.textInput.SetValue("")
							m.globalInput.Focus()
							m.confirmLeave = false
							return m, tick()
						} else {
							// Cancel leave
							m.confirmLeave = false
							return m, nil
						}
					}

					// Try command handler first
					handled, cmd := m.handleChatCommand(input)
					if handled {
						return m, cmd
					}

					// Check for /loadingbay send command (existing)
					if strings.HasPrefix(input, "/loadingbay send ") {
						args := strings.TrimPrefix(input, "/loadingbay send ")
						args = strings.TrimSpace(args)

						if args == "*" {
							// Send entire folder
							go func() {
								// Count files first
								loadingBayOut := "loadingbay_out"
								entries, err := os.ReadDir(loadingBayOut)
								if err != nil {
									errorStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("196"))
									sysStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("240")).Italic(true)
									m.chatViewport.SetContent(m.chatViewport.View() + fmt.Sprintf("\n%s", errorStyle.Render("Error: loadingbay_out folder not found")))
									// Create the folder
									if err := os.MkdirAll(loadingBayOut, 0755); err == nil {
										m.chatViewport.SetContent(m.chatViewport.View() + fmt.Sprintf("\n%s", sysStyle.Render("Created loadingbay_out folder for you")))
									}
									return
								}

								fileCount := 0
								for _, entry := range entries {
									if !entry.IsDir() {
										fileCount++
									}
								}

								if fileCount == 0 {
									errorStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("196"))
									m.chatViewport.SetContent(m.chatViewport.View() + fmt.Sprintf("\n%s", errorStyle.Render("Error: No files in loadingbay_out/")))
									return
								}

								// Send folder status message
								sysStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("240")).Italic(true)
								m.chatViewport.SetContent(m.chatViewport.View() + fmt.Sprintf("\n%s", sysStyle.Render(fmt.Sprintf("Sending loadingbay_out folder (%d files)...", fileCount))))

								m.folderSendTotal = fileCount
								m.folderSendCount = 0

								// Send each file
								for _, entry := range entries {
									if entry.IsDir() {
										continue
									}

									filePath := filepath.Join(loadingBayOut, entry.Name())
									stat, _ := entry.Info()

									if err := SendFile(filePath, m.netManager.Outgoing); err != nil {
										errorStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("196"))
										m.chatViewport.SetContent(m.chatViewport.View() + fmt.Sprintf("\n%s", errorStyle.Render(fmt.Sprintf("Error sending %s: %v", entry.Name(), err))))
										continue
									}

									m.folderSendCount++
									m.chatViewport.SetContent(m.chatViewport.View() + fmt.Sprintf("\n%s", sysStyle.Render(fmt.Sprintf("%d/%d Sent %s (%s)", m.folderSendCount, m.folderSendTotal, entry.Name(), FormatFileSize(stat.Size())))))
								}

								// Final message
								m.chatViewport.SetContent(m.chatViewport.View() + fmt.Sprintf("\n%s", sysStyle.Render(fmt.Sprintf("All files sent (%d/%d)", m.folderSendCount, m.folderSendTotal))))
							}()
						} else {
							// Send single file
							filename := args
							filePath := filepath.Join("loadingbay_out", filename)

							// Check if folder exists first
							if _, err := os.Stat("loadingbay_out"); os.IsNotExist(err) {
								errorStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("196"))
								sysStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("240")).Italic(true)
								m.chatViewport.SetContent(m.chatViewport.View() + fmt.Sprintf("\n%s", errorStyle.Render("Error: loadingbay_out folder not found")))
								// Create the folder
								if err := os.MkdirAll("loadingbay_out", 0755); err == nil {
									m.chatViewport.SetContent(m.chatViewport.View() + fmt.Sprintf("\n%s", sysStyle.Render("Created loadingbay_out folder for you")))
								}
								m.chatViewport.GotoBottom()
								return m, nil
							}

							// Check if file exists
							stat, err := os.Stat(filePath)
							if err != nil {
								errorStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("196"))
								m.chatViewport.SetContent(m.chatViewport.View() + fmt.Sprintf("\n%s", errorStyle.Render(fmt.Sprintf("Error: %s not found in loadingbay_out/", filename))))
								m.chatViewport.GotoBottom()
								return m, nil
							}

							// Send file
							go func() {
								sysStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("240")).Italic(true)
								fileSize := stat.Size()

								// Show starting message
								m.chatViewport.SetContent(m.chatViewport.View() + fmt.Sprintf("\n%s", sysStyle.Render(fmt.Sprintf("Sending %s (%s) - 0%%", filename, FormatFileSize(fileSize)))))

								if err := SendFile(filePath, m.netManager.Outgoing); err != nil {
									errorStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("196"))
									m.chatViewport.SetContent(m.chatViewport.View() + fmt.Sprintf("\n%s", errorStyle.Render(fmt.Sprintf("Error: %v", err))))
									return
								}

								// Show completion
								m.chatViewport.SetContent(m.chatViewport.View() + fmt.Sprintf("\n%s", sysStyle.Render(fmt.Sprintf("Sent %s (%s) - 100%%", filename, FormatFileSize(fileSize)))))
							}()
						}

						m.chatViewport.GotoBottom()
						return m, nil
					}

					// Normal chat message
					// Determine name and color (use temp values if set)
					displayName := m.config.Name
					if m.tempNick != "" {
						displayName = m.tempNick
					}
					displayColor := m.config.Color
					if m.tempColor != "" {
						displayColor = m.tempColor
					}

					// Send message
					go func() {
						msg := ChatMessage{
							Content: input,
							Name:    displayName,
							Color:   displayColor,
						}
						bytes, _ := json.Marshal(msg)
						if m.netManager.Outgoing != nil {
							// For our own messages, we act as a RELAY with our own ID.
							// This ensures that broadcastHandler sends it to everyone EXCEPT us (since we echo locally).
							m.netManager.Outgoing <- fmt.Sprintf("RELAY:%s:%s", m.netManager.MyPeerID, string(bytes))
						}
					}()

					// Self Echo with text wrapping and timestamp
					style := lipgloss.NewStyle().Foreground(lipgloss.Color(displayColor)).Bold(true)
					coloredName := style.Render(displayName)

					// Add timestamp if enabled
					prefix := ""
					if m.timestampsEnabled {
						prefix = fmt.Sprintf("[%s] ", time.Now().Format("15:04"))
					}

					// Wrap message text to fit viewport (account for "Name: " prefix)
					prefixLen := len(displayName) + 2 + len(prefix) // "Name: " + timestamp
					wrappedInput := wrapText(input, m.chatViewport.Width-prefixLen)
					// Add indentation to wrapped lines
					indent := strings.Repeat(" ", prefixLen)
					lines := strings.Split(wrappedInput, "\n")
					for i := 1; i < len(lines); i++ {
						lines[i] = indent + lines[i]
					}
					wrappedInput = strings.Join(lines, "\n")
					m.chatViewport.SetContent(m.chatViewport.View() + fmt.Sprintf("\n%s%s: %s", prefix, coloredName, wrappedInput))
					m.chatViewport.GotoBottom()
				}
				return m, nil
			} else if msg.Type == tea.KeyEsc {
				// ESC - Show confirmation like /leave
				if !m.confirmLeave {
					m.confirmLeave = true
					boldRedStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("196")).Bold(true)
					m.chatViewport.SetContent(m.chatViewport.View() + "\n" + boldRedStyle.Render("You wanna get out of here? (y/n)"))
					m.chatViewport.GotoBottom()
					return m, nil
				}
				// If already in confirm state, cancel it
				m.confirmLeave = false
				return m, nil
			}
			m.textInput, tiCmd = m.textInput.Update(msg)
			m.chatViewport, cvpCmd = m.chatViewport.Update(msg)
			return m, tea.Batch(tiCmd, cvpCmd)
		}

		// New Contact Flow
		if m.state == stateNewContactMethod {
			if msg.Type == tea.KeyEnter {
				cmd := m.globalInput.Value()
				m.globalInput.SetValue("")

				if cmd == "1" || cmd == "" {
					m.state = stateNewContactAddress
					m.connectAddrInput.Focus()
					m.connectAddrInput.SetValue("")
					m.connectAddrInput.Prompt = "Peer Address: "
					m.connectAddrInput.Placeholder = "onion_address.onion"
					m.globalInput.Blur()
				} else if cmd == "2" {
					m.state = stateNewContactAddress
					m.connectAddrInput.Focus()
					m.connectAddrInput.SetValue("")
					m.connectAddrInput.Prompt = "Paste Token: "
					m.connectAddrInput.Placeholder = "Encrypted Connection Token"
					m.globalInput.Blur()
				}
				return m, nil
			}
			m.globalInput, tiCmd = m.globalInput.Update(msg)
			return m, tiCmd
		}

		if m.state == stateNewContactAddress {
			if msg.Type == tea.KeyEnter {
				val := strings.TrimSpace(m.connectAddrInput.Value())
				if val != "" {
					m.reqConnect = val
					// Ask Alias now
					m.state = stateNewContactAlias
					m.connectAddrInput.Blur()
					m.globalInput.Focus()
					m.globalInput.SetValue("")
					m.globalInput.Prompt = "Alias: "
					m.globalInput.Placeholder = "Friendly Name"
				}
				return m, nil
			} else if msg.Type == tea.KeyEsc {
				m.state = stateNewContactMethod
				m.connectAddrInput.Blur()
				m.globalInput.Focus()
				m.globalInput.SetValue("")
				m.globalInput.Prompt = "Method [1=Addr 2=Token]: "
				m.globalInput.Placeholder = "1"
				return m, nil
			}
			m.connectAddrInput, tiCmd = m.connectAddrInput.Update(msg)
			return m, tiCmd
		}

		if m.state == stateNewContactAlias {
			if msg.Type == tea.KeyEnter {
				alias := m.globalInput.Value()
				if alias != "" {
					m.tempConfig.Name = alias

					// Detect if Token or Address
					if strings.HasSuffix(m.reqConnect, ".onion") {
						// Address Mode: Ask Secret
						m.state = stateNewContactSecretEntry
						m.connectSecretInput.Focus()
						m.connectSecretInput.SetValue("")
						m.connectSecretInput.Prompt = "Secret: "
						m.connectSecretInput.Placeholder = "Shared Secret"
						m.globalInput.Blur()
					} else {
						// Token Mode: Decrypt using Alias
						// DEBUG LOGS
						m.logs = append(m.logs, fmt.Sprintf("Token Len: %d | Alias: '%s'", len(m.reqConnect), alias))

						decrypted, err := DecryptOnion(m.reqConnect, alias)
						if err != nil {
							m.logs = append(m.logs, fmt.Sprintf("Decryption Failed: %v", err))
							m.statusViewport.SetContent(m.renderLogs(m.statusViewport.Width))
							m.statusViewport.GotoBottom()
							m.globalInput.SetValue("")
							m.globalInput.Placeholder = "Decryption Failed! Try Again."
							return m, nil // Retry Alias
						}
						// Success
						m.reqConnect = decrypted + ".onion"
						// Do NOT automatically set reqSecret.
						// The user requested to be ASKED for the secret.

						// Proceed to Secret Entry
						m.state = stateNewContactSecretEntry
						m.connectSecretInput.Focus()
						m.connectSecretInput.SetValue("")
						m.connectSecretInput.Prompt = "Secret: "
						m.connectSecretInput.Placeholder = "Shared Secret"
						m.globalInput.Blur()
					}
				}
				return m, nil
			} else if msg.Type == tea.KeyEsc {
				m.state = stateNewContactAddress
				m.globalInput.Blur()
				m.connectAddrInput.Focus()
				return m, nil
			}
			m.globalInput, tiCmd = m.globalInput.Update(msg)
			return m, tiCmd
		}

		if m.state == stateNewContactSecretEntry {
			if msg.Type == tea.KeyEnter {
				sec := m.connectSecretInput.Value()
				m.reqSecret = sec

				// Proceed to Ask Save (Pre-Connect)
				m.state = stateConnectedAskSave
				m.connectSecretInput.Blur()
				m.globalInput.Focus()
				m.globalInput.SetValue("")
				m.globalInput.Prompt = "Save Contact? [Y/n]: "
				return m, nil
			} else if msg.Type == tea.KeyEsc {
				m.state = stateNewContactAlias
				m.connectSecretInput.Blur()
				m.globalInput.Focus()
				m.globalInput.SetValue("")
				m.globalInput.Prompt = "Alias: "
				return m, nil
			}
			m.connectSecretInput, tiCmd = m.connectSecretInput.Update(msg)
			return m, tiCmd
		}

		if m.state == stateNewContactWait {
			// Spinner state, allow Abort
			if msg.Type == tea.KeyEsc {
				m.state = stateMenu
				m.globalInput.Focus()
				m.globalInput.SetValue("")
				m.globalInput.Prompt = ""
				m.globalInput.Placeholder = "Enter command..."
				return m, tick()
			}
			return m, nil
		}

		if m.state == stateConnectedAskSave {
			if msg.Type == tea.KeyEnter {
				ans := strings.ToLower(m.globalInput.Value())
				if ans == "y" || ans == "yes" || ans == "" {
					// Save
					alias := m.tempConfig.Name
					if m.config.Contacts == nil {
						m.config.Contacts = make(map[string]string)
					}
					// Normalize address before saving
					address := m.reqConnect
					if !strings.HasSuffix(address, ".onion") {
						address = address + ".onion"
					}
					m.config.Contacts[alias] = address
					if err := SaveConfig(m.config, m.masterKey); err != nil {
						m.logs = append(m.logs, "Error saving contact.")
					} else {
						m.logs = append(m.logs, "Contact Saved.")
					}
					m.statusViewport.SetContent(m.renderLogs(m.statusViewport.Width))
				}

				// NOW Initiate Connection
				m.state = stateNewContactWait
				m.globalInput.Blur()
				m.status = "Connecting..."
				m.logs = append(m.logs, m.status)
				m.statusViewport.SetContent(m.renderLogs(m.statusViewport.Width))

				return m, func() tea.Msg {
					go func() {
						if err := m.netManager.StartTor(context.Background(), m.progressChan); err != nil {
							m.progressChan <- "CONNECTION_FAILED"
							return
						}
						err := m.netManager.Connect(context.Background(), m.reqConnect, m.reqSecret, m.progressChan)
						if err != nil {
							errStr := err.Error()
							if strings.Contains(errStr, "timeout") || strings.Contains(errStr, "deadline") {
								m.progressChan <- "CONNECTION_TIMEOUT"
							} else {
								m.progressChan <- "CONNECTION_FAILED"
							}
							return
						}
						m.progressChan <- "OP_CONNECT_SUCCESS"
					}()
					return nil
				}
			} else if msg.Type == tea.KeyEsc {
				// Don't save, just connect? Or Cancel?
				// The prompt implies saving is optional but connection is the goal.
				// User pressed Esc, maybe they want to cancel everything?
				// But conventionally Esc backtracks.
				// Let's assume Esc means "No, don't save, but proceed".
				// Actually, if they want to cancel, they can Esc during "Connecting...".
				// So Esc here acts like "No".

				// Initiate Connection without saving
				m.state = stateNewContactWait
				m.globalInput.Blur()
				m.status = "Connecting..."
				m.logs = append(m.logs, m.status)
				m.statusViewport.SetContent(m.renderLogs(m.statusViewport.Width))

				return m, func() tea.Msg {
					go func() {
						if err := m.netManager.StartTor(context.Background(), m.progressChan); err != nil {
							m.progressChan <- "CONNECTION_FAILED"
							return
						}
						err := m.netManager.Connect(context.Background(), m.reqConnect, m.reqSecret, m.progressChan)
						if err != nil {
							errStr := err.Error()
							if strings.Contains(errStr, "timeout") || strings.Contains(errStr, "deadline") {
								m.progressChan <- "CONNECTION_TIMEOUT"
							} else {
								m.progressChan <- "CONNECTION_FAILED"
							}
							return
						}
						m.progressChan <- "OP_CONNECT_SUCCESS"
					}()
					return nil
				}
			}
			m.globalInput, tiCmd = m.globalInput.Update(msg)
			return m, tiCmd
		}

		if m.state == stateDeleteContact {
			if msg.Type == tea.KeyEnter {
				cmd := m.globalInput.Value()
				var idx int
				if _, err := fmt.Sscanf(cmd, "%d", &idx); err == nil {
					// Regenerate sorted
					var aliases []string
					for k := range m.config.Contacts {
						aliases = append(aliases, k)
					}
					sort.Strings(aliases)
					m.sortedContacts = aliases

					if idx > 0 && idx <= len(m.sortedContacts) {
						alias := m.sortedContacts[idx-1]
						delete(m.config.Contacts, alias)
						if err := SaveConfig(m.config, m.masterKey); err == nil {
							m.logs = append(m.logs, "Contact Deleted.")
						}
						m.state = stateConnect
						m.globalInput.SetValue("")
						m.globalInput.Prompt = ""
						m.statusViewport.SetContent(m.renderLogs(m.statusViewport.Width))
					}
				}
				return m, nil
			} else if msg.Type == tea.KeyEsc {
				m.state = stateConnect
				return m, nil
			}
			m.globalInput, tiCmd = m.globalInput.Update(msg)
			return m, tiCmd
		}

		// Connect Flow Inputs
		if m.state == stateConnectAddress {
			if msg.Type == tea.KeyEnter {
				addr := m.connectAddrInput.Value()
				if addr != "" {
					m.reqConnect = addr // Temporarily store
					m.state = stateConnectSecret
					m.connectSecretInput.Focus()
					m.connectAddrInput.Blur()
				}
				return m, nil
			} else if msg.Type == tea.KeyEsc {
				m.state = stateConnect
				m.connectAddrInput.Blur()
				m.globalInput.Focus()
				return m, nil
			}
			m.connectAddrInput, tiCmd = m.connectAddrInput.Update(msg)
			return m, tiCmd
		}

		if m.state == stateConnectSecret {
			if msg.Type == tea.KeyEnter {
				// Execute Connect
				secret := m.connectSecretInput.Value()
				m.reqSecret = secret

				// Use Standard Connect Flow
				m.state = stateNewContactWait
				m.connectSecretInput.Blur()
				m.status = "Connecting..."
				m.logs = append(m.logs, m.status)
				m.statusViewport.SetContent(m.renderLogs(m.statusViewport.Width))

				return m, func() tea.Msg {
					go func() {
						if err := m.netManager.StartTor(context.Background(), m.progressChan); err != nil {
							m.progressChan <- "CONNECTION_FAILED"
							return
						}
						err := m.netManager.Connect(context.Background(), m.reqConnect, m.reqSecret, m.progressChan)
						if err != nil {
							errStr := err.Error()
							if strings.Contains(errStr, "timeout") || strings.Contains(errStr, "deadline") {
								m.progressChan <- "CONNECTION_TIMEOUT"
							} else {
								m.progressChan <- "CONNECTION_FAILED"
							}
							return
						}
						m.progressChan <- "OP_CONNECT_SUCCESS"
					}()
					return nil
				}
			} else if msg.Type == tea.KeyEsc {
				m.state = stateConnectAddress
				m.connectSecretInput.Blur()
				m.connectAddrInput.Focus()
				return m, nil
			}
			m.connectSecretInput, tiCmd = m.connectSecretInput.Update(msg)
			return m, tiCmd
		}

		// Config Flow Inputs
		if m.state == stateConfigName {
			if msg.Type == tea.KeyEnter {
				val := m.globalInput.Value()
				if val != "" {
					m.tempConfig.Name = val
				} // If empty, keep existing (which was pre-filled or handled by logic? Actually user wants "enter to keep same")
				// m.globalInput.SetValue(m.config.Name) sets the Default to current.
				// If they clear it and enter, it might be empty.
				// Let's enforce: if empty, keep original `m.config.Name`.
				// But we pre-filled it. If they clear it, they might mean empty? No, name is required usually.
				if val == "" {
					m.tempConfig.Name = m.config.Name
				}

				// Transition to Color
				m.state = stateConfigColor
				m.globalInput.SetValue(m.config.Color)
				m.globalInput.Prompt = "Color: "
				m.globalInput.Placeholder = "Enter new color (hex or name)"
				// We can try to update the prompt style dynamically in View, but here we just prepare state.
				return m, nil
			} else if msg.Type == tea.KeyEsc {
				m.state = stateMenu
				m.globalInput.SetValue("")
				m.globalInput.Prompt = "" // Reset prompt
				m.globalInput.Placeholder = "Enter command..."
				return m, nil
			}
			m.globalInput, tiCmd = m.globalInput.Update(msg)
			return m, tiCmd
		}

		if m.state == stateConfigColor {
			// Live Preview of Color in globalInput
			m.globalInput.TextStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(m.globalInput.Value()))

			if msg.Type == tea.KeyEnter {
				val := m.globalInput.Value()
				if val != "" {
					// Validate Color?
					// lipgloss.Color(val) won't panic, but might not render if invalid.
					// We'll accept it.
					m.tempConfig.Color = val
				} else {
					m.tempConfig.Color = m.config.Color
				}

				// Transition to Password Check
				m.state = stateConfigPasswordCheck
				m.passwordInput.SetValue("")
				m.passwordInput.Placeholder = "Current Password (Enter to skip)"
				m.passwordInput.Prompt = "Current Pass: "
				m.passwordInput.Focus()
				m.globalInput.Blur()
				// Reset Global Input Style
				m.globalInput.TextStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("255"))
				m.globalInput.Prompt = ""

				return m, nil
			} else if msg.Type == tea.KeyEsc {
				m.state = stateMenu
				m.globalInput.TextStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("255")) // Reset
				m.globalInput.SetValue("")
				m.globalInput.Prompt = ""
				m.globalInput.Placeholder = "Enter command..."
				return m, nil
			}
			m.globalInput, tiCmd = m.globalInput.Update(msg)
			return m, tiCmd
		}

		if m.state == stateConfigPasswordCheck {
			if msg.Type == tea.KeyEnter {
				val := m.passwordInput.Value()
				if val == "" {
					// Skip Password Change -> Save Config
					m.config.Name = m.tempConfig.Name
					m.config.Color = m.tempConfig.Color

					if err := SaveConfig(m.config, m.masterKey); err != nil {
						m.logs = append(m.logs, fmt.Sprintf("Error saving config: %v", err))
					} else {
						m.logs = append(m.logs, "Configuration Updated.")
					}
					m.statusViewport.SetContent(m.renderLogs(m.statusViewport.Width))
					m.statusViewport.GotoBottom()

					// Return to Menu
					m.state = stateMenu
					m.passwordInput.SetValue("")
					m.passwordInput.Prompt = "Password: " // Reset default
					// m.passwordInput.Mask = '*'            // Ensure mask (Removed: undefined)
					m.globalInput.Focus()
					return m, nil
				}

				// Verify Password
				// Generate key from input
				key := DeriveMasterKey(val, m.salt) // We need m.salt
				// Compare key with m.masterKey.
				// Since DeriveMasterKey is deterministic with salt:
				// But we don't store m.masterKey for comparison? Yes we do: m.masterKey
				// Wait, checking if bytes match.
				if string(key) == string(m.masterKey) {
					// Match! Proceed to New Password
					m.state = stateConfigPasswordNew
					m.passwordInput.SetValue("")
					m.passwordInput.Placeholder = "Enter New Password"
					m.passwordInput.Prompt = "New Pass: "
				} else {
					// Invalid
					m.passwordInput.SetValue("")
					m.passwordInput.Placeholder = "Incorrect Password"
				}
				return m, nil
			} else if msg.Type == tea.KeyEsc {
				m.state = stateMenu
				m.passwordInput.SetValue("")
				m.passwordInput.Prompt = "Password: "
				m.globalInput.Focus()
				return m, nil
			}
			m.passwordInput, tiCmd = m.passwordInput.Update(msg)
			return m, tiCmd
		}

		if m.state == stateConfigPasswordNew {
			if msg.Type == tea.KeyEnter {
				newPass := m.passwordInput.Value()
				if newPass != "" {
					// 1. Generate new Salt?
					// Security best practice: New password = New Salt.
					// But if we change salt, we re-derive key.
					// newSalt, err := LoadOrGenerateSalt() // Unused
					// Let's keep salt for MVP unless user requested new salt (they didn't).

					newKey := DeriveMasterKey(newPass, m.salt)
					m.masterKey = newKey
					m.logs = append(m.logs, "Master Password Changed.")
				}

				// Save everything
				m.config.Name = m.tempConfig.Name
				m.config.Color = m.tempConfig.Color

				if err := SaveConfig(m.config, m.masterKey); err != nil {
					m.logs = append(m.logs, fmt.Sprintf("Error saving config: %v", err))
				} else {
					m.logs = append(m.logs, "Configuration Saved.")
				}
				m.statusViewport.SetContent(m.renderLogs(m.statusViewport.Width))

				// Return to Menu
				m.state = stateMenu
				m.passwordInput.SetValue("")
				m.passwordInput.Prompt = "Password: "
				m.globalInput.Focus()
				m.globalInput.SetValue("")
				m.globalInput.Placeholder = "Enter command..."
				return m, nil

			} else if msg.Type == tea.KeyEsc {
				m.state = stateMenu
				m.passwordInput.SetValue("")
				m.globalInput.Focus()
				return m, nil
			}
			m.passwordInput, tiCmd = m.passwordInput.Update(msg)
			return m, tiCmd
		}

		switch msg.Type {
		case tea.KeyCtrlC:
			return m, tea.Quit
		case tea.KeyEsc:
			if m.state == stateChat {
				// DISCONNECT LOGIC
				// 1. Send "There is nobody here."
				// 2. Close Connection (properly resets TOR state)
				// 3. Return to Menu

				// Send disconnect message
				goodbye := ChatMessage{
					Content: "There is nobody here.",
					Name:    m.config.Name,
					Color:   "196", // Red
				}
				if m.netManager != nil && m.netManager.Outgoing != nil {
					jsonBytes, _ := json.Marshal(goodbye)
					m.netManager.Outgoing <- string(jsonBytes)
					// Tiny sleep to ensure send
					time.Sleep(100 * time.Millisecond)
				}

				// Close properly - The enhanced Close() method now resets all state
				// including TorInstance, allowing the same NetworkManager to be reused
				if m.netManager != nil {
					m.netManager.Close()
				}

				m.state = stateMenu
				m.logs = append(m.logs, "Disconnected.")
				m.statusViewport.SetContent(m.renderLogs(m.statusViewport.Width))
				return m, nil
			} else {
				return m, tea.Quit
			}
		case tea.KeyEnter:
			if m.state == stateChat {
				v := m.textInput.Value()
				if v != "" {
					payload := ChatMessage{
						Content: v,
						Name:    m.config.Name,
						Color:   m.config.Color,
					}
					jsonBytes, _ := json.Marshal(payload)

					// Check nil to avoid panic
					if m.netManager != nil && m.netManager.Outgoing != nil {
						m.netManager.Outgoing <- string(jsonBytes)
					}

					style := lipgloss.NewStyle().Foreground(lipgloss.Color(m.config.Color)).Bold(true)
					coloredName := style.Render(m.config.Name)
					m.chatViewport.SetContent(m.chatViewport.View() + fmt.Sprintf("\n%s: %s", coloredName, v))

					m.textInput.Reset()
					m.chatViewport.GotoBottom()
				}
			}
		}

	case incomingMsg:
		raw := string(msg)

		// Check for peer-tagged messages from NetworkManager
		if strings.HasPrefix(raw, "PEER_MSG:") {
			// Format: PEER_MSG:peer_id:peer_name:peer_color:content
			parts := strings.SplitN(raw, ":", 5)
			if len(parts) == 5 {
				peerName := parts[2]
				// peerColor in parts[3] is not used - we extract it from ChatMessage
				content := parts[4]

				// Try to parse as FileMessage (includes bradbury_offer)
				var fileMsg FileMessage
				if err := json.Unmarshal([]byte(content), &fileMsg); err == nil && fileMsg.Type != "" {
					boldRedStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("196")).Bold(true)
					sysStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("240")).Italic(true)

					switch fileMsg.Type {
					case "bradbury_offer":
						// Receiver confirmation for /bradbury
						m.awaitingBradburyConfirm = true
						m.chatViewport.SetContent(m.chatViewport.View() + fmt.Sprintf("\n%s", boldRedStyle.Render("Will you receive good literature? (y/n)")))
						m.chatViewport.GotoBottom()
						return m, waitForIncoming(m.netManager)

					case "file_offer":
						// Hold the offer for confirmation
						fileMsgCopy := fileMsg
						m.pendingFileOffer = &fileMsgCopy
						m.pendingFileOfferFrom = peerName
						m.awaitingFileConfirm = true
						prompt := fmt.Sprintf("Will you receive %s from %s? (y/n)", fileMsg.Filename, peerName)
						m.chatViewport.SetContent(m.chatViewport.View() + fmt.Sprintf("\n%s", boldRedStyle.Render(prompt)))
						m.chatViewport.GotoBottom()
						return m, waitForIncoming(m.netManager)

					default:
						// file_chunk / file_end — process only if transfer state exists
						state, err := ReceiveFileChunk(fileMsg, m.fileTransfers)
						if err != nil {
							// Silently drop — no transfer was accepted for this file
						} else if state != nil {
							switch fileMsg.Type {
							case "file_chunk":
								if state.Progress%10 == 0 || state.Progress == state.TotalChunks {
									progress := int(float64(state.Progress) / float64(state.TotalChunks) * 100)
									m.chatViewport.SetContent(m.chatViewport.View() + fmt.Sprintf("\n%s", sysStyle.Render(fmt.Sprintf("Receiving: %s - %d%%", fileMsg.Filename, progress))))
								}
							case "file_end":
								m.chatViewport.SetContent(m.chatViewport.View() + fmt.Sprintf("\n%s", sysStyle.Render(fmt.Sprintf("Received: %s → loadingbay_in/", fileMsg.Filename))))
							}
							m.chatViewport.GotoBottom()
						}
						return m, waitForIncoming(m.netManager)
					}
				}

				// Regular chat message from peer (including whispers)
				var chatMsg ChatMessage
				if err := json.Unmarshal([]byte(content), &chatMsg); err == nil {
					// Add timestamp if enabled
					prefix := ""
					if m.timestampsEnabled {
						prefix = fmt.Sprintf("[%s] ", time.Now().Format("15:04"))
					}

					if chatMsg.Whisper {
						// Render whisper in pink italic
						whisperStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("205")).Italic(true)
						m.chatViewport.SetContent(m.chatViewport.View() + fmt.Sprintf("\n%s", whisperStyle.Render(fmt.Sprintf("%s[whisper from %s]: %s", prefix, chatMsg.Name, chatMsg.Content))))
						m.chatViewport.GotoBottom()
						return m, waitForIncoming(m.netManager)
					}

					// Wrap message text
					prefixLen := len(chatMsg.Name) + 2 + len(prefix)
					wrappedContent := wrapText(chatMsg.Content, m.chatViewport.Width-prefixLen)
					indent := strings.Repeat(" ", prefixLen)
					lines := strings.Split(wrappedContent, "\n")
					for i := 1; i < len(lines); i++ {
						lines[i] = indent + lines[i]
					}
					wrappedContent = strings.Join(lines, "\n")

					peerStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(chatMsg.Color)).Bold(true)
					peerNameStyled := peerStyle.Render(chatMsg.Name)
					m.chatViewport.SetContent(m.chatViewport.View() + fmt.Sprintf("\n%s%s: %s", prefix, peerNameStyled, wrappedContent))
					m.chatViewport.GotoBottom()

					// RELAY: Forward this message to other peers (host acts as relay)
					// Whispers must NOT be relayed
					if !chatMsg.Whisper {
						go func(senderID string, msg string) {
							m.netManager.Outgoing <- fmt.Sprintf("RELAY:%s:%s", senderID, msg)
						}(parts[1], content)
					}

					return m, waitForIncoming(m.netManager)
				}
			}
			return m, waitForIncoming(m.netManager)
		}

		// Check for peer left message
		if strings.HasPrefix(raw, "PEER_LEFT:") {
			// Just ignore peer left messages - they clutter the chat
			// The network layer already cleaned up the connection
			return m, waitForIncoming(m.netManager)
		}

		// Try to parse as FileMessage first (for backwards compatibility with single-peer mode)
		var fileMsg FileMessage
		if err := json.Unmarshal([]byte(raw), &fileMsg); err == nil && fileMsg.Type != "" {
			boldRedStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("196")).Bold(true)
			sysStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("240")).Italic(true)

			switch fileMsg.Type {
			case "bradbury_offer":
				m.awaitingBradburyConfirm = true
				m.chatViewport.SetContent(m.chatViewport.View() + fmt.Sprintf("\n%s", boldRedStyle.Render("Will you receive good literature? (y/n)")))
				m.chatViewport.GotoBottom()
				return m, waitForIncoming(m.netManager)

			case "file_offer":
				fileMsgCopy := fileMsg
				m.pendingFileOffer = &fileMsgCopy
				m.pendingFileOfferFrom = "peer"
				m.awaitingFileConfirm = true
				prompt := fmt.Sprintf("Will you receive %s from peer? (y/n)", fileMsg.Filename)
				m.chatViewport.SetContent(m.chatViewport.View() + fmt.Sprintf("\n%s", boldRedStyle.Render(prompt)))
				m.chatViewport.GotoBottom()
				return m, waitForIncoming(m.netManager)

			default:
				state, err := ReceiveFileChunk(fileMsg, m.fileTransfers)
				if err == nil && state != nil {
					switch fileMsg.Type {
					case "file_chunk":
						if state.Progress%10 == 0 || state.Progress == state.TotalChunks {
							progress := int(float64(state.Progress) / float64(state.TotalChunks) * 100)
							m.chatViewport.SetContent(m.chatViewport.View() + fmt.Sprintf("\n%s", sysStyle.Render(fmt.Sprintf("Receiving: %s - %d%%", fileMsg.Filename, progress))))
						}
					case "file_end":
						m.chatViewport.SetContent(m.chatViewport.View() + fmt.Sprintf("\n%s", sysStyle.Render(fmt.Sprintf("Received: %s → loadingbay_in/", fileMsg.Filename))))
					}
					m.chatViewport.GotoBottom()
				}
				return m, waitForIncoming(m.netManager)
			}
		} // end fallback fileMsg block

		// Fall through to ChatMessage handling
		var chatMsg ChatMessage
		var display string

		if err := json.Unmarshal([]byte(raw), &chatMsg); err == nil {
			// Check for Disconnect Message
			if chatMsg.Content == "There is nobody here." {
				// Log Red
				m.logs = append(m.logs, lipgloss.NewStyle().Foreground(lipgloss.Color("196")).Render("Peer Disconnected: There is nobody here."))
				m.statusViewport.SetContent(m.renderLogs(m.statusViewport.Width))
				m.netManager.Close()
				m.state = stateMenu
				return m, nil
			}

			// Save Peer Color if known contact
			if m.config.ContactColors == nil {
				m.config.ContactColors = make(map[string]string)
			}
			// We only know their Name from the msg. Is it the Alias?
			// The user said "create a new contact, it took the color that the person... chose for themselves".
			// But we map Contacts by Alias -> Address.
			// When we receive a msg, we get Name.
			// If we can match Name to Alias (or simple assume Name == Alias for now? Or just save Name -> Color mapping?)
			// The dashboard loops over `m.config.Contacts` (Aliases).
			// If the *Alias* matches the `chatMsg.Name`, we save it.
			// If not, we can't easily link it unless we reverse lookup address (which we don't have in msg).
			// Let's assume Alias == Name for synced contacts or update color for the alias if we find one.
			// OR: We blindly trust names? No, dashboard iterates Config.Contacts keys.

			// Simple approach: If `chatMsg.Name` is a key in `m.config.Contacts`, update its color.
			if _, ok := m.config.Contacts[chatMsg.Name]; ok {
				if m.config.ContactColors[chatMsg.Name] != chatMsg.Color {
					m.config.ContactColors[chatMsg.Name] = chatMsg.Color
					SaveConfig(m.config, m.masterKey) // Persist
				}
			}

			// Add timestamp if enabled
			prefix := ""
			if m.timestampsEnabled {
				prefix = fmt.Sprintf("[%s] ", time.Now().Format("15:04"))
			}

			peerStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(chatMsg.Color)).Bold(true)
			peerName := peerStyle.Render(chatMsg.Name)
			// Wrap peer message text to fit viewport
			prefixLen := len(chatMsg.Name) + 2 + len(prefix) // "Name: " + timestamp
			wrappedContent := wrapText(chatMsg.Content, m.chatViewport.Width-prefixLen)
			// Add indentation to wrapped lines
			indent := strings.Repeat(" ", prefixLen)
			lines := strings.Split(wrappedContent, "\n")
			for i := 1; i < len(lines); i++ {
				lines[i] = indent + lines[i]
			}
			wrappedContent = strings.Join(lines, "\n")
			display = fmt.Sprintf("\n%s%s: %s", prefix, peerName, wrappedContent)
		} else {
			display = fmt.Sprintf("\nPeer: %s", raw)
		}

		m.chatViewport.SetContent(m.chatViewport.View() + display)
		m.chatViewport.GotoBottom()
		return m, waitForIncoming(m.netManager)

	case statusMsg:
		if msg == "CONNECTION_FAILED" {
			redStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("196")).Bold(true)
			m.logs = append(m.logs, redStyle.Render("There is nobody here, you may have made a mistake."))
			m.statusViewport.SetContent(m.renderLogs(m.statusViewport.Width))
			m.statusViewport.GotoBottom()

			// Return to menu
			m.state = stateMenu
			m.globalInput.Focus()
			m.textInput.Blur()
			return m, nil
		}

		if msg == "CONNECTION_TIMEOUT" {
			redStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("196")).Bold(true)
			m.logs = append(m.logs, redStyle.Render("Connection timed out."))
			m.logs = append(m.logs, "Would you like to retry? (Type 'retry' or press ESC)")
			m.statusViewport.SetContent(m.renderLogs(m.statusViewport.Width))
			m.statusViewport.GotoBottom()

			m.state = stateMenu
			m.globalInput.Focus()
			return m, nil
		}

		if msg == "OP_CONNECT_SUCCESS" {
			m.state = stateChat
			// Fix: Ensure proper input focus when entering chat
			m.globalInput.Blur()
			m.textInput.Focus()
			m.connectionStart = time.Now()
			m.status = "Connection Established."
			m.logs = append(m.logs, m.status)
			m.statusViewport.SetContent(m.renderLogs(m.statusViewport.Width))
			return m, waitForIncoming(m.netManager)
		}

		// Handle Host Errors with helpful message
		if strings.HasPrefix(string(msg), "Host Error:") {
			redStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("196")).Bold(true)
			m.logs = append(m.logs, redStyle.Render(string(msg)))
			m.logs = append(m.logs, redStyle.Render("Couldn't start this time, maybe try /rehost"))
			m.statusViewport.SetContent(m.renderLogs(m.statusViewport.Width))
			m.statusViewport.GotoBottom()
			return m, nil
		}

		// Handle Rehost Errors with helpful message
		if strings.HasPrefix(string(msg), "Rehost Error:") {
			redStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("196")).Bold(true)
			m.logs = append(m.logs, redStyle.Render(string(msg)))
			m.logs = append(m.logs, redStyle.Render("Still having issues. Wait a bit, rest, check your connection and try /rehost."))
			m.statusViewport.SetContent(m.renderLogs(m.statusViewport.Width))
			m.statusViewport.GotoBottom()
			return m, nil
		}

		m.status = string(msg)
		m.logs = append(m.logs, m.status)

		// Capture Token
		if strings.Contains(m.status, "SECURE TOKEN") {
			parts := strings.Split(m.status, "\n")
			if len(parts) > 1 {
				m.currentToken = strings.TrimSpace(parts[1])
				m.logs = append(m.logs, "Press Ctrl+Y to copy token.")
			}
		}

		m.statusViewport.SetContent(m.renderLogs(m.statusViewport.Width))
		m.statusViewport.GotoBottom()

		// Update Progress
		// Parse percentage from Tor logs if available
		// Format: [Tor] ... Bootstrapped 10% ...
		if strings.Contains(m.status, "Bootstrapped") {
			// Extract percent
			// Simple fallback if regex is overkill
			var p int
			if _, err := fmt.Sscanf(m.status, "[Tor] Bootstrapped %d%%", &p); err == nil {
				prgCmd = m.progress.SetPercent(float64(p) / 100.0)
			} else {
				// Try finding substring manually if format varies
				idx := strings.Index(m.status, "Bootstrapped")
				if idx != -1 {
					rest := m.status[idx+len("Bootstrapped"):]
					rest = strings.TrimSpace(rest)
					var p2 int
					if _, err := fmt.Sscanf(rest, "%d%%", &p2); err == nil {
						prgCmd = m.progress.SetPercent(float64(p2) / 100.0)
					}
				}
			}
		} else if strings.Contains(m.status, "Listening on") {
			prgCmd = m.progress.SetPercent(1.0)
		} else if strings.Contains(m.status, "Connected") {
			prgCmd = m.progress.SetPercent(1.0)
		} else {
			// Slow tick for liveness?
			// if m.tick % 20 == 0 {
			// 	m.progress.IncrPercent(0.01)
			// }
		}

		if strings.Contains(m.status, "Listening") || strings.Contains(m.status, "You are not alone") || strings.Contains(m.status, "It whispers back.") || strings.Contains(m.status, "Awaiting others.") {
			m.ready = true // Mark ready for ETA purposes
			m.state = stateChat
			// Fix: Ensure proper input focus when entering chat
			m.globalInput.Blur()
			m.textInput.Focus()
		}

	case pingMsg:
		m.contactStatus[msg.Alias] = msg.Status
		return m, waitForPing(m.pingChan)

	case errMsg:
		m.err = msg
		m.state = stateError
		return m, nil
	}

	m.textInput, tiCmd = m.textInput.Update(msg)
	m.chatViewport, cvpCmd = m.chatViewport.Update(msg)
	m.statusViewport, svpCmd = m.statusViewport.Update(msg)
	// Only update progress if we have a command or tick?
	// Progress model needs updates?
	// Specifically window size updates, but we did that manually.
	// Actually, standard bubbletea update:
	// m.progress, prgCmd = m.progress.Update(msg)
	// But frameMsg is internal to progress? No.
	// We should just update it.

	// Note: We need to handle m.progress.Update(msg) for it to animate.
	progModel, pCmd := m.progress.Update(msg)
	m.progress = progModel.(progress.Model)

	// Helper to update layout if state changed
	// m.updateViewportHeight() <-- REMOVED, we use static distribution now
	m = m.recalcLayout(m.width, m.height)

	return m, tea.Batch(tiCmd, cvpCmd, svpCmd, prgCmd, pCmd)
}

// wrapText inserts newlines into a string to ensure no single line exceeds the given column width.
func wrapText(text string, width int) string {
	if width <= 0 {
		return text
	}

	var wrapped strings.Builder
	words := strings.Fields(text)
	if len(words) == 0 {
		return text
	}

	lineLen := 0
	for i, word := range words {
		wordLen := len(word)

		// If adding this word would exceed width, start new line
		if lineLen > 0 && lineLen+1+wordLen > width {
			wrapped.WriteString("\n")
			wrapped.WriteString(word)
			lineLen = wordLen
		} else {
			if lineLen > 0 {
				wrapped.WriteString(" ")
				lineLen++
			}
			wrapped.WriteString(word)
			lineLen += wordLen
		}

		// Don't add space after last word
		if i < len(words)-1 && lineLen >= width {
			wrapped.WriteString("\n")
			lineLen = 0
		}
	}

	return wrapped.String()
}

func (m model) renderLogs(width int) string {
	var s strings.Builder
	for _, log := range m.logs {
		// Wrap text to fit within viewport width
		wrappedLog := wrapText(log, width)

		var style lipgloss.Style
		if strings.Contains(log, "You are not alone") || strings.Contains(log, "Listening on") || strings.Contains(log, "SECURE TOKEN") {
			style = lipgloss.NewStyle().Foreground(lipgloss.Color("196")).Bold(true)
		} else if strings.Contains(log, "It whispers back.") {
			style = lipgloss.NewStyle().Foreground(lipgloss.Color("#560591")).Bold(true)
		} else {
			style = lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
		}
		s.WriteString(style.Render(wrappedLog) + "\n")
	}
	return s.String()
}

// View renders the entire application UI based on the current session state.
func (m model) View() string {
	if m.state == stateError || m.err != nil {
		return fmt.Sprintf("\n  CRITICAL ERROR:\n\n  %v\n\n  Press any key to exit.\n", m.err)
	}

	if m.state == statePassword {
		if m.passwordMsg != "" {
			return fmt.Sprintf("\n\n   %s\n", lipgloss.NewStyle().Foreground(lipgloss.Color("196")).Bold(true).Render(m.passwordMsg))
		}
		return fmt.Sprintf("\n\n   %s\n", m.passwordInput.View())
	}

	// Define Main Content & Footer based on State
	var mainContent, footerContent string

	if m.state == stateChat {
		mainContent = m.viewChatContent()
		// Footer for Chat is just the input, but stylized
		inputStyle := lipgloss.NewStyle().
			Border(lipgloss.NormalBorder()).
			BorderForeground(lipgloss.Color("240")). // Grey to match Dashboard
			Width(m.width - 2)
		footerContent = inputStyle.Render(m.textInput.View())
	} else if m.state == stateConfigName || m.state == stateConfigColor {
		// Just like Menu but specific content
		rightWidth := 45
		if m.width < 100 {
			rightWidth = 35
		}
		leftWidth := m.width - rightWidth - 4
		if leftWidth < 10 {
			leftWidth = 10
		}

		header := 16
		footer := 3
		bodyH := m.height - (header + footer) - 2
		if bodyH < 0 {
			bodyH = 0
		}

		// Use Dashboard content or Custom?
		// We want to show the specific Config UI.
		// Actually, we can reuse the dashboard layout but put the Input in the center or footer?
		// The Input IS the footer in the current `View`.
		// `footerContent` is `m.globalInput.View()`.
		// So we just need `mainContent` to explain what's happening.

		title := "CONFIGURATION MODE"
		desc := "Enter your details below."
		if m.state == stateConfigColor {
			desc = "Enter a color name or hex code. Text color updates live."
		}

		mainContent = lipgloss.NewStyle().
			Width(leftWidth).
			Height(bodyH).
			Align(lipgloss.Center, lipgloss.Center).
			Render(fmt.Sprintf("\n%s\n\n%s",
				lipgloss.NewStyle().Foreground(lipgloss.Color("205")).Bold(true).Render(title),
				desc,
			))

		inputStyle := lipgloss.NewStyle().
			Border(lipgloss.NormalBorder()).
			BorderForeground(lipgloss.Color("240")).
			Width(m.width - 2)
		footerContent = inputStyle.Render(m.globalInput.View())

	} else if m.state == stateConfigPasswordCheck || m.state == stateConfigPasswordNew {
		// Use Password Input View
		// We can reuse the `statePassword` view style (full screen centered)?
		// Or keep the dashboard layout?
		// Let's keep Dashboard layout for consistency, but put Password Input in Footer?
		// OR put Password Input in Main Content?
		// Current logic puts `passwordInput` in `View` for `statePassword`.
		// Let's use the Footer for consistency since we are in "App Mode".

		rightWidth := 45
		if m.width < 100 {
			rightWidth = 35
		}
		leftWidth := m.width - rightWidth - 4
		header := 16
		footer := 3
		bodyH := m.height - (header + footer) - 2

		title := "SECURITY SETTINGS"
		desc := "Enter Current Password to authorize changes."
		if m.state == stateConfigPasswordNew {
			desc = "Enter NEW Password."
		}

		mainContent = lipgloss.NewStyle().
			Width(leftWidth).
			Height(bodyH).
			Align(lipgloss.Center, lipgloss.Center).
			Render(fmt.Sprintf("\n%s\n\n%s",
				lipgloss.NewStyle().Foreground(lipgloss.Color("196")).Bold(true).Render(title),
				desc,
			))

		inputStyle := lipgloss.NewStyle().
			Border(lipgloss.NormalBorder()).
			BorderForeground(lipgloss.Color("196")). // Red border for password
			Width(m.width - 2)
		footerContent = inputStyle.Render(m.passwordInput.View())

	} else {
		// Dashboard / Menu / Connect
		// Calculate available size for Left Column
		// Logic matches recalcLayout/viewLayout
		rightWidth := 45
		if m.width < 100 {
			rightWidth = 35
		}
		leftWidth := m.width - rightWidth - 4
		if leftWidth < 10 {
			leftWidth = 10
		}

		// Body Height (inner)
		header := 16
		if m.state == stateChat {
			header = 0
		}
		footer := 3
		bodyH := m.height - (header + footer) - 2
		if bodyH < 0 {
			bodyH = 0
		}

		mainContent = m.viewDashboardContent(leftWidth, bodyH)

		inputStyle := lipgloss.NewStyle().
			Border(lipgloss.NormalBorder()).
			BorderForeground(lipgloss.Color("240")).
			Width(m.width - 2)
		footerContent = inputStyle.Render(m.globalInput.View())
	}

	view := m.viewLayout(mainContent, footerContent)

	// NUCLEAR OPTION: Force clip to window size
	// This prevents ANY overflow from pushing the UI up or down.
	return lipgloss.NewStyle().MaxWidth(m.width).MaxHeight(m.height).Render(view)
}

// viewLayout assembles the high-level UI grid, including the header, body columns, and footer input.
func (m model) viewLayout(leftContent, footer string) string {
	// Layout Calc matching viewStatus/recalcLayout logic
	rightWidth := 45
	if m.width < 100 {
		rightWidth = 35
	} // Fixed Status Width
	leftWidth := m.width - rightWidth - 4 // Borders

	// HEADER
	headerLeftStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("196")). // Crimson
		Bold(true).
		// Width constraint removed

		Align(lipgloss.Left)

	headerRightStyle := lipgloss.NewStyle().
		Width(rightWidth).     // Match Status Section Width
		Align(lipgloss.Center) // Center Horizontally

	var header string
	// Show header in all states EXCEPT Chat (and Error)
	if m.state != stateChat {
		// Rose with Vertical Padding to align with ASCII Art (~8 lines vs 4 lines)
		roseView := m.rose.View()

		// Logo Selection: Easter Egg or Default
		logo := ASCII_RIZOMA_2
		if m.easterEgg {
			logo = ASCII_RIZOMA_1
		}

		// Calculate Padding to push Rose to Right
		logoWidth := lipgloss.Width(logo)
		padding := m.width - logoWidth - rightWidth
		if padding < 0 {
			padding = 0
		}
		spacer := strings.Repeat(" ", padding)

		rawHeader := lipgloss.JoinHorizontal(lipgloss.Top,
			headerLeftStyle.Render(logo),
			spacer,
			headerRightStyle.Render(roseView),
		)
		// FORCE HEADER HEIGHT to 10 (Tight fit for Logo)
		header = lipgloss.NewStyle().Height(10).Render(rawHeader)
	} else {
		header = ""
	}

	// BODY Calculations
	headerHeight := 0
	if m.state != stateChat {
		headerHeight = 10
	} else {
		headerHeight = 0 // Actually 0 for Chat to remove it completely
	}
	footerHeight := 3

	// Consistency Check with recalcLayout
	minBodyHeight := 10
	if m.height < headerHeight+footerHeight+minBodyHeight {
		if m.state != stateChat {
			headerHeight = 0
		}
	}
	verticalMarginHeight := headerHeight + footerHeight

	// Layout: Left (Main) + Right (Status)
	// leftWidth is calculated at top functions

	// Ensure min width
	if leftWidth < 10 {
		leftWidth = 10
	}

	// Column Styles
	borderColor := lipgloss.Color("240")

	// Ensure non-negative height
	// SUBTRACT 3 for Borders + 1 Safety Margin to prevent footer clip
	// Adjust footer position for Dashboard views (pull down by 1 line)
	safetyMargin := 3
	// Apply to all non-chat states (Menu, Host, Connect) to ensure consistent footer position
	if m.state != stateChat {
		safetyMargin = 2
	}
	bodyHeight := m.height - verticalMarginHeight - safetyMargin
	if bodyHeight < 0 {
		bodyHeight = 0
	}

	leftStyle := lipgloss.NewStyle().
		Border(lipgloss.NormalBorder()).
		BorderForeground(borderColor).
		Width(leftWidth).
		Height(bodyHeight)

	rightStyle := lipgloss.NewStyle().
		Border(lipgloss.NormalBorder()).
		BorderForeground(borderColor).
		Width(rightWidth).
		Height(bodyHeight)

	// Content
	// Pass the EXACT height available for the status content
	// bodyHeight is the inner height (excluding borders).
	statusContent := m.viewStatus(rightWidth, bodyHeight)

	// Left Content (Dashboard/Menu or Chat)
	// We need to pass dimensions to viewDashboardContent if that's what we are rendering
	// NOTE: viewLayout receives `leftContent` as a string currently.
	// We need to move the decision UP or refactor viewDashboardContent call.
	// `leftContent` is generated in `View()`.
	// We should refactor `View` to pass dimensions or call viewDashboardContent inside viewLayout?
	// Better: `View` relies on `viewLayout` to do the heavy lifting.
	// Actually `viewLayout` calculates dimensions.
	// So `View` calling `viewDashboardContent` BEFORE `viewLayout` is the problem.
	// We must move `viewDashboardContent` call INSIDE `viewLayout` or pass calculated dims to `View`.
	// Since `recalcLayout` runs on every resize, `m.width` and `m.height` are known.
	// And `recalcLayout` sets `m.chatViewport` size.
	// We can use the calculated `leftWidth` and `bodyHeight` (approx) in `View`?
	// Re-calculating in `View` is redundant but safe.
	// Or we can just trust `recalcLayout` happened.
	// Let's look at `View`.

	// Join Columns
	body := lipgloss.JoinHorizontal(lipgloss.Top,
		leftStyle.Render(leftContent),
		rightStyle.Render(statusContent),
	)

	// Force Footer Height to 3
	footer = lipgloss.NewStyle().Height(3).Render(footer)

	return lipgloss.JoinVertical(lipgloss.Left, header, body, footer)
}

// viewStatus renders the right-hand sidebar containing system info, logs, and network progress.
func (m model) viewStatus(width int, height int) string {
	// STATIC LAYOUT DISTRIBUTION
	// Height is the total available height for this column (excluding borders).
	// We must fit: Info + Divider + Logs + Divider + Progress.

	// 1. Establish Fixed/Min Heights
	// Progress: "Network Status:\n" + Bar = 2 lines
	progHeight := 2

	// Info: Variable, but let's cap it to max 5 lines.
	duration := time.Since(m.startTime).Truncate(time.Second)
	eta := "Running..."
	if !m.ready {
		est := 45 * time.Second
		rem := est - duration
		if rem < 0 {
			rem = 0
		}
		eta = fmt.Sprintf("ETA: %s", rem)
	} else {
		eta = fmt.Sprintf("Uptime: %s", duration)
	}

	infoStr := fmt.Sprintf("STATUS: [%s]\n%s", m.status, eta)
	if m.netManager.OnionAddr != "" {
		infoStr += fmt.Sprintf("\nOnion: %s.onion", m.netManager.OnionAddr)
	}
	if m.reqSecret != "" {
		infoStr += fmt.Sprintf("\nSecret: %s", m.reqSecret)
	}
	// Measure Info Height
	infoHeight := strings.Count(infoStr, "\n") + 1
	if infoHeight > 6 {
		infoHeight = 6
	} // Hard cap

	// 3. Divider & Gaps
	// We use JoinVertical. It adds newlines.
	// Structure: Info \n Divider \n Logs \n Divider \n Progress
	// Gaps: 4 internal newlines.
	// Dividers: 2 lines (strings of dashes).
	gapLines := 4
	dividerLines := 2

	fixedOverhead := infoHeight + progHeight + gapLines + dividerLines

	// 4. Calculate Log Height
	logHeight := height - fixedOverhead
	if logHeight < 0 {
		logHeight = 0
	}

	// Styles
	infoStyle := lipgloss.NewStyle().Height(infoHeight).MaxHeight(infoHeight)
	logStyle := lipgloss.NewStyle().Height(logHeight).MaxHeight(logHeight)
	progStyle := lipgloss.NewStyle().Height(progHeight).MaxHeight(progHeight)

	// Use viewport view which should be updated safely in background?
	// Or we force clip it here:
	logs := m.statusViewport.View()

	// Divider
	dividerWidth := width
	if dividerWidth < 0 {
		dividerWidth = 0
	}
	divider := strings.Repeat("-", dividerWidth)

	// Progress
	progView := fmt.Sprintf("Network Status:\n%s", m.progress.View())

	return lipgloss.JoinVertical(lipgloss.Left,
		infoStyle.Render(infoStr),
		divider,
		logStyle.Render(logs),
		divider,
		progStyle.Render(progView),
	)
}

// viewChatContent renders the scrollable chat history viewport.
func (m model) viewChatContent() string {
	// Chat Viewport
	return m.chatViewport.View()
}

// viewDashboardContent renders the primary menu screen with operational commands and peer status.
func (m model) viewDashboardContent(width, height int) string {
	// SPLIT VIEW: OPERATIONS | PEERS
	// No borders, just a vertical separator.

	sepWidth := 1
	innerWidth := width - sepWidth
	halfWidth := innerWidth / 2

	if halfWidth < 20 {
		halfWidth = 20
	}

	// Styles - No Border
	boxStyle := lipgloss.NewStyle().
		Width(halfWidth).
		Height(height)

	// Vertical Divider
	var sepBuilder strings.Builder
	// Extend 6 lines down as requested
	sepHeight := height + 6
	for i := 0; i < sepHeight; i++ {
		sepBuilder.WriteString("|\n")
	}
	sepContent := lipgloss.NewStyle().
		Foreground(lipgloss.Color("240")).
		Height(height). // Keep container height same to prevent layout break, but render overflowing content?
		// Actually, JoinHorizontal aligns top. If content is longer, it affects the whole block height?
		// Let's rely on Render returning the long string.
		Render(strings.TrimRight(strings.Repeat("|\n", sepHeight), "\n"))

	// OPERATIONS LIST
	ops := []string{
		"[1] HOST",
		"[2] CONNECT",
		"[3] CONFIG",
		"[4] PRE-HEAT",
		"[5] THE VOID",
	}

	var opsList strings.Builder
	opsList.WriteString("\n") // Top padding
	for _, op := range ops {
		opsList.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color("255")).Render(op) + "\n\n")
	}

	opsContent := lipgloss.JoinVertical(lipgloss.Left,
		opsList.String(),
	)

	// PEERS LIST
	var peersList strings.Builder

	if len(m.config.Contacts) == 0 {
		peersList.WriteString("\n No contacts found.")
	} else {
		peersList.WriteString("\n")
		// Generate Sorted List for Consistency
		var aliases []string
		for k := range m.config.Contacts {
			aliases = append(aliases, k)
		}
		sort.Strings(aliases)
		m.sortedContacts = aliases

		peersList.WriteString("\n")

		for i, alias := range aliases {
			status := m.contactStatus[alias]
			if status == "" {
				status = "..."
			}

			// Colorize Status
			statusColor := "240"
			if status == "ONLN" {
				statusColor = "46"
			}
			if status == "OFFLN" {
				statusColor = "196"
			}

			// Main Menu Format: "-Alias [STATUS]" (Requested: "first the name, then status")
			// Connect Mode Format: "N -Alias [STATUS]" (Requested: "all contacts should now show a number")

			prefix := ""
			if m.state == stateConnect || m.state == stateDeleteContact {
				prefix = fmt.Sprintf("%d ", i+1)
			}

			sRender := lipgloss.NewStyle().Foreground(lipgloss.Color(statusColor)).Render(fmt.Sprintf("[%s]", status))

			// Color Logic: Use Saved Color if available, else Hash
			var aliasColor string
			if savedColor, ok := m.config.ContactColors[alias]; ok && savedColor != "" {
				aliasColor = savedColor
			} else {
				aliasColor = hashColor(alias)
			}

			aliRender := lipgloss.NewStyle().Foreground(lipgloss.Color(aliasColor)).Render("-" + alias)

			// Format: "N -Alias [STATUS]" or "-Alias [STATUS]"
			row := fmt.Sprintf("%s%s %s\n", prefix, aliRender, sRender)
			peersList.WriteString(row)
		}
	}

	peersContent := lipgloss.JoinVertical(lipgloss.Left,
		peersList.String(),
	)

	// Determine what to show in the "Operations" box based on sub-state
	leftBoxStr := opsContent

	if m.state == stateHost {
		leftBoxStr = lipgloss.JoinVertical(lipgloss.Left,
			"\n",
			lipgloss.NewStyle().Foreground(lipgloss.Color("205")).Bold(true).Render("HOST MODE"),
			"\n",
			"Enter Session Secret:",
		)
	} else if m.state == stateConnect {
		// Connect Mode
		leftBoxStr = lipgloss.JoinVertical(lipgloss.Left,
			"\n",
			lipgloss.NewStyle().Foreground(lipgloss.Color("205")).Bold(true).Render("CONNECT MODE"),
			"\n",
			"Select Index.",
			"\n",
			"[0]  New Contact",
			"[-1] Delete Contact",
		)
	} else if m.state == stateNewContactMethod {
		leftBoxStr = lipgloss.JoinVertical(lipgloss.Left,
			"\n",
			"NEW CONTACT",
			"\n",
			"Select Method:",
			"[1] Address",
			"[2] Encrypted Key",
		)
	} else if m.state == stateNewContactAddress {
		leftBoxStr = lipgloss.JoinVertical(lipgloss.Left,
			"\n",
			"NEW CONTACT",
			"\n",
			"Enter Details:",
			m.connectAddrInput.View(), // Reusing input models
		)
	} else if m.state == stateNewContactAlias {
		leftBoxStr = lipgloss.JoinVertical(lipgloss.Left,
			"\n",
			"NEW CONTACT",
			"\n",
			"Enter Alias:",
			m.globalInput.View(),
		)
	} else if m.state == stateNewContactSecretEntry {
		leftBoxStr = lipgloss.JoinVertical(lipgloss.Left,
			"\n",
			"NEW CONTACT",
			"\n",
			"Enter Session Secret:",
			m.connectSecretInput.View(),
		)
	} else if m.state == stateNewContactWait {
		leftBoxStr = lipgloss.JoinVertical(lipgloss.Left,
			"\n",
			"NEW CONTACT",
			"\n",
			"Connecting...",
			"(Please Wait)",
		)
	} else if m.state == stateConnectedAskSave {
		leftBoxStr = lipgloss.JoinVertical(lipgloss.Left,
			"\n",
			"NEW CONTACT",
			"\n",
			"Save Contact?",
			"[Y] Yes   [N] No",
		)
	} else if m.state == stateDeleteContact {
		leftBoxStr = lipgloss.JoinVertical(lipgloss.Left,
			"\n",
			lipgloss.NewStyle().Foreground(lipgloss.Color("196")).Bold(true).Render("DELETE MODE"),
			"\n",
			"Select Index to Delete:",
			"(Esc to Cancel)",
		)
	} else if m.state == stateConnectSecret {
		leftBoxStr = lipgloss.JoinVertical(lipgloss.Left,
			"\n",
			lipgloss.NewStyle().Foreground(lipgloss.Color("205")).Bold(true).Render("CONNECT MODE"),
			"\n",
			"Enter Secret:",
			m.connectSecretInput.View(),
		)
	}

	return lipgloss.JoinHorizontal(lipgloss.Top,
		boxStyle.Render(leftBoxStr),
		sepContent,
		boxStyle.Render(peersContent),
	)
}

// waitForIncoming creates a command that blocks until a new message is received from the network layer.
func waitForIncoming(nm *NetworkManager) tea.Cmd {
	return func() tea.Msg {
		msg, ok := <-nm.Incoming
		if !ok {
			return nil
		}
		return incomingMsg(msg)
	}
}

// recalcLayout adjusts the dimensions of all UI components and viewports to fit the current terminal size.
func (m model) recalcLayout(width, height int) model {
	m.width = width
	m.height = height

	headerHeight := 0
	// Show Header in all non-Chat states
	if m.state != stateChat {
		headerHeight = 10 // Tight header with ASCII Art
	} else {
		headerHeight = 0 // 0 for Chat
	}
	footerHeight := 3

	// BOUNDS CHECKING
	// Ensure we don't exceed window height
	// If window is too small, force hide header
	minBodyHeight := 10
	if height < headerHeight+footerHeight+minBodyHeight {
		if m.state != stateChat {
			// Try compact header? Or just hide it?
			// User complained about cut-off. Better to hide or reduce?
			// Let's try to reduce it first if possible, or just hide it.
			// Given the ASCII art is fixed size, we can't really "reduce" it easily without changing asset.
			// Let's hide it if space is CRITICAL.
			// But for now, let's just ensure body has 0 height rather than negative?
			// Actually, removing header in low-height situations is better UX.
			headerHeight = 0
		}
	}

	verticalMarginHeight := headerHeight + footerHeight
	bodyHeight := height - verticalMarginHeight

	// More strict bounds checks
	if bodyHeight < 0 {
		bodyHeight = 0
	}

	// WIDTHS
	// WIDTHS
	// FIXED RIGHT SIDEBAR LOGIC (Matching ViewLayout)
	rightWidth := 45
	if width < 100 {
		rightWidth = 35
	} // Shrink slightly on small screens

	// Main Content (Left) takes remaining space
	leftWidth := width - rightWidth - 4
	if leftWidth < 10 {
		leftWidth = 10
	}

	chatWidth := leftWidth - 4 // Chat is Left

	// HEIGHTS
	// Account for Vertical Borders (Top+Bottom = 2) in body styles
	// AND Safety Margin (1)
	bodyContentHeight := bodyHeight - 3
	if bodyContentHeight < 0 {
		bodyContentHeight = 0
	}

	if !m.ready {
		// Chat Viewport is LEFT (chatWidth)
		m.chatViewport = viewport.New(chatWidth, bodyContentHeight)
		m.chatViewport.YPosition = headerHeight
		m.chatViewport.SetContent("Welcome to Rizoma.\nInitializing Tor network...\n")

		// Status Viewport is RIGHT (rightWidth)
		m.statusViewport = viewport.New(rightWidth, bodyContentHeight-8)
		m.statusViewport.YPosition = headerHeight
		m.statusViewport.SetContent(m.renderLogs(rightWidth))

		m.ready = true
	} else {
		m.chatViewport.Width = chatWidth
		m.chatViewport.Height = bodyContentHeight

		// We need to keep the viewport height correct for scrolling to work.
		// "Safe" estimate based on our viewStatus logic:
		// Body - Info(max 6) - Prog(2) - Dividers(2) - Gaps(4) = Body - 14
		// We set it to bodyContentHeight - 14 to be safe.
		safeLogHeight := bodyContentHeight - 14
		if safeLogHeight < 0 {
			safeLogHeight = 0
		}

		m.statusViewport.Width = rightWidth
		m.statusViewport.Height = safeLogHeight
		m.statusViewport.SetContent(m.renderLogs(rightWidth))
	}
	// Layout is: Info (Variable) + Divider (1) + Viewport + Progress (4)
	// Info lines: 2 default + 1 if onion + 1 if secret = Max 4.
	// Divider = 1.
	// Progress = 3 (Label + Bar + spacing).
	// Bottom Divider = 1.
	// Gaps added by JoinVertical ~3.
	m.progress.Width = rightWidth - 4
	m.textInput.Width = chatWidth
	m.globalInput.Width = width - 4

	return m
}

// InteractiveMenu provides a text-based fallback interface for basic configuration before the TUI starts.
// It returns true if the application should proceed to the graphical TUI mode.
func InteractiveMenu(reader *bufio.Reader, hostMode *bool, connectAddr *string, secret *string, masterKey []byte) bool {
	// If flags are already set, skip menu
	if *hostMode || *connectAddr != "" {
		return true // Proceed to TUI bootstrapping
	}

	for {
		fmt.Println("\nRizoma (Simple CLI Mode)")
		fmt.Println("-----------------")
		fmt.Println("1. Host")
		fmt.Println("2. Connect")
		fmt.Println("3. Settings (Manage Contacts)")
		fmt.Print("Mode: ")

		choice, _ := reader.ReadString('\n')
		choice = strings.TrimSpace(choice)

		if choice == "1" {
			*hostMode = true
			if *secret == "" {
				fmt.Print("Enter Secret Phrase (optional, enter for random): ")
				s, _ := reader.ReadString('\n')
				*secret = strings.TrimSpace(s)
				if *secret == "" {
					*secret = fmt.Sprintf("rizoma-session-%d", time.Now().Unix())
				}
			}
			return true
		} else if choice == "2" {
			// Connect Menu
			cfg, _, err := LoadConfig(masterKey)
			if err != nil {
				fmt.Printf("Error loading config: %v\n", err)
				continue
			}

			fmt.Println("\n--- Connect ---")
			var contacts []string
			i := 1
			for alias, addr := range cfg.Contacts {
				fmt.Printf("%d. %s (%s)\n", i, alias, addr)
				contacts = append(contacts, alias)
				i++
			}
			fmt.Println("0. New Connection")

			fmt.Print("Select Contact: ")
			selStr, _ := reader.ReadString('\n')
			selStr = strings.TrimSpace(selStr)

			if selStr == "0" {
				// New Connection
				fmt.Print("Do you have a Secure Token? (y/n): ")
				hasToken, _ := reader.ReadString('\n')
				hasToken = strings.TrimSpace(strings.ToLower(hasToken))

				if hasToken == "y" || hasToken == "yes" {
					fmt.Print("Enter target Username: ")
					username, _ := reader.ReadString('\n')
					username = strings.TrimSpace(username)

					fmt.Print("Paste Secure Token: ")
					token, _ := reader.ReadString('\n')
					token = strings.TrimSpace(token)

					// We need the secret too? No, secret is INSIDE the token?
					// Wait, DecryptToken needs (token, username, secret).
					// Ah, the token encrypts the ONION ADDRESS using the derived key from (username + secret).
					// So if I have the token, I MUST know the secret already?
					// Reread crypto_utils.go: DecryptToken(tokenB64, username, secret)
					// Yes, the receiver MUST know the secret to decrypt the address.
					// So asking for token implies I also need to ask for the secret.

					fmt.Print("Enter Shared Secret: ")
					s, _ := reader.ReadString('\n')
					*secret = strings.TrimSpace(s)

					onion, err := DecryptToken(token, username, *secret)
					if err != nil {
						fmt.Printf("Error decrypting token: %v\n", err)
						continue
					}
					*connectAddr = onion
					fmt.Printf("Decrypted Address: %s\n", *connectAddr)
				} else {
					// Manual Onion
					fmt.Print("Enter Onion Address: ")
					addr, _ := reader.ReadString('\n')
					*connectAddr = strings.TrimSpace(addr)

					if *secret == "" {
						fmt.Print("Enter Secret Phrase: ")
						s, _ := reader.ReadString('\n')
						*secret = strings.TrimSpace(s)
					}
				}

				// Ask to Save
				if *connectAddr != "" {
					fmt.Print("Save this contact? (y/n): ")
					save, _ := reader.ReadString('\n')
					if strings.HasPrefix(strings.ToLower(save), "y") {
						fmt.Print("Enter Alias: ")
						alias, _ := reader.ReadString('\n')
						alias = strings.TrimSpace(alias)
						if alias != "" {
							if cfg.Contacts == nil {
								cfg.Contacts = make(map[string]string)
							}
							cfg.Contacts[alias] = *connectAddr
							if err := SaveConfig(cfg, masterKey); err != nil {
								fmt.Printf("Error saving config: %v\n", err)
							} else {
								fmt.Println("Contact saved.")
							}
						}
					}
					return true
				}
			} else {
				// Existing Contact
				// Need to parse selection index
				var idx int
				if _, err := fmt.Sscanf(selStr, "%d", &idx); err == nil && idx > 0 && idx <= len(contacts) {
					alias := contacts[idx-1]
					*connectAddr = cfg.Contacts[alias]
					fmt.Printf("Connecting to %s (%s)...\n", alias, *connectAddr)

					// Use existing secret? Or prompt?
					// The secure token logic implies secret is shared out of band.
					// If saved contact, we only saved address. We probably need secret again.
					if *secret == "" {
						fmt.Print("Enter Secret Phrase: ")
						s, _ := reader.ReadString('\n')
						*secret = strings.TrimSpace(s)
					}
					return true
				} else {
					fmt.Println("Invalid selection.")
				}
			}

		} else if choice == "3" {
			// Settings / Manage Contacts
			cfg, _, err := LoadConfig(masterKey)
			if err != nil {
				fmt.Printf("Error loading config: %v\n", err)
				continue
			}
			fmt.Println("\n--- Manage Contacts ---")
			var contacts []string
			i := 1
			for alias, addr := range cfg.Contacts {
				fmt.Printf("%d. %s (%s)\n", i, alias, addr)
				contacts = append(contacts, alias)
				i++
			}
			fmt.Println("Type number to delete, or 0 to go back.")
			fmt.Print("Choice: ")
			selStr, _ := reader.ReadString('\n')
			selStr = strings.TrimSpace(selStr)

			if selStr != "0" {
				var idx int
				if _, err := fmt.Sscanf(selStr, "%d", &idx); err == nil && idx > 0 && idx <= len(contacts) {
					alias := contacts[idx-1]
					delete(cfg.Contacts, alias)
					if err := SaveConfig(cfg, masterKey); err != nil {
						fmt.Printf("Error deleting contact: %v\n", err)
					} else {
						fmt.Printf("Deleted %s.\n", alias)
					}
				}
			}

		} else {
			fmt.Println("Invalid choice.")
		}
	}
}
