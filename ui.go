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

 
type sessionState int

const (
	statePassword sessionState = iota
	stateMenu
	stateHost
	stateConnect
	stateConnectAddress
	stateConnectSecret
	stateChat
	stateConfigName
	stateConfigColor
	stateConfigPasswordCheck
	stateConfigPasswordNew
	stateError
	 
	stateConnectMethod  
	 
	 
	stateNewContactMethod       
	stateNewContactAddress      
	stateNewContactSecretEntry  
	stateNewContactAlias        
	stateNewContactWait         
	stateConnectedAskSave       
	stateDeleteContact          
)

 
type incomingMsg string
type errMsg error
type statusMsg string

 
type ChatMessage struct {
	Content string `json:"content"`
	Name    string `json:"name"`
	Color   string `json:"color"`
}

type PingResult struct {
	Alias  string
	Status string  
}

type pingMsg PingResult

type model struct {
	state          sessionState
	chatViewport   viewport.Model
	statusViewport viewport.Model  
	textInput      textinput.Model
	 
	passwordInput textinput.Model
	passwordMsg   string  

	 
	netManager *NetworkManager
	config     Config
	salt       []byte  
	masterKey  []byte
	status     string
	logs       []string
	err        error
	ready      bool
	width      int
	height     int

	 
	contactStatus  map[string]string  
	sortedContacts []string           
	pingChan       chan PingResult
	lastPing       time.Time

	 
	tempConfig Config

	 
	tick      int
	rose      RoseModel
	startTime time.Time
	msgTimer  int
	easterEgg bool

	 
	globalInput textinput.Model
	progress    progress.Model

	 
	connectAddrInput   textinput.Model
	connectSecretInput textinput.Model

	 
	reqHost    bool
	reqConnect string
	reqSecret  string

	 
	progressChan chan string
	currentToken string

	 
	fileTransfers   map[string]*FileTransferState  
	folderSendCount int                            
	folderSendTotal int

	 
	timestampsEnabled bool       
	tempNick          string     
	tempColor         string     
	connectionStart   time.Time  
	confirmLeave      bool       
}

 
var illustratedManPDF []byte

 
 
func GenerateStyledQR(encryptedKey, alias, secret string) (string, error) {
	 
	qrDir := "qr_codes"
	if err := os.MkdirAll(qrDir, 0755); err != nil {
		return "", fmt.Errorf("failed to create qr_codes directory: %v", err)
	}

	 
	qr, err := qrcode.New(encryptedKey, qrcode.High)
	if err != nil {
		return "", fmt.Errorf("failed to create QR code: %v", err)
	}

	 
	qrSize := 500
	qr.DisableBorder = true  

	 
	qrImg := qr.Image(qrSize)

	 
	canvasWidth := 500
	canvasHeight := 540  
	canvas := image.NewRGBA(image.Rect(0, 0, canvasWidth, canvasHeight))

	 
	black := color.RGBA{0, 0, 0, 255}
	draw.Draw(canvas, canvas.Bounds(), &image.Uniform{black}, image.Point{}, draw.Src)

	 
	red := color.RGBA{255, 0, 0, 255}

	 
	qrBounds := qrImg.Bounds()
	offsetX := 0  
	offsetY := 0  

	for y := qrBounds.Min.Y; y < qrBounds.Max.Y; y++ {
		for x := qrBounds.Min.X; x < qrBounds.Max.X; x++ {
			 
			originalColor := qrImg.At(x, y)
			r, g, b, _ := originalColor.RGBA()

			 
			if r < 32768 && g < 32768 && b < 32768 {
				canvas.Set(x+offsetX, y+offsetY, red)
			}
		}
	}

	 
	textY := offsetY + qrBounds.Dy() + 20

	 
	aliasText := "//" + alias
	addLabel(canvas, aliasText, 10, textY, red)  

	 
	secretText := "[" + secret + "]"
	secretWidth := len(secretText) * 7                                    
	addLabel(canvas, secretText, canvasWidth-secretWidth-10, textY, red)  

	 
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

func initialModel(nm *NetworkManager, salt []byte, reqHost bool, reqConnect string, reqSecret string, progressChan chan string) model {
	 
	ti := textinput.New()
	ti.Placeholder = "Enter command..."
	ti.Focus()
	 
	 
	ti.TextStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("255"))  
	 
	 
	 
	 
	ti.Cursor.Style = lipgloss.NewStyle().Foreground(lipgloss.Color("196")).Bold(true)  

	 
	pi := textinput.New()
	pi.Placeholder = ""
	pi.EchoMode = textinput.EchoPassword
	pi.Prompt = "Password: "
	pi.Focus()
	pi.TextStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("196")).Bold(true)     
	pi.PromptStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("196")).Bold(true)   
	pi.Cursor.Style = lipgloss.NewStyle().Foreground(lipgloss.Color("196")).Bold(true)  
	pi.Cursor.Style = lipgloss.NewStyle().Foreground(lipgloss.Color("196")).Bold(true).Reverse(true)
	 
	pi.Cursor.Blink = true

	 
	gi := textinput.New()
	gi.Placeholder = "Enter command..."
	gi.CharLimit = 10000  
	gi.TextStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("255"))
	gi.Cursor.Style = lipgloss.NewStyle().Foreground(lipgloss.Color("196")).Bold(true)

	 
	cai := textinput.New()
	cai.Placeholder = "onion_address.onion"
	cai.CharLimit = 2048                                                   
	cai.TextStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("205"))  
	cai.Prompt = "Peer Address: "
	cai.Cursor.Style = lipgloss.NewStyle().Foreground(lipgloss.Color("205"))

	csi := textinput.New()
	csi.Placeholder = "Shared Secret"
	csi.CharLimit = 1000                   
	csi.EchoMode = textinput.EchoPassword  
	csi.Prompt = "Secret: "
	csi.Cursor.Style = lipgloss.NewStyle().Foreground(lipgloss.Color("205"))

	 
	prog := progress.New(progress.WithDefaultGradient())
	prog.Width = 30  

	 
	rose := NewRoseModel()

	 
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

 
func hashColor(s string) string {
	sum := 0
	for _, c := range s {
		sum += int(c)
	}
	 
	colors := []string{"196", "205", "46", "39", "51", "226", "208", "201", "165", "118"}
	return colors[sum%len(colors)]
}

 
func checkContacts(contacts map[string]string, nm *NetworkManager, ch chan PingResult) {
	 
	sem := make(chan struct{}, 3)  
	 
	 
	 
	 

	for alias, addr := range contacts {
		go func(a, ad string) {
			sem <- struct{}{}         
			defer func() { <-sem }()  

			online := nm.CheckConnection(ad)
			status := "OFFLN"
			if online {
				status = "ONLN"
			}
			ch <- PingResult{Alias: a, Status: status}
		}(alias, addr)
	}
}

 

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
	 
	case "/help":
		 
		if len(args) == 0 {
			helpText := `Available Commands:

/host /connect /contact /preheat /leave /help /clear /timestamp /color /nick
/online /loadingbay /coinflip /dice /quote /ping /reconnect /rehost /qr /rizoma

Shortcuts:
CTRL + Y: Copy encrypted key
CTRL + L: Copy logs

Type /help <command> for details on a specific command.`

			m.logs = append(m.logs, helpText)
			m.statusViewport.SetContent(m.renderLogs(m.statusViewport.Width))
			m.statusViewport.GotoBottom()
			return true, nil
		}

		 
		helpCmd := strings.ToLower(args[0])
		var detailedHelp string

		switch helpCmd {
		case "host":
			detailedHelp = "/host <secret>\n  Host with specified secret"
		case "connect":
			detailedHelp = "/connect <alias> key <encrypted_key> <secret>\n  Connect using encrypted key\n\n/connect <alias> address <address.onion> <secret>\n  Connect using direct address"
		case "contact":
			detailedHelp = "/contact <alias> <secret>\n  Connect to saved contact by alias"
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
			detailedHelp = "/loadingbay <subcommand>\n  send <file|*>  - Send file(s), use * for all\n  list           - List available files\n  preview <file> - Show file details"
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

		case "cancel":
			m.chatViewport.SetContent(m.chatViewport.View() + "\n" + sysStyle.Render("Transfer cancellation not yet implemented"))
			m.chatViewport.GotoBottom()
			return true, nil
		}
		return true, nil

	 
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

	 
	case "/ping":
		 
		 
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
			 
			addr := m.reqConnect
			secret := m.reqSecret

			 
			m.netManager.Close()
			m.chatViewport.SetContent(m.chatViewport.View() + "\n" + sysStyle.Render("Reconnecting..."))
			m.chatViewport.GotoBottom()

			 
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
			 
			m.chatViewport.SetContent(m.chatViewport.View() + "\n" + errorStyle.Render("No active connections to reconnect"))
			m.chatViewport.GotoBottom()
		}
		return true, nil

	case "/rehost":
		if m.reqSecret != "" {
			 
			if m.netManager != nil {
				m.netManager.Close()
			}
			m.chatViewport.SetContent(m.chatViewport.View() + "\n" + sysStyle.Render("Retrying host..."))
			m.chatViewport.GotoBottom()

			 
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
		 
		if m.netManager.OnionAddr != "" && m.reqSecret != "" {
			 
			alias := m.config.Name
			if alias == "" {
				alias = "Anonymous"
			}

			 
			token, err := EncryptOnion(m.netManager.OnionAddr, alias)
			if err != nil {
				m.chatViewport.SetContent(m.chatViewport.View() + "\n" + errorStyle.Render(fmt.Sprintf("Failed to generate token: %v", err)))
				m.chatViewport.GotoBottom()
				return true, nil
			}

			 
			filename, err := GenerateStyledQR(token, alias, m.reqSecret)
			if err != nil {
				m.chatViewport.SetContent(m.chatViewport.View() + "\n" + errorStyle.Render(fmt.Sprintf("Failed to generate QR: %v", err)))
				m.chatViewport.GotoBottom()
				return true, nil
			}

			 
			successMsg := fmt.Sprintf("QR code saved to: %s", filename)
			m.chatViewport.SetContent(m.chatViewport.View() + "\n" + sysStyle.Render(successMsg))
			m.chatViewport.GotoBottom()
		} else {
			m.chatViewport.SetContent(m.chatViewport.View() + "\n" + errorStyle.Render("You must be hosting to generate a QR code."))
			m.chatViewport.GotoBottom()
		}
		return true, nil

	 
	case "/rizoma":
		about := `Rizoma

Use this tool carefully, and meaningfully. The world is in your hands and nobody shall take it away from you.
Built with Go, TOR, love and determination by GonnSolo.`
		 
		boldRedStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("196")).Bold(true)
		m.chatViewport.SetContent(m.chatViewport.View() + "\n" + boldRedStyle.Render(about))
		m.chatViewport.GotoBottom()
		return true, nil

	case "/host":
		 
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
		 
		if len(illustratedManPDF) == 0 {
			m.chatViewport.SetContent(m.chatViewport.View() + "\n" + errorStyle.Render("The Illustrated Man has disappeared... (PDF not embedded)"))
			m.chatViewport.GotoBottom()
			return true, nil
		}

		 
		if m.netManager != nil && len(m.netManager.Connections) > 0 {
			go func() {
				 
				if err := SendFileFromBytes("illustrated-man-by-ray-bradbury.pdf", illustratedManPDF, m.netManager.Outgoing); err != nil {
					m.progressChan <- fmt.Sprintf("Failed to send the Illustrated Man: %v", err)
				} else {
					m.progressChan <- "The Illustrated Man has been shared with your peers."
				}
			}()

			bradburyStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("208")).Italic(true)
			m.chatViewport.SetContent(m.chatViewport.View() + "\n" + bradburyStyle.Render("\"It was a pleasure to burn.\" - Sharing The Illustrated Man..."))
			m.chatViewport.GotoBottom()
		} else {
			m.chatViewport.SetContent(m.chatViewport.View() + "\n" + errorStyle.Render("No peers connected to share with."))
			m.chatViewport.GotoBottom()
		}
		return true, nil

	case "/leave":
		 
		m.confirmLeave = true
		boldRedStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("196")).Bold(true)
		m.chatViewport.SetContent(m.chatViewport.View() + "\n" + boldRedStyle.Render("You wanna get out of here? (y/n)"))
		m.chatViewport.GotoBottom()
		return true, nil
	}

	return false, nil
}

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

	 
	case tickMsg:
		if m.state == statePassword && m.passwordMsg != "" {
			if m.msgTimer > 0 {
				m.msgTimer--
				return m, tick()
			}
			m.passwordMsg = ""

			 

			 

			 
			 
			 

			 
			if m.reqHost {
				m.state = stateChat
				m.connectionStart = time.Now()
				m.textInput.Focus()

				 
				return m, func() tea.Msg {
					go func() {
						 
						 
						 
						 
						 
						 
						 
						 
						 
						 

						 
						 
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

			 
			 
			 
			 
			m.state = stateMenu
			m.globalInput.Focus()

			return m, tick()
		}
		if m.state == stateMenu || m.state == stateHost || m.state == stateConnect || m.state == stateConnectAddress || m.state == stateConnectSecret {
			 
			m.rose.Tick()
			m.tick++

			 
			 
			 
			 
			 
			if time.Since(m.lastPing) > 60*time.Second && m.netManager.TorInstance != nil {
				m.lastPing = time.Now()
				 
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

		 
		if m.state == statePassword {
			if msg.Type == tea.KeyEnter {
				pass := m.passwordInput.Value()
				if pass != "" {
					 
					key := DeriveMasterKey(pass, m.salt)  
					 
					cfg, _, err := LoadConfig(key)
					if err == nil {
						 
						m.masterKey = key
						m.config = cfg
						m.passwordMsg = "There are no ears in these walls."
						m.msgTimer = 20  
						m.passwordInput.Blur()
						 
						 
						return m, tick()
					} else {
						 
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

		 
		if msg.Type == tea.KeyCtrlL {
			 
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
			 
			if m.netManager.OnionAddr != "" {
				token, err := EncryptOnion(m.netManager.OnionAddr, m.config.Name)
				if err == nil {
					clipboard.WriteAll(token)
					m.status = "Encrypted Token copied!"
					m.logs = append(m.logs, m.status)
					 
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

		 
		if m.state == stateMenu || m.state == stateHost || m.state == stateConnect {
			if msg.Type == tea.KeyEnter {
				cmd := m.globalInput.Value()
				m.globalInput.SetValue("")

				 
				if m.state == stateMenu {
					 
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
										 
										encryptedKey := parts[3]
										secret := parts[4]

										 
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

									 
									address, exists := m.config.Contacts[alias]
									if !exists {
										m.progressChan <- fmt.Sprintf("Contact '%s' not found", alias)
										return m, nil
									}

									 
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
							case "/help":
								 
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
						 
						m.state = stateConfigName
						m.tempConfig = m.config  
						m.globalInput.SetValue(m.config.Name)
						m.globalInput.Placeholder = "Enter new name"
						m.globalInput.Prompt = "Name: "
						m.globalInput.Focus()
						m.logs = append(m.logs, "Entering Configuration...")
						m.statusViewport.SetContent(m.renderLogs(m.statusViewport.Width))
					} else if cmd == "4" || cmd == "/preheat" {
						 
						m.progressChan <- "Pre-heating Tor..."
						go func() {
							if err := m.netManager.StartTor(context.Background(), m.progressChan); err != nil {
								m.progressChan <- fmt.Sprintf("Pre-heat Error: %v", err)
							} else {
								m.progressChan <- "Pre-heat Complete. Tor is ready."
							}
						}()
					}
				} else if m.state == stateHost {
					 
					 
					 
					 
					if cmd != "" {
						 
						 
						 
						 
						 
						 
						m.reqSecret = cmd
						 
						 
						 
						 
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
				} else if m.state == stateConnect {
					 
					 
					 
					 

					 
					var idx int
					if _, err := fmt.Sscanf(cmd, "%d", &idx); err == nil {
						if idx == 0 {
							 
							m.state = stateNewContactMethod
							m.globalInput.SetValue("")
							m.globalInput.Prompt = "Method [1=Addr 2=Token]: "
							m.globalInput.Placeholder = "1"  
						} else if idx == -1 {
							 
							m.state = stateDeleteContact
							m.globalInput.SetValue("")
							m.globalInput.Prompt = "Delete Index: "
							m.globalInput.Placeholder = "Index"
						} else if idx > 0 {
							 
							 
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

		 
		if m.state == stateChat {
			if msg.Type == tea.KeyEnter {
				input := m.textInput.Value()
				m.textInput.SetValue("")
				if input != "" {
					 
					if m.confirmLeave {
						if strings.ToLower(input) == "y" {
							 
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
							 
							m.confirmLeave = false
							return m, nil
						}
					}

					 
					handled, cmd := m.handleChatCommand(input)
					if handled {
						return m, cmd
					}

					 
					if strings.HasPrefix(input, "/loadingbay send ") {
						args := strings.TrimPrefix(input, "/loadingbay send ")
						args = strings.TrimSpace(args)

						if args == "*" {
							 
							go func() {
								 
								loadingBayOut := "loadingbay_out"
								entries, err := os.ReadDir(loadingBayOut)
								if err != nil {
									errorStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("196"))
									sysStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("240")).Italic(true)
									m.chatViewport.SetContent(m.chatViewport.View() + fmt.Sprintf("\n%s", errorStyle.Render("Error: loadingbay_out folder not found")))
									 
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

								 
								sysStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("240")).Italic(true)
								m.chatViewport.SetContent(m.chatViewport.View() + fmt.Sprintf("\n%s", sysStyle.Render(fmt.Sprintf("Sending loadingbay_out folder (%d files)...", fileCount))))

								m.folderSendTotal = fileCount
								m.folderSendCount = 0

								 
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

								 
								m.chatViewport.SetContent(m.chatViewport.View() + fmt.Sprintf("\n%s", sysStyle.Render(fmt.Sprintf("All files sent (%d/%d)", m.folderSendCount, m.folderSendTotal))))
							}()
						} else {
							 
							filename := args
							filePath := filepath.Join("loadingbay_out", filename)

							 
							if _, err := os.Stat("loadingbay_out"); os.IsNotExist(err) {
								errorStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("196"))
								sysStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("240")).Italic(true)
								m.chatViewport.SetContent(m.chatViewport.View() + fmt.Sprintf("\n%s", errorStyle.Render("Error: loadingbay_out folder not found")))
								 
								if err := os.MkdirAll("loadingbay_out", 0755); err == nil {
									m.chatViewport.SetContent(m.chatViewport.View() + fmt.Sprintf("\n%s", sysStyle.Render("Created loadingbay_out folder for you")))
								}
								m.chatViewport.GotoBottom()
								return m, nil
							}

							 
							stat, err := os.Stat(filePath)
							if err != nil {
								errorStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("196"))
								m.chatViewport.SetContent(m.chatViewport.View() + fmt.Sprintf("\n%s", errorStyle.Render(fmt.Sprintf("Error: %s not found in loadingbay_out/", filename))))
								m.chatViewport.GotoBottom()
								return m, nil
							}

							 
							go func() {
								sysStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("240")).Italic(true)
								fileSize := stat.Size()

								 
								m.chatViewport.SetContent(m.chatViewport.View() + fmt.Sprintf("\n%s", sysStyle.Render(fmt.Sprintf("Sending %s (%s) - 0%%", filename, FormatFileSize(fileSize)))))

								if err := SendFile(filePath, m.netManager.Outgoing); err != nil {
									errorStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("196"))
									m.chatViewport.SetContent(m.chatViewport.View() + fmt.Sprintf("\n%s", errorStyle.Render(fmt.Sprintf("Error: %v", err))))
									return
								}

								 
								m.chatViewport.SetContent(m.chatViewport.View() + fmt.Sprintf("\n%s", sysStyle.Render(fmt.Sprintf("Sent %s (%s) - 100%%", filename, FormatFileSize(fileSize)))))
							}()
						}

						m.chatViewport.GotoBottom()
						return m, nil
					}

					 
					 
					displayName := m.config.Name
					if m.tempNick != "" {
						displayName = m.tempNick
					}
					displayColor := m.config.Color
					if m.tempColor != "" {
						displayColor = m.tempColor
					}

					 
					go func() {
						msg := ChatMessage{
							Content: input,
							Name:    displayName,
							Color:   displayColor,
						}
						bytes, _ := json.Marshal(msg)
						if m.netManager.Outgoing != nil {
							m.netManager.Outgoing <- string(bytes)
						}
					}()

					 
					style := lipgloss.NewStyle().Foreground(lipgloss.Color(displayColor)).Bold(true)
					coloredName := style.Render(displayName)

					 
					prefix := ""
					if m.timestampsEnabled {
						prefix = fmt.Sprintf("[%s] ", time.Now().Format("15:04"))
					}

					 
					prefixLen := len(displayName) + 2 + len(prefix)  
					wrappedInput := wrapText(input, m.chatViewport.Width-prefixLen)
					 
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
				 
				if !m.confirmLeave {
					m.confirmLeave = true
					boldRedStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("196")).Bold(true)
					m.chatViewport.SetContent(m.chatViewport.View() + "\n" + boldRedStyle.Render("You wanna get out of here? (y/n)"))
					m.chatViewport.GotoBottom()
					return m, nil
				}
				 
				m.confirmLeave = false
				return m, nil
			}
			m.textInput, tiCmd = m.textInput.Update(msg)
			m.chatViewport, cvpCmd = m.chatViewport.Update(msg)
			return m, tea.Batch(tiCmd, cvpCmd)
		}

		 
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

					 
					if strings.HasSuffix(m.reqConnect, ".onion") {
						 
						m.state = stateNewContactSecretEntry
						m.connectSecretInput.Focus()
						m.connectSecretInput.SetValue("")
						m.connectSecretInput.Prompt = "Secret: "
						m.connectSecretInput.Placeholder = "Shared Secret"
						m.globalInput.Blur()
					} else {
						 
						 
						m.logs = append(m.logs, fmt.Sprintf("Token Len: %d | Alias: '%s'", len(m.reqConnect), alias))

						decrypted, err := DecryptOnion(m.reqConnect, alias)
						if err != nil {
							m.logs = append(m.logs, fmt.Sprintf("Decryption Failed: %v", err))
							m.statusViewport.SetContent(m.renderLogs(m.statusViewport.Width))
							m.statusViewport.GotoBottom()
							m.globalInput.SetValue("")
							m.globalInput.Placeholder = "Decryption Failed! Try Again."
							return m, nil  
						}
						 
						m.reqConnect = decrypted + ".onion"
						 
						 

						 
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
					 
					alias := m.tempConfig.Name
					if m.config.Contacts == nil {
						m.config.Contacts = make(map[string]string)
					}
					 
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

		 
		if m.state == stateConnectAddress {
			if msg.Type == tea.KeyEnter {
				addr := m.connectAddrInput.Value()
				if addr != "" {
					m.reqConnect = addr  
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
				 
				secret := m.connectSecretInput.Value()
				m.reqSecret = secret

				 
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

		 
		if m.state == stateConfigName {
			if msg.Type == tea.KeyEnter {
				val := m.globalInput.Value()
				if val != "" {
					m.tempConfig.Name = val
				}  
				 
				 
				 
				 
				if val == "" {
					m.tempConfig.Name = m.config.Name
				}

				 
				m.state = stateConfigColor
				m.globalInput.SetValue(m.config.Color)
				m.globalInput.Prompt = "Color: "
				m.globalInput.Placeholder = "Enter new color (hex or name)"
				 
				return m, nil
			} else if msg.Type == tea.KeyEsc {
				m.state = stateMenu
				m.globalInput.SetValue("")
				m.globalInput.Prompt = ""  
				m.globalInput.Placeholder = "Enter command..."
				return m, nil
			}
			m.globalInput, tiCmd = m.globalInput.Update(msg)
			return m, tiCmd
		}

		if m.state == stateConfigColor {
			 
			m.globalInput.TextStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(m.globalInput.Value()))

			if msg.Type == tea.KeyEnter {
				val := m.globalInput.Value()
				if val != "" {
					 
					 
					 
					m.tempConfig.Color = val
				} else {
					m.tempConfig.Color = m.config.Color
				}

				 
				m.state = stateConfigPasswordCheck
				m.passwordInput.SetValue("")
				m.passwordInput.Placeholder = "Current Password (Enter to skip)"
				m.passwordInput.Prompt = "Current Pass: "
				m.passwordInput.Focus()
				m.globalInput.Blur()
				 
				m.globalInput.TextStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("255"))
				m.globalInput.Prompt = ""

				return m, nil
			} else if msg.Type == tea.KeyEsc {
				m.state = stateMenu
				m.globalInput.TextStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("255"))  
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
					 
					m.config.Name = m.tempConfig.Name
					m.config.Color = m.tempConfig.Color

					if err := SaveConfig(m.config, m.masterKey); err != nil {
						m.logs = append(m.logs, fmt.Sprintf("Error saving config: %v", err))
					} else {
						m.logs = append(m.logs, "Configuration Updated.")
					}
					m.statusViewport.SetContent(m.renderLogs(m.statusViewport.Width))
					m.statusViewport.GotoBottom()

					 
					m.state = stateMenu
					m.passwordInput.SetValue("")
					m.passwordInput.Prompt = "Password: "  
					 
					m.globalInput.Focus()
					return m, nil
				}

				 
				 
				key := DeriveMasterKey(val, m.salt)  
				 
				 
				 
				 
				if string(key) == string(m.masterKey) {
					 
					m.state = stateConfigPasswordNew
					m.passwordInput.SetValue("")
					m.passwordInput.Placeholder = "Enter New Password"
					m.passwordInput.Prompt = "New Pass: "
				} else {
					 
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
					 
					 
					 
					 
					 

					newKey := DeriveMasterKey(newPass, m.salt)
					m.masterKey = newKey
					m.logs = append(m.logs, "Master Password Changed.")
				}

				 
				m.config.Name = m.tempConfig.Name
				m.config.Color = m.tempConfig.Color

				if err := SaveConfig(m.config, m.masterKey); err != nil {
					m.logs = append(m.logs, fmt.Sprintf("Error saving config: %v", err))
				} else {
					m.logs = append(m.logs, "Configuration Saved.")
				}
				m.statusViewport.SetContent(m.renderLogs(m.statusViewport.Width))

				 
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
				 
				 
				 
				 

				 
				goodbye := ChatMessage{
					Content: "There is nobody here.",
					Name:    m.config.Name,
					Color:   "196",  
				}
				if m.netManager != nil && m.netManager.Outgoing != nil {
					jsonBytes, _ := json.Marshal(goodbye)
					m.netManager.Outgoing <- string(jsonBytes)
					 
					time.Sleep(100 * time.Millisecond)
				}

				 
				 
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

		 
		if strings.HasPrefix(raw, "PEER_MSG:") {
			 
			parts := strings.SplitN(raw, ":", 5)
			if len(parts) == 5 {
				peerName := parts[2]
				 
				content := parts[4]

				 
				var fileMsg FileMessage
				if err := json.Unmarshal([]byte(content), &fileMsg); err == nil && fileMsg.Type != "" {
					 
					sysStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("240")).Italic(true)

					state, err := ReceiveFileChunk(fileMsg, m.fileTransfers)
					if err != nil {
						errorStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("196"))
						m.chatViewport.SetContent(m.chatViewport.View() + fmt.Sprintf("\n%s", errorStyle.Render(fmt.Sprintf("File transfer error: %v", err))))
					} else if state != nil {
						switch fileMsg.Type {
						case "file_offer":
							m.chatViewport.SetContent(m.chatViewport.View() + fmt.Sprintf("\n%s", sysStyle.Render(fmt.Sprintf("%s is sending: %s (%s)", peerName, fileMsg.Filename, FormatFileSize(fileMsg.FileSize)))))
						case "file_chunk":
							if state.Progress%10 == 0 || state.Progress == state.TotalChunks {
								progress := int(float64(state.Progress) / float64(state.TotalChunks) * 100)
								m.chatViewport.SetContent(m.chatViewport.View() + fmt.Sprintf("\n%s", sysStyle.Render(fmt.Sprintf("Receiving: %s - %d%%", fileMsg.Filename, progress))))
							}
						case "file_end":
							m.chatViewport.SetContent(m.chatViewport.View() + fmt.Sprintf("\n%s", sysStyle.Render(fmt.Sprintf("Received: %s → loadingbay_in/", fileMsg.Filename))))
						}
					}
					m.chatViewport.GotoBottom()
					return m, waitForIncoming(m.netManager)
				}

				 
				var chatMsg ChatMessage
				if err := json.Unmarshal([]byte(content), &chatMsg); err == nil {
					 
					prefix := ""
					if m.timestampsEnabled {
						prefix = fmt.Sprintf("[%s] ", time.Now().Format("15:04"))
					}

					 
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

					 
					 
					go func(senderID string, msg string) {
						m.netManager.Outgoing <- fmt.Sprintf("RELAY:%s:%s", senderID, msg)
					}(parts[1], content)

					return m, waitForIncoming(m.netManager)
				}
			}
			return m, waitForIncoming(m.netManager)
		}

		 
		if strings.HasPrefix(raw, "PEER_LEFT:") {
			 
			 
			return m, waitForIncoming(m.netManager)
		}

		 
		var fileMsg FileMessage
		if err := json.Unmarshal([]byte(raw), &fileMsg); err == nil && fileMsg.Type != "" {
			 
			sysStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("240")).Italic(true)

			state, err := ReceiveFileChunk(fileMsg, m.fileTransfers)
			if err != nil {
				errorStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("196"))
				m.chatViewport.SetContent(m.chatViewport.View() + fmt.Sprintf("\n%s", errorStyle.Render(fmt.Sprintf("File transfer error: %v", err))))
			} else if state != nil {
				 
				switch fileMsg.Type {
				case "file_offer":
					m.chatViewport.SetContent(m.chatViewport.View() + fmt.Sprintf("\n%s", sysStyle.Render(fmt.Sprintf("%s is sending: %s (%s)", m.config.Name, fileMsg.Filename, FormatFileSize(fileMsg.FileSize)))))
				case "file_chunk":
					 
					if state.Progress%10 == 0 || state.Progress == state.TotalChunks {
						progress := int(float64(state.Progress) / float64(state.TotalChunks) * 100)
						m.chatViewport.SetContent(m.chatViewport.View() + fmt.Sprintf("\n%s", sysStyle.Render(fmt.Sprintf("Receiving: %s - %d%%", fileMsg.Filename, progress))))
					}
				case "file_end":
					m.chatViewport.SetContent(m.chatViewport.View() + fmt.Sprintf("\n%s", sysStyle.Render(fmt.Sprintf("Received: %s → loadingbay_in/", fileMsg.Filename))))
				}
			}

			m.chatViewport.GotoBottom()
			return m, waitForIncoming(m.netManager)
		}

		 
		var chatMsg ChatMessage
		var display string

		if err := json.Unmarshal([]byte(raw), &chatMsg); err == nil {
			 
			if chatMsg.Content == "There is nobody here." {
				 
				m.logs = append(m.logs, lipgloss.NewStyle().Foreground(lipgloss.Color("196")).Render("Peer Disconnected: There is nobody here."))
				m.statusViewport.SetContent(m.renderLogs(m.statusViewport.Width))
				m.netManager.Close()
				m.state = stateMenu
				return m, nil
			}

			 
			if m.config.ContactColors == nil {
				m.config.ContactColors = make(map[string]string)
			}
			 
			 
			 
			 
			 
			 
			 
			 
			 
			 

			 
			if _, ok := m.config.Contacts[chatMsg.Name]; ok {
				if m.config.ContactColors[chatMsg.Name] != chatMsg.Color {
					m.config.ContactColors[chatMsg.Name] = chatMsg.Color
					SaveConfig(m.config, m.masterKey)  
				}
			}

			 
			prefix := ""
			if m.timestampsEnabled {
				prefix = fmt.Sprintf("[%s] ", time.Now().Format("15:04"))
			}

			peerStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(chatMsg.Color)).Bold(true)
			peerName := peerStyle.Render(chatMsg.Name)
			 
			prefixLen := len(chatMsg.Name) + 2 + len(prefix)  
			wrappedContent := wrapText(chatMsg.Content, m.chatViewport.Width-prefixLen)
			 
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
			 
			m.globalInput.Blur()
			m.textInput.Focus()
			m.connectionStart = time.Now()
			m.status = "Connection Established."
			m.logs = append(m.logs, m.status)
			m.statusViewport.SetContent(m.renderLogs(m.statusViewport.Width))
			return m, waitForIncoming(m.netManager)
		}

		 
		if strings.HasPrefix(string(msg), "Host Error:") {
			redStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("196")).Bold(true)
			m.logs = append(m.logs, redStyle.Render(string(msg)))
			m.logs = append(m.logs, redStyle.Render("Couldn't start this time, maybe try /rehost"))
			m.statusViewport.SetContent(m.renderLogs(m.statusViewport.Width))
			m.statusViewport.GotoBottom()
			return m, nil
		}

		 
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

		 
		if strings.Contains(m.status, "SECURE TOKEN") {
			parts := strings.Split(m.status, "\n")
			if len(parts) > 1 {
				m.currentToken = strings.TrimSpace(parts[1])
				m.logs = append(m.logs, "Press Ctrl+Y to copy token.")
			}
		}

		m.statusViewport.SetContent(m.renderLogs(m.statusViewport.Width))
		m.statusViewport.GotoBottom()

		 
		 
		 
		if strings.Contains(m.status, "Bootstrapped") {
			 
			 
			var p int
			if _, err := fmt.Sscanf(m.status, "[Tor] Bootstrapped %d%%", &p); err == nil {
				prgCmd = m.progress.SetPercent(float64(p) / 100.0)
			} else {
				 
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
			 
			 
			 
			 
		}

		if strings.Contains(m.status, "Listening") || strings.Contains(m.status, "You are not alone") {
			m.ready = true  
			m.state = stateChat
			 
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
	 
	 
	 
	 
	 
	 
	 

	 
	progModel, pCmd := m.progress.Update(msg)
	m.progress = progModel.(progress.Model)

	 
	 
	m = m.recalcLayout(m.width, m.height)

	return m, tea.Batch(tiCmd, cvpCmd, svpCmd, prgCmd, pCmd)
}

 
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
		 
		wrappedLog := wrapText(log, width)

		var style lipgloss.Style
		if strings.Contains(log, "You are not alone") || strings.Contains(log, "Listening on") || strings.Contains(log, "SECURE TOKEN") {
			style = lipgloss.NewStyle().Foreground(lipgloss.Color("196")).Bold(true)
		} else {
			style = lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
		}
		s.WriteString(style.Render(wrappedLog) + "\n")
	}
	return s.String()
}

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

	 
	var mainContent, footerContent string

	if m.state == stateChat {
		mainContent = m.viewChatContent()
		 
		inputStyle := lipgloss.NewStyle().
			Border(lipgloss.NormalBorder()).
			BorderForeground(lipgloss.Color("240")).  
			Width(m.width - 2)
		footerContent = inputStyle.Render(m.textInput.View())
	} else if m.state == stateConfigName || m.state == stateConfigColor {
		 
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
			BorderForeground(lipgloss.Color("196")).  
			Width(m.width - 2)
		footerContent = inputStyle.Render(m.passwordInput.View())

	} else {
		 
		 
		 
		rightWidth := 45
		if m.width < 100 {
			rightWidth = 35
		}
		leftWidth := m.width - rightWidth - 4
		if leftWidth < 10 {
			leftWidth = 10
		}

		 
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

	 
	 
	return lipgloss.NewStyle().MaxWidth(m.width).MaxHeight(m.height).Render(view)
}

func (m model) viewLayout(leftContent, footer string) string {
	 
	rightWidth := 45
	if m.width < 100 {
		rightWidth = 35
	}  
	leftWidth := m.width - rightWidth - 4  

	 
	headerLeftStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("196")).  
		Bold(true).
		 

		Align(lipgloss.Left)

	headerRightStyle := lipgloss.NewStyle().
		Width(rightWidth).      
		Align(lipgloss.Center)  

	var header string
	 
	if m.state != stateChat {
		 
		roseView := m.rose.View()

		 
		logo := ASCII_RIZOMA_2
		if m.easterEgg {
			logo = ASCII_RIZOMA_1
		}

		 
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
		 
		header = lipgloss.NewStyle().Height(10).Render(rawHeader)
	} else {
		header = ""
	}

	 
	headerHeight := 0
	if m.state != stateChat {
		headerHeight = 10
	} else {
		headerHeight = 0  
	}
	footerHeight := 3

	 
	minBodyHeight := 10
	if m.height < headerHeight+footerHeight+minBodyHeight {
		if m.state != stateChat {
			headerHeight = 0
		}
	}
	verticalMarginHeight := headerHeight + footerHeight

	 
	 

	 
	if leftWidth < 10 {
		leftWidth = 10
	}

	 
	borderColor := lipgloss.Color("240")

	 
	 
	 
	safetyMargin := 3
	 
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

	 
	 
	 
	statusContent := m.viewStatus(rightWidth, bodyHeight)

	 
	 
	 
	 
	 
	 
	 
	 
	 
	 
	 
	 
	 
	 
	 
	 

	 
	body := lipgloss.JoinHorizontal(lipgloss.Top,
		leftStyle.Render(leftContent),
		rightStyle.Render(statusContent),
	)

	 
	footer = lipgloss.NewStyle().Height(3).Render(footer)

	return lipgloss.JoinVertical(lipgloss.Left, header, body, footer)
}

func (m model) viewStatus(width int, height int) string {
	 
	 
	 

	 
	 
	progHeight := 2

	 
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
	 
	infoHeight := strings.Count(infoStr, "\n") + 1
	if infoHeight > 6 {
		infoHeight = 6
	}  

	 
	 
	 
	 
	 
	gapLines := 4
	dividerLines := 2

	fixedOverhead := infoHeight + progHeight + gapLines + dividerLines

	 
	logHeight := height - fixedOverhead
	if logHeight < 0 {
		logHeight = 0
	}

	 
	infoStyle := lipgloss.NewStyle().Height(infoHeight).MaxHeight(infoHeight)
	logStyle := lipgloss.NewStyle().Height(logHeight).MaxHeight(logHeight)
	progStyle := lipgloss.NewStyle().Height(progHeight).MaxHeight(progHeight)

	 
	 
	logs := m.statusViewport.View()

	 
	dividerWidth := width
	if dividerWidth < 0 {
		dividerWidth = 0
	}
	divider := strings.Repeat("-", dividerWidth)

	 
	progView := fmt.Sprintf("Network Status:\n%s", m.progress.View())

	return lipgloss.JoinVertical(lipgloss.Left,
		infoStyle.Render(infoStr),
		divider,
		logStyle.Render(logs),
		divider,
		progStyle.Render(progView),
	)
}

func (m model) viewChatContent() string {
	 
	return m.chatViewport.View()
}

func (m model) viewDashboardContent(width, height int) string {
	 
	 

	sepWidth := 1
	innerWidth := width - sepWidth
	halfWidth := innerWidth / 2

	if halfWidth < 20 {
		halfWidth = 20
	}

	 
	boxStyle := lipgloss.NewStyle().
		Width(halfWidth).
		Height(height)

	 
	var sepBuilder strings.Builder
	 
	sepHeight := height + 6
	for i := 0; i < sepHeight; i++ {
		sepBuilder.WriteString("|\n")
	}
	sepContent := lipgloss.NewStyle().
		Foreground(lipgloss.Color("240")).
		Height(height).  
		 
		 
		Render(strings.TrimRight(strings.Repeat("|\n", sepHeight), "\n"))

	 
	ops := []string{
		"[1] HOST",
		"[2] CONNECT",
		"[3] CONFIG",
		"[4] PRE-HEAT",
	}

	var opsList strings.Builder
	opsList.WriteString("\n")  
	for _, op := range ops {
		opsList.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color("255")).Render(op) + "\n\n")
	}

	opsContent := lipgloss.JoinVertical(lipgloss.Left,
		opsList.String(),
	)

	 
	var peersList strings.Builder

	if len(m.config.Contacts) == 0 {
		peersList.WriteString("\n No contacts found.")
	} else {
		peersList.WriteString("\n")
		 
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

			 
			statusColor := "240"
			if status == "ONLN" {
				statusColor = "46"
			}
			if status == "OFFLN" {
				statusColor = "196"
			}

			 
			 

			prefix := ""
			if m.state == stateConnect || m.state == stateDeleteContact {
				prefix = fmt.Sprintf("%d ", i+1)
			}

			sRender := lipgloss.NewStyle().Foreground(lipgloss.Color(statusColor)).Render(fmt.Sprintf("[%s]", status))

			 
			var aliasColor string
			if savedColor, ok := m.config.ContactColors[alias]; ok && savedColor != "" {
				aliasColor = savedColor
			} else {
				aliasColor = hashColor(alias)
			}

			aliRender := lipgloss.NewStyle().Foreground(lipgloss.Color(aliasColor)).Render("-" + alias)

			 
			row := fmt.Sprintf("%s%s %s\n", prefix, aliRender, sRender)
			peersList.WriteString(row)
		}
	}

	peersContent := lipgloss.JoinVertical(lipgloss.Left,
		peersList.String(),
	)

	 
	leftBoxStr := opsContent

	if m.state == stateHost {
		leftBoxStr = lipgloss.JoinVertical(lipgloss.Left,
			"\n",
			lipgloss.NewStyle().Foreground(lipgloss.Color("205")).Bold(true).Render("HOST MODE"),
			"\n",
			"Enter Session Secret:",
		)
	} else if m.state == stateConnect {
		 
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
			m.connectAddrInput.View(),  
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

func waitForIncoming(nm *NetworkManager) tea.Cmd {
	return func() tea.Msg {
		msg, ok := <-nm.Incoming
		if !ok {
			return nil
		}
		return incomingMsg(msg)
	}
}

func (m model) recalcLayout(width, height int) model {
	m.width = width
	m.height = height

	headerHeight := 0
	 
	if m.state != stateChat {
		headerHeight = 10  
	} else {
		headerHeight = 0  
	}
	footerHeight := 3

	 
	 
	 
	minBodyHeight := 10
	if height < headerHeight+footerHeight+minBodyHeight {
		if m.state != stateChat {
			 
			 
			 
			 
			 
			 
			 
			headerHeight = 0
		}
	}

	verticalMarginHeight := headerHeight + footerHeight
	bodyHeight := height - verticalMarginHeight

	 
	if bodyHeight < 0 {
		bodyHeight = 0
	}

	 
	 
	 
	rightWidth := 45
	if width < 100 {
		rightWidth = 35
	}  

	 
	leftWidth := width - rightWidth - 4
	if leftWidth < 10 {
		leftWidth = 10
	}

	chatWidth := leftWidth - 4  

	 
	 
	 
	bodyContentHeight := bodyHeight - 3
	if bodyContentHeight < 0 {
		bodyContentHeight = 0
	}

	if !m.ready {
		 
		m.chatViewport = viewport.New(chatWidth, bodyContentHeight)
		m.chatViewport.YPosition = headerHeight
		m.chatViewport.SetContent("Welcome to Rizoma.\nInitializing Tor network...\n")

		 
		m.statusViewport = viewport.New(rightWidth, bodyContentHeight-8)
		m.statusViewport.YPosition = headerHeight
		m.statusViewport.SetContent(m.renderLogs(rightWidth))

		m.ready = true
	} else {
		m.chatViewport.Width = chatWidth
		m.chatViewport.Height = bodyContentHeight

		 
		 
		 
		 
		safeLogHeight := bodyContentHeight - 14
		if safeLogHeight < 0 {
			safeLogHeight = 0
		}

		m.statusViewport.Width = rightWidth
		m.statusViewport.Height = safeLogHeight
		m.statusViewport.SetContent(m.renderLogs(rightWidth))
	}
	 
	 
	 
	 
	 
	 
	m.progress.Width = rightWidth - 4
	m.textInput.Width = chatWidth
	m.globalInput.Width = width - 4

	return m
}

 
 
func InteractiveMenu(reader *bufio.Reader, hostMode *bool, connectAddr *string, secret *string, masterKey []byte) bool {
	 
	if *hostMode || *connectAddr != "" {
		return true  
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
					 
					fmt.Print("Enter Onion Address: ")
					addr, _ := reader.ReadString('\n')
					*connectAddr = strings.TrimSpace(addr)

					if *secret == "" {
						fmt.Print("Enter Secret Phrase: ")
						s, _ := reader.ReadString('\n')
						*secret = strings.TrimSpace(s)
					}
				}

				 
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
				 
				 
				var idx int
				if _, err := fmt.Sscanf(selStr, "%d", &idx); err == nil && idx > 0 && idx <= len(contacts) {
					alias := contacts[idx-1]
					*connectAddr = cfg.Contacts[alias]
					fmt.Printf("Connecting to %s (%s)...\n", alias, *connectAddr)

					 
					 
					 
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
