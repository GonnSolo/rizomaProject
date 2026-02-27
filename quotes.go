package main

import (
	"fmt"
	"math/rand"
)

// Quote represents a literary or cultural quote with its associated author and year.
type Quote struct {
	Text   string
	Author string
	Year   string // Year is a string to accommodate various formats (e.g., "19 BBY", "Unknown").
}

// GetRandomQuote selects a quote from the curated collection and returns it as a formatted string.
func GetRandomQuote() string {
	q := coolQuotes[rand.Intn(len(coolQuotes))]
	return fmt.Sprintf("\"%s\" - %s (%s)", q.Text, q.Author, q.Year)
}

// coolQuotes is a curated list of quotes intended to inspire or decorate the application's interface.
var coolQuotes = []Quote{
	{Text: "Maybe you should try /bradbury while connected with others ;)", Author: "GonnSolo", Year: "2026"},
	{Text: "What I am about to do has not been approved by the Vatican.", Author: "Priest", Year: "1986"},
	{Text: "Beyond the shadow you settle for, there's a miracle, illuminated.", Author: "Alan Wake", Year: "2010"},
	{Text: "The right man in the wrong place can make all the difference in the world.", Author: "The G-Man", Year: "20XX"},
	{Text: "We built those walls, you and I. Such elegant spires. A beautiful prison.", Author: "Riven of a Thousand Voices", Year: "Unknown"},
	{Text: "I've seen things you people wouldn't believe. Attack ships on fire off the shoulder of Orion. I watched C-beams glitter in the dark near the Tannhäuser Gate. All those moments will be lost in time, like tears in rain.", Author: "Roy Batty", Year: "2019"},
	{Text: "We used to look up at the sky and wonder at our place in the stars, now we just look down and worry about our place in the dirt.", Author: "Cooper", Year: "2067"},
	{Text: "Luminous beings are we, not this crude matter.", Author: "Yoda", Year: "3 ABY"},
	{Text: "If you only read the books that everyone else is reading, you can only think what everyone else is thinking.", Author: "Haruki Murakami", Year: "1987"},
	{Text: "Often, when we guess at others' motives, we reveal only our own.", Author: "Mara Sov", Year: "Unknown"},
}
