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

package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path"
	"strconv"
	"strings"
	"time"

	"gxx/internal/agent"
	"gxx/internal/approval"
)

const (
	defaultImageModel   = "gpt-image-2"
	defaultImageTimeout = 3 * time.Minute
	maxImagePromptRunes = 32000
	maxImageBytes       = 32 * 1024 * 1024
	maxImageEdge        = 3840
	maxImagePixels      = 3840 * 2160
)

var errImageNeedsAPIKey = errors.New("image generation needs an OpenAI platform API key (not ChatGPT login); run /config or export OPENAI_API_KEY")

var gptImageModels = map[string]string{
	"gpt-image-2":            "gpt-image-2",
	"gpt-image-2-2026-04-21": "gpt-image-2-2026-04-21",
	"gpt-image-1":            "gpt-image-1",
	"gpt-image-1.5":          "gpt-image-1.5",
	"gpt-image-1-mini":       "gpt-image-1-mini",
	"chatgpt-image-latest":   "chatgpt-image-latest",
}

type ImageRequest struct {
	Prompt     string
	Model      string
	Size       string
	Quality    string
	Format     string
	Background string
}

type ImageResult struct {
	Data    []byte
	Model   string
	Size    string
	Quality string
	Format  string
}

type generateImageArgs struct {
	Prompt       string  `json:"prompt"`
	Path         string  `json:"path"`
	Model        *string `json:"model"`
	Size         *string `json:"size"`
	Quality      *string `json:"quality"`
	OutputFormat *string `json:"output_format"`
	Background   *string `json:"background"`
}

type resolvedImage struct {
	prompt     string
	path       string
	model      string
	size       string
	quality    string
	format     string
	background string
	overwrite  bool
}

func (r *Registry) generateImageSpec() toolSpec {
	return toolSpec{
		definition: agent.ToolDefinition{
			Name: "generate_image",
			Description: `Generate an image with a GPT image model and write it into the workspace.
Default model is gpt-image-2. Requires an OpenAI platform API key (OPENAI_API_KEY or /config), not ChatGPT login.
path is workspace-relative and should end in .png, .webp, .jpg, or .jpeg.
Use this only when the user needs a new image file.`,
			ReadOnly: false,
			Parameters: objectSchema(map[string]any{
				"prompt": map[string]any{
					"type":        "string",
					"description": "What the image should show.",
				},
				"path": map[string]any{
					"type":        "string",
					"description": "Workspace-relative destination, for example assets/logo.png.",
				},
				"model": map[string]any{
					"type":        []string{"string", "null"},
					"description": "GPT image model, or null for gpt-image-2.",
				},
				"size": map[string]any{
					"type":        []string{"string", "null"},
					"description": "auto, 1024x1024, 1536x1024, 1024x1536, or WIDTHxHEIGHT (divisible by 16). Null for auto.",
				},
				"quality": map[string]any{
					"type":        []string{"string", "null"},
					"description": "low, medium, high, or auto. Null for auto.",
				},
				"output_format": map[string]any{
					"type":        []string{"string", "null"},
					"description": "png, webp, or jpeg. Null infers from path, or png.",
				},
				"background": map[string]any{
					"type":        []string{"string", "null"},
					"description": "transparent, opaque, or auto. Null for auto. Transparent needs png or webp.",
				},
			}, "prompt", "path", "model", "size", "quality", "output_format", "background"),
		},
		prepare: r.prepareGenerateImage,
	}
}

func (r *Registry) prepareGenerateImage(raw json.RawMessage) (approval.Action, toolRun, error) {
	resolved, err := r.parseGenerateImage(raw)
	if err != nil {
		return approval.Action{}, nil, err
	}
	if r.generateImage == nil {
		return approval.Action{}, nil, errImageNeedsAPIKey
	}

	action := approval.Action{
		Title:   "Generate image " + resolved.path,
		Preview: approval.CapPreview(imagePreview(resolved)),
		Kind:    approval.KindWrite,
	}
	run := func(ctx context.Context) (string, error) {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		timeout := r.imageTimeout
		if timeout <= 0 {
			timeout = defaultImageTimeout
		}
		ctx, cancel := context.WithTimeout(ctx, timeout)
		defer cancel()

		reportProgressNow(ctx, resolved.model)
		result, err := r.generateImage(ctx, ImageRequest{
			Prompt:     resolved.prompt,
			Model:      resolved.model,
			Size:       resolved.size,
			Quality:    resolved.quality,
			Format:     resolved.format,
			Background: resolved.background,
		})
		if err != nil {
			return "", err
		}
		if len(result.Data) == 0 {
			return "", errors.New("image API returned empty data")
		}
		if len(result.Data) > maxImageBytes {
			return "", fmt.Errorf("generated image exceeds %d bytes", maxImageBytes)
		}
		reportProgressNow(ctx, resolved.path)
		if err := r.workspace.AtomicWrite(resolved.path, result.Data); err != nil {
			return "", err
		}
		return formatImageResult(resolved, result), nil
	}
	return action, run, nil
}

func (r *Registry) parseGenerateImage(raw json.RawMessage) (resolvedImage, error) {
	var args generateImageArgs
	if err := decodeArgs(raw, &args); err != nil {
		return resolvedImage{}, err
	}
	prompt := strings.TrimSpace(args.Prompt)
	if prompt == "" {
		return resolvedImage{}, errors.New("prompt cannot be empty")
	}
	if len([]rune(prompt)) > maxImagePromptRunes {
		return resolvedImage{}, fmt.Errorf("prompt exceeds %d characters", maxImagePromptRunes)
	}

	model, err := normalizeImageModel(optionalString(args.Model, defaultImageModel))
	if err != nil {
		return resolvedImage{}, err
	}
	size, err := normalizeImageSize(optionalString(args.Size, "auto"))
	if err != nil {
		return resolvedImage{}, err
	}
	quality, err := normalizeChoice("quality", optionalString(args.Quality, "auto"), "low", "medium", "high", "auto")
	if err != nil {
		return resolvedImage{}, err
	}
	format, err := normalizeImageFormat(optionalString(args.OutputFormat, ""))
	if err != nil {
		return resolvedImage{}, err
	}
	background, err := normalizeChoice("background", optionalString(args.Background, "auto"), "transparent", "opaque", "auto")
	if err != nil {
		return resolvedImage{}, err
	}

	dest, format, err := resolveImagePath(args.Path, format)
	if err != nil {
		return resolvedImage{}, err
	}
	clean, err := r.workspace.Clean(dest)
	if err != nil {
		return resolvedImage{}, fmt.Errorf("%s: %w", dest, err)
	}
	if clean == "." {
		return resolvedImage{}, errors.New("cannot write the workspace root")
	}
	if isSensitivePath(clean) {
		return resolvedImage{}, refuseSensitive("write", clean)
	}
	if background == "transparent" && format == "jpeg" {
		return resolvedImage{}, errors.New("transparent background requires png or webp")
	}

	overwrite := false
	info, statErr := r.workspace.Lstat(clean)
	if statErr == nil {
		if info.IsDir() {
			return resolvedImage{}, fmt.Errorf("cannot replace directory: %s", clean)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return resolvedImage{}, fmt.Errorf("refusing to replace symlink: %s", clean)
		}
		overwrite = true
	} else if !errors.Is(statErr, os.ErrNotExist) {
		return resolvedImage{}, statErr
	}

	return resolvedImage{
		prompt:     prompt,
		path:       clean,
		model:      model,
		size:       size,
		quality:    quality,
		format:     format,
		background: background,
		overwrite:  overwrite,
	}, nil
}

func normalizeImageModel(value string) (string, error) {
	id := strings.ToLower(strings.TrimSpace(value))
	if canonical, ok := gptImageModels[id]; ok {
		return canonical, nil
	}
	return "", fmt.Errorf("unsupported image model %q; use gpt-image-2", value)
}

func normalizeImageFormat(value string) (string, error) {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" {
		return "", nil
	}
	if value == "jpg" {
		value = "jpeg"
	}
	switch value {
	case "png", "webp", "jpeg":
		return value, nil
	default:
		return "", fmt.Errorf("output_format must be png, webp, or jpeg")
	}
}

func normalizeImageSize(value string) (string, error) {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" || value == "auto" {
		return "auto", nil
	}
	switch value {
	case "1024x1024", "1536x1024", "1024x1536":
		return value, nil
	}
	width, height, ok := parseImageWH(value)
	if !ok {
		return "", errors.New("size must be auto or WIDTHxHEIGHT")
	}
	if width%16 != 0 || height%16 != 0 {
		return "", errors.New("width and height must be divisible by 16")
	}
	if width < 16 || height < 16 || width > maxImageEdge || height > maxImageEdge {
		return "", fmt.Errorf("size must be between 16x16 and %dx%d", maxImageEdge, maxImageEdge)
	}
	if width*height > maxImagePixels {
		return "", fmt.Errorf("size exceeds %d pixels", maxImagePixels)
	}
	if width*3 < height || height*3 < width {
		return "", errors.New("aspect ratio must be between 1:3 and 3:1")
	}
	return fmt.Sprintf("%dx%d", width, height), nil
}

func parseImageWH(value string) (int, int, bool) {
	left, right, ok := strings.Cut(value, "x")
	if !ok {
		return 0, 0, false
	}
	width, err := strconv.Atoi(left)
	if err != nil || width <= 0 {
		return 0, 0, false
	}
	height, err := strconv.Atoi(right)
	if err != nil || height <= 0 {
		return 0, 0, false
	}
	return width, height, true
}

func normalizeChoice(name, value string, allowed ...string) (string, error) {
	value = strings.ToLower(strings.TrimSpace(value))
	for _, option := range allowed {
		if value == option {
			return option, nil
		}
	}
	return "", fmt.Errorf("%s must be %s", name, strings.Join(allowed, ", "))
}

func resolveImagePath(raw, format string) (string, string, error) {
	value := strings.TrimSpace(strings.ReplaceAll(raw, "\\", "/"))
	if value == "" {
		return "", "", errors.New("path cannot be empty")
	}
	ext := strings.ToLower(path.Ext(value))
	switch ext {
	case "":
		if format == "" {
			format = "png"
		}
		return value + "." + formatExt(format), format, nil
	case ".png", ".webp":
		inferred := strings.TrimPrefix(ext, ".")
		if format == "" {
			format = inferred
		} else if format != inferred {
			return "", "", fmt.Errorf("path extension %s does not match output_format %s", ext, format)
		}
		return value, format, nil
	case ".jpg", ".jpeg":
		if format == "" {
			format = "jpeg"
		} else if format != "jpeg" {
			return "", "", fmt.Errorf("path extension %s does not match output_format %s", ext, format)
		}
		return value, format, nil
	default:
		return "", "", errors.New("path must end in .png, .webp, .jpg, or .jpeg")
	}
}

func formatExt(format string) string {
	if format == "jpeg" {
		return "jpg"
	}
	return format
}

func imagePreview(resolved resolvedImage) string {
	var builder strings.Builder
	if resolved.overwrite {
		fmt.Fprintf(&builder, "Overwrite %s\n", resolved.path)
	} else {
		fmt.Fprintf(&builder, "Write %s\n", resolved.path)
	}
	fmt.Fprintf(&builder, "model: %s\n", resolved.model)
	fmt.Fprintf(&builder, "size: %s\n", resolved.size)
	fmt.Fprintf(&builder, "quality: %s\n", resolved.quality)
	fmt.Fprintf(&builder, "format: %s\n", resolved.format)
	fmt.Fprintf(&builder, "background: %s\n\n", resolved.background)
	builder.WriteString(resolved.prompt)
	return builder.String()
}

func formatImageResult(resolved resolvedImage, result ImageResult) string {
	model := firstNonEmpty(result.Model, resolved.model)
	size := firstNonEmpty(result.Size, resolved.size)
	quality := firstNonEmpty(result.Quality, resolved.quality)
	format := firstNonEmpty(result.Format, resolved.format)
	return fmt.Sprintf("Wrote %s (%s, %s, %s, %s)", resolved.path, model, size, quality, format)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}
