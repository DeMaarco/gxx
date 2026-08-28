// Copyright 2026 DeMarco
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package ui

import (
	"fmt"
	"io"
	"strings"
)

const (
	cursorUp  = "\x1b[1A"
	clearLine = "\r\x1b[2K"
)

func formatHeader(settings REPLSettings) string {
	version := formatVersion(settings.Version)
	name := paint(settings.Color, bold, "gxx")
	badge := paint(settings.Color, dim, version)
	diamond := paint(settings.Color, bold+magenta, markGxx)
	return diamond + " " + name + "  " + badge
}

// FormatVersion prefixes a release number with v for display.
func FormatVersion(version string) string {
	return formatVersion(version)
}

func formatVersion(version string) string {
	version = strings.TrimSpace(version)
	if version == "" {
		return "dev"
	}
	if strings.EqualFold(version, "dev") {
		return version
	}
	if strings.HasPrefix(version, "v") || strings.HasPrefix(version, "V") {
		return version
	}
	return "v" + version
}

func formatStatus(settings REPLSettings) string {
	model := strings.TrimSpace(settings.Model)
	if model == "" {
		model = "model"
	}
	effort := strings.TrimSpace(settings.Effort)
	if effort == "" {
		effort = "medium"
	}
	parts := []string{
		paint(settings.Color, dim, model),
		paintPermission(settings.Color, settings.PermissionMode),
		paint(settings.Color, dim, effort),
		paint(settings.Color, dim, orDefault(settings.Context, "272k")),
		paintContextPercent(settings.Color, settings.contextUsage().Percent),
	}
	if settings.Fast {
		parts = append(parts, paint(settings.Color, dim, "fast"))
	}
	return strings.Join(parts, paint(settings.Color, dim, " · "))
}

func promptPrefix(settings REPLSettings) string {
	prefix := "> "
	if settings.Plan {
		prefix += paint(settings.Color, yellow, "plan") + " "
	}
	if settings.Eco > 0 {
		prefix += paint(settings.Color, green, ecoLabel(settings.Eco)) + " "
	}
	return prefix
}

func writeHeader(writer io.Writer, settings REPLSettings) error {
	_, err := fmt.Fprintln(writer, formatHeader(settings))
	return err
}

func writeChrome(writer io.Writer, settings REPLSettings) error {
	header := formatHeader(settings)
	status := formatStatus(settings)
	prefix := promptPrefix(settings)
	if settings.Color {
		if _, err := fmt.Fprintf(writer, "%s\n%s\n%s%s\r%s", header, prefix, status, cursorUp, prefix); err != nil {
			return err
		}
		return nil
	}
	_, err := fmt.Fprintf(writer, "%s\n%s\n%s\n", header, strings.TrimRight(prefix, " "), status)
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
