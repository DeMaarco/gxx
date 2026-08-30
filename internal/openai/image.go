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

package openai

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"

	openaisdk "github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
)

const DefaultImageModel = "gpt-image-2"

type ImageRequest struct {
	Prompt     string
	Model      string
	Size       string
	Quality    string
	Format     string
	Background string
	BaseURL    string
}

type ImageResult struct {
	Data    []byte
	Model   string
	Size    string
	Quality string
	Format  string
}

func GenerateImage(ctx context.Context, apiKey string, req ImageRequest) (ImageResult, error) {
	if strings.TrimSpace(apiKey) == "" {
		return ImageResult{}, errors.New("image generation needs an OpenAI platform API key (not ChatGPT login); run /config or export OPENAI_API_KEY")
	}
	prompt := strings.TrimSpace(req.Prompt)
	if prompt == "" {
		return ImageResult{}, errors.New("prompt cannot be empty")
	}
	model := strings.TrimSpace(req.Model)
	if model == "" {
		model = DefaultImageModel
	}

	opts := []option.RequestOption{option.WithAPIKey(apiKey)}
	if baseURL := strings.TrimSpace(req.BaseURL); baseURL != "" {
		opts = append(opts, option.WithBaseURL(strings.TrimRight(baseURL, "/")+"/"))
	}
	client := openaisdk.NewClient(opts...)

	params := openaisdk.ImageGenerateParams{
		Prompt: prompt,
		Model:  openaisdk.ImageModel(model),
		N:      openaisdk.Int(1),
	}
	if size := strings.TrimSpace(req.Size); size != "" {
		params.Size = openaisdk.ImageGenerateParamsSize(size)
	}
	if quality := strings.TrimSpace(req.Quality); quality != "" {
		params.Quality = openaisdk.ImageGenerateParamsQuality(quality)
	}
	if format := strings.TrimSpace(req.Format); format != "" {
		params.OutputFormat = openaisdk.ImageGenerateParamsOutputFormat(format)
	}
	if background := strings.TrimSpace(req.Background); background != "" {
		params.Background = openaisdk.ImageGenerateParamsBackground(background)
	}

	resp, err := client.Images.Generate(ctx, params)
	if err != nil {
		return ImageResult{}, formatResponsesError(err)
	}
	if resp == nil || len(resp.Data) == 0 {
		return ImageResult{}, errors.New("image API returned no image data")
	}
	encoded := strings.TrimSpace(resp.Data[0].B64JSON)
	if encoded == "" {
		if strings.TrimSpace(resp.Data[0].URL) != "" {
			return ImageResult{}, errors.New("image API returned a URL; GPT image models should return b64_json")
		}
		return ImageResult{}, errors.New("image API returned no image data")
	}
	data, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return ImageResult{}, fmt.Errorf("decode generated image: %w", err)
	}
	if len(data) == 0 {
		return ImageResult{}, errors.New("image API returned empty data")
	}

	result := ImageResult{
		Data:    data,
		Model:   model,
		Size:    string(resp.Size),
		Quality: string(resp.Quality),
		Format:  string(resp.OutputFormat),
	}
	if result.Format == "" {
		result.Format = strings.TrimSpace(req.Format)
	}
	if result.Size == "" {
		result.Size = strings.TrimSpace(req.Size)
	}
	if result.Quality == "" {
		result.Quality = strings.TrimSpace(req.Quality)
	}
	return result, nil
}
