//go:build bradbury

// Package main provides the core functionality of the Rizoma application.
package main

import _ "embed"

// illustratedManPDF contains the embedded bytes of the "Illustrated Man" PDF.
// This is included in the binary when the 'bradbury' build tag is enabled.
//go:embed illustrated-man-by-ray-bradbury.pdf
var illustratedManPDF []byte
