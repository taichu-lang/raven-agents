package openai

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/taichu-lang/raven-agents/agent/llm/underlying"
)

func newImageTestProvider(endpoint string) *Provider {
	return &Provider{
		options: &underlying.ProviderOptions{
			ImageGenURL: endpoint,
			Catalog:     &underlying.Catalog{ID: "gpt-image-2"},
		},
		logger: slog.Default().With("llm", "openai"),
	}
}

func TestResolveImageRequest(t *testing.T) {
	tests := []struct {
		name    string
		req     *underlying.ImageRequest
		want    string
		wantErr string
	}{
		{name: "bare generate", req: &underlying.ImageRequest{Prompt: "a cat"}, want: ""},
		{
			name: "size auto",
			req:  &underlying.ImageRequest{Prompt: "a cat", Size: underlying.ImageSizeAuto},
			want: underlying.ImageSizeAuto,
		},
		{
			name: "free form size",
			req:  &underlying.ImageRequest{Prompt: "a cat", Size: "2048x2048"},
			want: "2048x2048",
		},
		{
			name:    "invalid size",
			req:     &underlying.ImageRequest{Prompt: "a cat", Size: "huge"},
			wantErr: "must be \"auto\" or \"WxH\"",
		},
		{
			name: "aspect ratio maps to square",
			req:  &underlying.ImageRequest{Prompt: "a cat", AspectRatio: "1:1"},
			want: underlying.ImageSizeSquare,
		},
		{
			name: "aspect ratio maps to landscape",
			req:  &underlying.ImageRequest{Prompt: "a cat", AspectRatio: "3:2"},
			want: underlying.ImageSizeLandscape,
		},
		{
			name:    "unsupported aspect ratio",
			req:     &underlying.ImageRequest{Prompt: "a cat", AspectRatio: "16:9"},
			wantErr: "unsupported aspect ratio",
		},
		{
			name:    "count too large",
			req:     &underlying.ImageRequest{Prompt: "a cat", Count: 11},
			wantErr: "between 0 and 10",
		},
		{
			name: "too many source images",
			req: func() *underlying.ImageRequest {
				images := make([]*underlying.ImageSource, 17)
				for i := range images {
					images[i] = underlying.NewImageSourceBytes(underlying.MediaTypeImagePNG, []byte("img"))
				}
				return &underlying.ImageRequest{Prompt: "edit", Images: images}
			}(),
			wantErr: "at most 16 source images",
		},
		{
			name: "mask must be png",
			req: &underlying.ImageRequest{
				Prompt: "edit",
				Images: []*underlying.ImageSource{
					underlying.NewImageSourceBytes(underlying.MediaTypeImageJPG, []byte("img")),
				},
				Mask: underlying.NewImageSourceBytes(underlying.MediaTypeImageJPG, []byte("mask")),
			},
			wantErr: "mask must be a png image",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			size, err := resolveImageRequest(tt.req)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("expected error containing %q, got %v", tt.wantErr, err)
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if size != tt.want {
				t.Fatalf("expected size %q, got %q", tt.want, size)
			}
		})
	}
}

func TestBuildImageEditBody(t *testing.T) {
	p := newImageTestProvider("https://api.openai.com/v1/responses")

	body, edit, err := p.buildImageRequestBody(&underlying.ImageRequest{
		Prompt: "replace the sky",
		Images: []*underlying.ImageSource{
			underlying.NewImageSourceBytes(underlying.MediaTypeImagePNG, []byte("img")),
		},
		Mask:          underlying.NewImageSourceBytes(underlying.MediaTypeImagePNG, []byte("mask")),
		Size:          underlying.ImageSizeSquare,
		InputFidelity: "high",
	}, underlying.ImageSizeSquare, true)
	if err != nil {
		t.Fatal(err)
	}

	if !edit {
		t.Fatal("source images must target the edits endpoint")
	}

	decoded := map[string]any{}
	if err := json.Unmarshal(body, &decoded); err != nil {
		t.Fatal(err)
	}

	images, ok := decoded["images"].([]any)
	if !ok || len(images) != 1 {
		t.Fatalf("expected one source image, got %v", decoded["images"])
	}
	first, ok := images[0].(map[string]any)
	if !ok {
		t.Fatalf("expected an image_url object, got %v", images[0])
	}
	if url, ok := first["image_url"].(string); !ok || !strings.HasPrefix(url, "data:image/png;base64,") {
		t.Fatalf("unexpected source image: %v", first)
	}

	assertBodyValue(
		t,
		decoded,
		"mask",
		"data:image/png;base64,"+base64.StdEncoding.EncodeToString([]byte("mask")),
	)
	assertBodyValue(t, decoded, "input_fidelity", "high")
	assertBodyValue(t, decoded, "stream", true)
}

func assertBodyValue(t *testing.T, body map[string]any, key string, want any) {
	t.Helper()
	if got, ok := body[key]; !ok || got != want {
		t.Fatalf("expected body[%q] = %v, got %v (body: %v)", key, want, got, body)
	}
}

func sseResponse(frames ...string) *http.Response {
	return &http.Response{
		Body: io.NopCloser(strings.NewReader(strings.Join(frames, "\n\n") + "\n\n")),
	}
}

func collectChunks(t *testing.T, stream underlying.ImageStream) []*underlying.ImageChunk {
	t.Helper()
	chunks := make([]*underlying.ImageChunk, 0)
	for chunk, err := range stream {
		if err != nil {
			t.Fatal(err)
		}
		chunks = append(chunks, chunk)
	}

	return chunks
}

func TestGenerationsStreamEvents(t *testing.T) {
	p := newImageTestProvider("https://api.openai.com/v1/responses")
	resp := sseResponse(
		`data: {"type":"image_generation.partial_image","b64_json":"cGFydGlhbA==","partial_image_index":0,"output_format":"png"}`,
		`data: {"type":"image_generation.completed","b64_json":"ZmluYWw=","output_format":"png","usage":{"input_tokens":10,"output_tokens":20,"total_tokens":30}}`,
	)

	chunks := collectChunks(t, p.streamGenerations(resp, underlying.ImageFormatPNG))
	if len(chunks) != 2 {
		t.Fatalf("expected 2 chunks, got %d", len(chunks))
	}

	partial := chunks[0]
	if partial.Index != 0 || partial.PartialImage == nil || string(partial.PartialImage.Data) != "partial" {
		t.Fatalf("unexpected partial chunk: %+v", partial)
	}
	if partial.PartialImage.MediaType != underlying.MediaTypeImagePNG {
		t.Fatalf("unexpected partial media type: %q", partial.PartialImage.MediaType)
	}

	terminal := chunks[1]
	if terminal.Result == nil || len(terminal.Result.Images) != 1 ||
		string(terminal.Result.Images[0].Data) != "final" {
		t.Fatalf("unexpected terminal chunk: %+v", terminal)
	}
	usage := terminal.Result.Usage
	if usage == nil || usage.Input != 10 || usage.Output != 20 || usage.Total != 30 {
		t.Fatalf("unexpected usage: %+v", usage)
	}
}

func TestEditsStreamEvents(t *testing.T) {
	p := newImageTestProvider("https://api.openai.com/v1/responses")
	resp := sseResponse(
		`data: {"type":"image_edit.partial_image","b64_json":"cGFydGlhbA==","partial_image_index":1,"output_format":"webp"}`,
		`data: {"type":"image_edit.completed","b64_json":"ZmluYWw=","output_format":"webp","usage":{"input_tokens":5,"output_tokens":7,"total_tokens":12}}`,
	)

	chunks := collectChunks(t, p.streamEdits(resp, underlying.ImageFormatWebP))
	if len(chunks) != 2 {
		t.Fatalf("expected 2 chunks, got %d", len(chunks))
	}

	if chunks[0].Index != 1 || chunks[0].PartialImage.MediaType != underlying.MediaTypeImageWebP {
		t.Fatalf("unexpected partial chunk: %+v", chunks[0])
	}

	usage := chunks[1].Result.Usage
	if usage.Total != 12 {
		t.Fatalf("unexpected usage: %+v", usage)
	}
}

func TestStreamWithoutImagesErrors(t *testing.T) {
	p := newImageTestProvider("https://api.openai.com/v1/responses")
	resp := sseResponse(
		`data: {"type":"image_generation.partial_image","b64_json":"cGFydGlhbA==","partial_image_index":0,"output_format":"png"}`,
	)

	var streamErr error
	for _, err := range p.streamGenerations(resp, underlying.ImageFormatPNG) {
		streamErr = err
	}
	if streamErr == nil || !strings.Contains(streamErr.Error(), "without any image") {
		t.Fatalf("expected a missing image error, got %v", streamErr)
	}
}

func TestURIContentFeedsVisionInput(t *testing.T) {
	items := inputItemsFromMessage(&underlying.Message{
		Role: underlying.RoleUser,
		Contents: underlying.MessageContents{
			underlying.NewTextContent("what is in this image?"),
			underlying.NewURIContent(underlying.MediaTypeImageJPG, "https://example.com/photo.jpg"),
		},
	})
	if len(items) != 1 {
		t.Fatalf("expected one input message, got %d", len(items))
	}

	content := items[0].InputMessage.Content
	if len(content) != 2 {
		t.Fatalf("expected 2 input contents, got %d", len(content))
	}

	if content[1].Type != InputTypeImage || content[1].InputImage.ImageURL != "https://example.com/photo.jpg" {
		t.Fatalf("the uri image did not reach input_image: %+v", content[1])
	}
}

func TestGenerateImageBlockingAgainstServer(t *testing.T) {
	var requestBody map[string]any

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/images/generations" {
			w.WriteHeader(http.StatusNotFound)
			return
		}

		if err := json.NewDecoder(r.Body).Decode(&requestBody); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(
			w,
			`{"created":1,"output_format":"jpeg","data":[{"b64_json":"ZmluYWw="}],`+
				`"usage":{"input_tokens":1,"output_tokens":2,"total_tokens":3}}`,
		)
	}))
	defer server.Close()

	p := &Provider{
		client: server.Client(),
		options: &underlying.ProviderOptions{
			ImageGenURL: server.URL + "/v1/images/generations",
			Catalog:     &underlying.Catalog{ID: "gpt-image-2"},
		},
		logger: slog.Default().With("llm", "openai"),
	}

	result, err := p.GenImage(
		context.Background(),
		&underlying.ImageRequest{Prompt: "a cat", PartialImages: 2},
	)
	if err != nil {
		t.Fatal(err)
	}

	if requestBody["stream"] != false {
		t.Fatalf("blocking request must send stream=false, got %v", requestBody["stream"])
	}

	if _, ok := requestBody["partial_images"]; ok {
		t.Fatal("partial_images must be omitted for blocking requests")
	}

	if len(result.Images) != 1 || string(result.Images[0].Data) != "final" ||
		result.Images[0].MediaType != underlying.MediaTypeImageJPG {
		t.Fatalf("unexpected result: %+v", result)
	}

	if result.Usage == nil || result.Usage.Total != 3 {
		t.Fatalf("unexpected usage: %+v", result.Usage)
	}
}
