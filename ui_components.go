package main

import (
	"math/rand"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// TickMsg represents a periodic animation event.
type TickMsg time.Time

// tickCmd returns a command that triggers a TickMsg every 50 milliseconds.
func tickCmd() tea.Cmd {
	return tea.Tick(time.Millisecond*50, func(t time.Time) tea.Msg {
		return TickMsg(t)
	})
}

// roseFrames defines the ASCII animation frames for a blooming rose.
var roseFrames = []string{
	`
      
      
  .   
      `,
	`
      
   .  
  .@. 
   |  `,
	`
   ,  
  \w/ 
  .@. 
   |  `,
	`
  _v_ 
 ({ })
  /|\ 
   |  `,
	`
  _v_ 
 ({%})
  /|\ 
   |  `,
}

// Animation state constants for the Rose component.
const (
	AnimBlooming = iota // Rose is currently opening
	AnimWait            // Rose is fully open, waiting before root growth
	AnimRoots           // Roots are actively growing downwards
)

// RoseModel manages the state and logic for the decorative blooming rose animation.
type RoseModel struct {
	frameIndex    int
	maxFrames     int
	width         int
	tickCount     int
	ticksPerFrame int

	animState int
	waitTicks int
	rootLines []string // Accumulated ASCII lines representing the roots
	rootTips  []int    // Current X coordinates for active root tip growth
	rnd       *rand.Rand
	viewportH int // Height of the visible animation area
}

// NewRoseModel initializes a RoseModel with default animation settings.
func NewRoseModel() RoseModel {
	src := rand.NewSource(time.Now().UnixNano())
	return RoseModel{
		frameIndex:    0,
		maxFrames:     len(roseFrames),
		ticksPerFrame: 20, // Duration of each blooming frame (approx 1s)
		animState:     AnimBlooming,
		rnd:           rand.New(src),
		viewportH:     8,
	}
}

// Tick advances the animation state based on elapsed time.
func (r *RoseModel) Tick() {
	r.tickCount++

	if r.animState == AnimBlooming {
		if r.tickCount >= r.ticksPerFrame {
			r.tickCount = 0
			if r.frameIndex < r.maxFrames-1 {
				r.frameIndex++
			} else {
				r.animState = AnimWait
				r.waitTicks = 0
			}
		}
	} else if r.animState == AnimWait {
		// Pause for 5 seconds before starting root growth.
		if r.tickCount >= 100 {
			r.animState = AnimRoots
			r.tickCount = 0
			r.rootTips = []int{7} // Start roots from the base of the stem
			r.rootLines = []string{}
		}
	} else if r.animState == AnimRoots {
		// Roots grow slowly: one new line every 2 seconds.
		if r.tickCount >= 40 {
			r.tickCount = 0
			r.growRoots()
		}
	}
}

// growRoots generates the next line of root expansion based on probabilistic branching.
func (r *RoseModel) growRoots() {
	row := make([]rune, 15)
	for i := range row {
		row[i] = ' '
	}

	newTips := []int{}

	// Helper to safely add tips
	addTip := func(idx int, ch rune) {
		if idx >= 0 && idx < 15 {
			row[idx] = ch
			newTips = append(newTips, idx)
		}
	}

	for _, x := range r.rootTips {
		// Always try to continue downwards first
		grown := false

		// 70% probability to grow straight down.
		if r.rnd.Float32() < 0.7 {
			addTip(x, '|')
			grown = true
		}

		// 30% probability to branch left.
		if r.rnd.Float32() < 0.3 {
			addTip(x-1, '/')
			grown = true
		}

		// 30% probability to branch right.
		if r.rnd.Float32() < 0.3 {
			addTip(x+1, '\\')
			grown = true
		}

		// Ensure continued growth if no branches were selected.
		if !grown {
			addTip(x, '|')
		}
	}

	// De-duplicate tip positions to prevent overlapping paths.
	seen := make(map[int]bool)
	uniqueTips := []int{}
	for _, t := range newTips {
		if !seen[t] {
			seen[t] = true
			uniqueTips = append(uniqueTips, t)
		}
	}
	r.rootTips = uniqueTips
	r.rootLines = append(r.rootLines, string(row))
}

// View returns the current visual representation of the rose animation as a string.
func (r RoseModel) View() string {
	style := lipgloss.NewStyle().Foreground(lipgloss.Color("196")).Bold(true) // Red
	stemStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("40"))         // Green
	rootStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("231"))        // White

	// 1. Get Rose Lines (Cleanly)
	rawRose := roseFrames[r.frameIndex]
	// Handle leading newline if present
	splitRose := strings.Split(rawRose, "\n")
	var roseLines []string
	for _, l := range splitRose {
		// Filter out empty lines that might be artifacts of backticks,
		// BUT we know the frames are 4 lines tall visually.
		// "   |  " length is > 0.
		if len(l) > 0 {
			roseLines = append(roseLines, l)
		}
	}
	// Verify we have 4 lines, if not, something is odd with formatting, but proceed.

	// 2. Build the "World" Buffer
	// Structure: [Empty x 3] + [Rose x 4] + [Roots...] + [Empty Padding if needed]

	// Assemble the animation buffer.
	world := make([]string, 0)
	world = append(world, "", "", "")     // Padding above
	world = append(world, roseLines...)   // Current rose frame
	world = append(world, r.rootLines...) // Accumulated root lines

	// 3. Ensure Minimum Size for Alignement
	// We want initially 1 line BELOW the rose (Total 8).
	// Initial total lines = 3 (above) + 4 (rose) = 7.
	// We want strict 8 lines total for the viewport.
	// So we need to pad the world until it has AT LEAST 8 lines.
	// This implicitly adds the "1 below" initially (was 2).
	for len(world) < 8 {
		world = append(world, "")
	}

	// 4. Calculate Viewport (Bottom 8 Lines)
	// As roots grow, world length increases > 8.
	// We slice the LAST 8 lines.
	// This causes the top (empty lines, then rose lines) to scroll off the top.
	// Calculate and slice the visible viewport window (sliding bottom).
	start := 0
	if len(world) > 8 {
		start = len(world) - 8
	}
	visible := world[start:]

	// 5. Render visible lines
	var sb strings.Builder
	for i, line := range visible {
		// We need to know WHAT this line is to style it.
		// Map viewport index 'i' back to world index.
		worldIdx := start + i

		// World Map:
		// 0, 1, 2: Empty Top
		// 3, 4, 5, 6: Rose
		// > 6: Roots (or Bottom Padding)

		lineContent := line

		if worldIdx >= 3 && worldIdx <= 6 && (worldIdx-3) < len(roseLines) {
			// Style rose and stem lines.
			// Apply stem logic
			if strings.Contains(lineContent, "|") || strings.Contains(lineContent, "/") || strings.Contains(lineContent, "\\") {
				sb.WriteString(stemStyle.Render(lineContent) + "\n")
			} else {
				sb.WriteString(style.Render(lineContent) + "\n")
			}
		} else if worldIdx > 6 {
			// Style root lines.
			// It is a Root line (or padding)
			if strings.TrimSpace(lineContent) == "" {
				sb.WriteString(lineContent + "\n") // Padding
			} else {
				sb.WriteString(rootStyle.Render(lineContent) + "\n")
			}
		} else {
			// Render top padding lines.
			// Top Empty lines
			sb.WriteString(lineContent + "\n")
		}
	}
	return sb.String()
}
