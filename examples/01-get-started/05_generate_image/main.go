package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/taichu-lang/raven-agents/agent"
	"github.com/taichu-lang/raven-agents/agent/llm"
	"github.com/taichu-lang/raven-agents/agent/llm/openai"
	"github.com/taichu-lang/raven-agents/agent/llm/underlying"
)

func main() {
	agent.UseJsonLog(agent.WithLoggerLevel("debug"))

	ctx := context.Background()
	provider := openai.NewProvider(&underlying.ProviderOptions{
		ApiKey:       os.Getenv("OPENAI_APIKEY"),
		ImageGenURL:  fmt.Sprintf("%s/v1/images/generations", os.Getenv("OPENAI_API")),
		ImageEditURL: fmt.Sprintf("%s/v1/images/edits", os.Getenv("OPENAI_API")),
		Catalog:      &underlying.Catalog{ID: "gpt-image-2.5-flare"},
	})

	dir, err := os.MkdirTemp("", "raven-image-")
	if err != nil {
		slog.Error("failed to create output directory", "err", err)
		return
	}
	defer slog.Info("images are kept in", "dir", dir)

	// Text-to-image, streaming progressive previews while the image renders.
	generated, err := generate(ctx, provider, dir, "generated.png", &underlying.ImageRequest{
		Prompt:        "A red panda sipping tea on a mountain cliff at dawn",
		Size:          underlying.ImageSizeSquare,
		Quality:       underlying.ImageQualityMedium,
		Format:        underlying.ImageFormatPNG,
		PartialImages: 2,
	})
	if err != nil {
		slog.Error("generation failed", "err", err)
		return
	}

	// Edit the generated image: source images travel as bytes in the request.
	edited, err := generate(ctx, provider, dir, "edited.png", &underlying.ImageRequest{
		Prompt:        "Recolor the scene in a warm sunset palette",
		Images:        []*underlying.ImageSource{generated.Images[0]},
		Size:          underlying.ImageSizeLandscape,
		InputFidelity: "high",
	})
	if err != nil {
		slog.Error("editing failed", "err", err)
		return
	}

	// Feed the result back to a chat model as vision input: contents become input_image blocks inside a user
	// message.
	vision := llm.NewRunner(&underlying.ProviderOptions{
		ApiKey:  os.Getenv("OPENAI_APIKEY"),
		TextURL: fmt.Sprintf("%s/v1/responses", os.Getenv("OPENAI_API")),
		Catalog: &underlying.Catalog{ID: "gpt-6-astra"},
	})
	feedback := underlying.NewMessage(underlying.RoleUser, append(
		underlying.MessageContents{underlying.NewTextContent("Describe the edited image in one sentence.")},
		edited.Contents()...,
	))
	for chunk, err := range vision.Run(ctx, []*underlying.Message{feedback}) {
		if err != nil {
			slog.Error("vision feedback failed", "err", err)
			return
		}

		if chunk.Type == underlying.ResponseChunkTypeFinal {
			slog.Info("vision feedback", "text", chunk.Contents.CollectText())
		}
	}
}

// generate consumes the image stream, logs partial previews, saves the final image
// into dir/name and returns the terminal result.
func generate(
	ctx context.Context,
	provider *openai.Provider,
	dir string,
	name string,
	req *underlying.ImageRequest,
) (*underlying.ImageResult, error) {

	result, err := provider.GenerateImage(ctx, req)
	if err != nil {
		return nil, err
	}

	if result == nil {
		slog.Error("stream ended without a result", "request", name)
		return nil, nil
	}

	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, result.Images[0].Data, 0o600); err != nil {
		return nil, err
	}

	if result.Usage != nil {
		slog.Info("image saved", "path", path, "usage", result.Usage.Total)
	}

	return result, nil
}
