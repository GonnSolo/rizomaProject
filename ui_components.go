package main

import (
	"math/rand"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

 
type TickMsg time.Time

func tickCmd() tea.Cmd {
	return tea.Tick(time.Millisecond*50, func(t time.Time) tea.Msg {
		return TickMsg(t)
	})
}

 

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

 
const (
	AnimBlooming = iota
	AnimWait
	AnimRoots
)

type RoseModel struct {
	frameIndex    int
	maxFrames     int
	width         int
	tickCount     int
	ticksPerFrame int

	 
	animState int
	waitTicks int
	rootLines []string  
	rootTips  []int     
	rnd       *rand.Rand
	viewportH int  
}

func NewRoseModel() RoseModel {
	src := rand.NewSource(time.Now().UnixNano())
	return RoseModel{
		frameIndex:    0,
		maxFrames:     len(roseFrames),
		ticksPerFrame: 20,  
		animState:     AnimBlooming,
		rnd:           rand.New(src),
		viewportH:     8,  
	}
}

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
		 
		if r.tickCount >= 100 {
			r.animState = AnimRoots
			r.tickCount = 0
			 
			 
			r.rootTips = []int{7}
			r.rootLines = []string{}
		}
	} else if r.animState == AnimRoots {
		 
		if r.tickCount >= 40 {
			r.tickCount = 0
			r.growRoots()
		}
	}
}

func (r *RoseModel) growRoots() {
	 
	 
	row := make([]rune, 15)
	for i := range row {
		row[i] = ' '
	}

	newTips := []int{}

	 
	addTip := func(idx int, ch rune) {
		if idx >= 0 && idx < 15 {
			row[idx] = ch
			newTips = append(newTips, idx)
		}
	}

	for _, x := range r.rootTips {
		 
		grown := false

		 
		if r.rnd.Float32() < 0.7 {
			addTip(x, '|')
			grown = true
		}

		 
		if r.rnd.Float32() < 0.3 {
			addTip(x-1, '/')
			grown = true
		}

		 
		if r.rnd.Float32() < 0.3 {
			addTip(x+1, '\\')
			grown = true
		}

		 
		if !grown {
			addTip(x, '|')
		}
	}

	 
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

func (r RoseModel) View() string {
	style := lipgloss.NewStyle().Foreground(lipgloss.Color("196")).Bold(true)  
	stemStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("40"))          
	rootStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("231"))         

	 
	rawRose := roseFrames[r.frameIndex]
	 
	splitRose := strings.Split(rawRose, "\n")
	var roseLines []string
	for _, l := range splitRose {
		 
		 
		 
		if len(l) > 0 {
			roseLines = append(roseLines, l)
		}
	}
	 

	 
	 

	world := make([]string, 0)
	world = append(world, "", "", "")      
	world = append(world, roseLines...)    
	world = append(world, r.rootLines...)  

	 
	 
	 
	 
	 
	 
	for len(world) < 8 {
		world = append(world, "")
	}

	 
	 
	 
	 
	start := 0
	if len(world) > 8 {
		start = len(world) - 8
	}
	visible := world[start:]

	 
	var sb strings.Builder
	for i, line := range visible {
		 
		 
		worldIdx := start + i

		 
		 
		 
		 

		lineContent := line

		if worldIdx >= 3 && worldIdx <= 6 && (worldIdx-3) < len(roseLines) {
			 
			 
			if strings.Contains(lineContent, "|") || strings.Contains(lineContent, "/") || strings.Contains(lineContent, "\\") {
				sb.WriteString(stemStyle.Render(lineContent) + "\n")
			} else {
				sb.WriteString(style.Render(lineContent) + "\n")
			}
		} else if worldIdx > 6 {
			 
			if strings.TrimSpace(lineContent) == "" {
				sb.WriteString(lineContent + "\n")  
			} else {
				sb.WriteString(rootStyle.Render(lineContent) + "\n")
			}
		} else {
			 
			sb.WriteString(lineContent + "\n")
		}
	}
	return sb.String()
}
