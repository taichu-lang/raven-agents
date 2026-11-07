package underlying

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestImageSourceValidate(t *testing.T) {
	tests := []struct {
		name    string
		source  *ImageSource
		wantErr string
	}{
		{name: "bytes", source: NewImageSourceBytes(MediaTypeImagePNG, []byte("png"))},
		{name: "uri", source: NewImageSourceURI(MediaTypeImageJPG, "https://example.com/a.jpg")},
		{name: "data uri", source: NewImageSourceURI(MediaTypeImagePNG, "data:image/png;base64,AAAA")},
		{
			name: "both set",
			source: &ImageSource{
				MediaType: MediaTypeImagePNG,
				Data:      []byte("x"),
				URI:       "data:image/png;base64,AAAA",
			},
			wantErr: "mutually exclusive",
		},
		{
			name:    "neither set",
			source:  &ImageSource{MediaType: MediaTypeImagePNG},
			wantErr: "one of data or uri",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.source.Validate()
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}

			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("expected error containing %q, got %v", tt.wantErr, err)
			}
		})
	}
}

func TestImageSourceDataURL(t *testing.T) {
	encoded, err := NewImageSourceBytes(MediaTypeImagePNG, []byte("png-bytes")).DataURL()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(encoded, "data:image/png;base64,") {
		t.Fatalf("unexpected data url: %s", encoded)
	}

	passthrough := "data:image/jpeg;base64,QUJD"
	dataURL, err := NewImageSourceURI(MediaTypeImageJPG, passthrough).DataURL()
	if err != nil {
		t.Fatal(err)
	}
	if dataURL != passthrough {
		t.Fatalf("data url not passed through: %s", dataURL)
	}

	_, err = NewImageSourceURI(MediaTypeImageJPG, "https://example.com/a.jpg").DataURL()
	if err == nil || !strings.Contains(err.Error(), "remote uri") {
		t.Fatalf("expected remote uri error, got %v", err)
	}
}

func TestImageRequestValidate(t *testing.T) {

	tests := []struct {
		name    string
		req     *ImageRequest
		wantErr string
	}{
		{name: "minimal generate", req: &ImageRequest{Prompt: "a cat"}},
		{
			name:    "empty instructions",
			req:     &ImageRequest{},
			wantErr: "instructions is required",
		},
		{
			name: "transparent with jpeg",
			req: &ImageRequest{
				Prompt:     "a cat",
				Background: ImageBackgroundTransparent,
				Format:     ImageFormatJPEG,
			},
			wantErr: "transparent background requires png or webp",
		},
		{
			name: "transparent with png",
			req: &ImageRequest{
				Prompt:     "a cat",
				Background: ImageBackgroundTransparent,
				Format:     ImageFormatPNG,
			},
		},
		{
			name:    "size and aspect ratio",
			req:     &ImageRequest{Prompt: "a cat", Size: ImageSizeSquare, AspectRatio: "1:1"},
			wantErr: "mutually exclusive",
		},
		{
			name: "invalid source image",
			req: &ImageRequest{
				Prompt: "a cat",
				Images: []*ImageSource{{MediaType: MediaTypeImagePNG}},
			},
			wantErr: "images[0]",
		},
		{
			name: "mask without images",
			req: &ImageRequest{
				Prompt: "a cat",
				Mask:   NewImageSourceBytes(MediaTypeImagePNG, []byte("m")),
			},
			wantErr: "mask requires at least one source image",
		},
		{
			name: "full edit",
			req: &ImageRequest{
				Prompt:        "replace the sky",
				Images:        []*ImageSource{NewImageSourceBytes(MediaTypeImagePNG, []byte("img"))},
				Mask:          NewImageSourceBytes(MediaTypeImagePNG, []byte("mask")),
				Size:          ImageSizeSquare,
				Count:         2,
				Format:        ImageFormatWebP,
				InputFidelity: "high",
				PartialImages: 2,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.req.Validate()
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}

			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("expected error containing %q, got %v", tt.wantErr, err)
			}
		})
	}
}

func TestImageResultContents(t *testing.T) {
	result := &ImageResult{Images: []*ImageSource{
		NewImageSourceBytes(MediaTypeImagePNG, []byte("png")),
		NewImageSourceURI(MediaTypeImageJPG, "https://example.com/a.jpg"),
	}}

	contents := result.Contents()
	if len(contents) != 2 {
		t.Fatalf("expected 2 contents, got %d", len(contents))
	}

	if data, ok := contents[0].(*DataContent); !ok || string(data.Data) != "png" {
		t.Fatalf("unexpected data content: %+v", contents[0])
	}

	if uri, ok := contents[1].(*URIContent); !ok || uri.URI != "https://example.com/a.jpg" {
		t.Fatalf("unexpected uri content: %+v", contents[1])
	}

	empty := (&ImageResult{}).Contents()
	if empty == nil || len(empty) != 0 {
		t.Fatalf("expected non-nil empty contents, got %#v", empty)
	}
}

type stubImageGenerator struct {
	stream ImageStream
}

func (s *stubImageGenerator) GenerateImageStream(
	_ context.Context,
	_ *ImageRequest,
) (ImageStream, error) {
	return s.stream, nil
}

func TestGenerateImageCollectsTerminalChunk(t *testing.T) {
	result := &ImageResult{Images: []*ImageSource{NewImageSourceBytes(MediaTypeImagePNG, []byte("png"))}}
	generator := &stubImageGenerator{stream: func(yield func(*ImageChunk, error) bool) {
		if !yield(&ImageChunk{Index: 0, PartialImage: result.Images[0]}, nil) {
			return
		}
		yield(&ImageChunk{Result: result}, nil)
	}}

	collected, err := GenerateImage(context.Background(), generator, &ImageRequest{Prompt: "a cat"})
	if err != nil {
		t.Fatal(err)
	}
	if len(collected.Images) != 1 {
		t.Fatalf("expected 1 image, got %+v", collected)
	}

	broken := &stubImageGenerator{stream: func(yield func(*ImageChunk, error) bool) {
		yield(nil, errors.New("boom"))
	}}
	if _, err := GenerateImage(context.Background(), broken, &ImageRequest{Prompt: "a cat"}); err == nil {
		t.Fatal("expected the stream error to surface")
	}

	empty := &stubImageGenerator{stream: func(yield func(*ImageChunk, error) bool) {
		yield(&ImageChunk{}, nil)
	}}
	if _, err := GenerateImage(context.Background(), empty, &ImageRequest{Prompt: "a cat"}); err == nil {
		t.Fatal("expected a missing terminal chunk error")
	}
}
