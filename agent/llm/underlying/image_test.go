package underlying

import (
	"context"
	"strings"
	"testing"
)

func TestImageSourceValidate(t *testing.T) {
	tests := []struct {
		name    string
		source  *MediaContent
		wantErr string
	}{
		{name: "bytes", source: NewMediaContentFromData(MediaTypeImagePNG, []byte("png"))},
		{name: "uri", source: NewMediaContentFromURI(MediaTypeImageJPG, "https://example.com/a.jpg")},
		{name: "data uri", source: NewMediaContentFromURI(MediaTypeImagePNG, "data:image/png;base64,AAAA")},
		{
			name: "both set",
			source: &MediaContent{
				MediaType: MediaTypeImagePNG,
				Data:      []byte("x"),
				URI:       "data:image/png;base64,AAAA",
			},
			wantErr: "mutually exclusive",
		},
		{
			name:    "neither set",
			source:  &MediaContent{MediaType: MediaTypeImagePNG},
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
	encoded := NewMediaContentFromData(MediaTypeImagePNG, []byte("png-bytes")).ImageURI()
	if !strings.HasPrefix(encoded, "data:image/png;base64,") {
		t.Fatalf("unexpected data url: %s", encoded)
	}

	passthrough := "data:image/jpeg;base64,QUJD"
	dataURL := NewMediaContentFromURI(MediaTypeImageJPG, passthrough).ImageURI()
	if dataURL != passthrough {
		t.Fatalf("data url not passed through: %s", dataURL)
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
			wantErr: "image request: prompt is required",
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
				Images: []*MediaContent{{MediaType: MediaTypeImagePNG}},
			},
			wantErr: "images[0]",
		},
		{
			name: "mask without images",
			req: &ImageRequest{
				Prompt: "a cat",
				Mask:   NewMediaContentFromData(MediaTypeImagePNG, []byte("m")),
			},
			wantErr: "mask requires at least one source image",
		},
		{
			name: "full edit",
			req: &ImageRequest{
				Prompt:        "replace the sky",
				Images:        []*MediaContent{NewMediaContentFromData(MediaTypeImagePNG, []byte("img"))},
				Mask:          NewMediaContentFromData(MediaTypeImagePNG, []byte("mask")),
				Size:          ImageSizeSquare,
				Count:         2,
				Format:        ImageFormatWebP,
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
	result := &ImageResult{Images: []*MediaContent{
		NewMediaContentFromData(MediaTypeImagePNG, []byte("png")),
		NewMediaContentFromURI(MediaTypeImageJPG, "https://example.com/a.jpg"),
	}}

	contents := result.Contents()
	if len(contents) != 2 {
		t.Fatalf("expected 2 contents, got %d", len(contents))
	}

	if data, ok := contents[0].(*MediaContent); !ok || string(data.Data) != "png" {
		t.Fatalf("unexpected data content: %+v", contents[0])
	}

	if uri, ok := contents[1].(*MediaContent); !ok || uri.URI != "https://example.com/a.jpg" {
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
