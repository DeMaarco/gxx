package ui

import (
	"fmt"
	"io"
	"strings"
)

const (
	PermissionAsk = "ask"
	cursorUp      = "\x1b[1A"
	clearLine     = "\r\x1b[2K"
)

func formatHeader(settings REPLSettings) string {
	version := strings.TrimSpace(settings.Version)
	if version == "" {
		version = "dev"
	}
	name := paint(settings.Color, bold, "gxx")
	badge := paint(settings.Color, dim, version)
	diamond := paint(settings.Color, bold+magenta, markGxx)
	return diamond + " " + name + "  " + badge
}

func formatStatus(settings REPLSettings) string {
	model := strings.TrimSpace(settings.Model)
	if model == "" {
		model = "model"
	}
	permission := strings.TrimSpace(settings.PermissionMode)
	if permission == "" {
		permission = PermissionAsk
	}
	effort := strings.TrimSpace(settings.Effort)
	if effort == "" {
		effort = "medium"
	}
	line := model + " · " + permission + " · " + effort + " · " + orDefault(settings.Context, "272k")
	if settings.Fast {
		line += " · fast"
	}
	return paint(settings.Color, dim, line)
}

func writeHeader(writer io.Writer, settings REPLSettings) error {
	_, err := fmt.Fprintln(writer, formatHeader(settings))
	return err
}

func writeChrome(writer io.Writer, settings REPLSettings) error {
	header := formatHeader(settings)
	status := formatStatus(settings)
	if settings.Color {
		if _, err := fmt.Fprintf(writer, "%s\n> \n%s%s\r> ", header, status, cursorUp); err != nil {
			return err
		}
		return nil
	}
	_, err := fmt.Fprintf(writer, "%s\n>\n%s\n", header, status)
	return err
}

func clearStatusLine(writer io.Writer, color bool) {
	if color {
		_, _ = io.WriteString(writer, clearLine)
	}
}

func WriteAskHeader(writer io.Writer, settings REPLSettings) error {
	if _, err := fmt.Fprintf(writer, "%s\n%s\n\n", formatHeader(settings), formatStatus(settings)); err != nil {
		return err
	}
	return nil
}
