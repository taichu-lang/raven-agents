package openai

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"

	"github.com/openai/openai-go/v3"
	"github.com/taichu-lang/raven-agents/agent/llm/underlying"
)

var _ underlying.ImageGenerator = (*Provider)(nil)

// imageBaseRequest carries the parameters shared by the generations and edits endpoints. Optional fields are
// omitted so the api defaults apply.
type imageBaseRequest struct {
	Model         string `json:"model"`
	Prompt        string `json:"prompt"`
	N             int    `json:"n,omitzero"`
	Size          string `json:"size,omitzero"`
	Quality       string `json:"quality,omitzero"`
	Background    string `json:"background,omitzero"`
	OutputFormat  string `json:"output_format,omitzero"`
	Moderation    string `json:"moderation,omitzero"`
	Stream        bool   `json:"stream"`
	PartialImages int    `json:"partial_images,omitzero"`
}

type imageGenerateRequest struct {
	imageBaseRequest
}

// imageEditRequest is the JSON body of the edits endpoint: source images travel as data URLs (the multipart
// alternative is not implemented).
type imageEditRequest struct {
	imageBaseRequest
	Images        []imageEditSource `json:"images"`
	Mask          string            `json:"mask,omitzero"`
	InputFidelity string            `json:"input_fidelity,omitzero"`
}

// imageEditSource is one entry of the edits images array: a data URL nested under the image_url key, the shape
// the edits endpoint expects for JSON bodies.
type imageEditSource struct {
	ImageURL string `json:"image_url"`
}

// GenerateImageStream generates or edits images via the images api. The stream emits partial previews (when
// req.PartialImages > 0) followed by a terminal chunk carrying the final result. The request context is the
// only deadline: the call routinely takes 30-90s before the first response byte.
func (p *Provider) GenerateImageStream(
	ctx context.Context,
	req *underlying.ImageRequest,
) (underlying.ImageStream, error) {
	size, err := resolveImageRequest(req)
	if err != nil {
		return nil, err
	}

	body, edit, err := p.buildImageRequestBody(req, size)
	if err != nil {
		return nil, err
	}

	url := p.options.ImageGenURL
	if edit {
		url = p.options.ImageEditURL
	}

	request, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}

	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Authorization", "Bearer "+p.options.ApiKey)
	request.Header.Set("Accept", "text/event-stream")

	p.logger.Debug(
		"request body of image generation",
		"model",
		p.options.Catalog.ID,
		"body",
		string(body),
	)

	resp, err := p.client.Do(request)
	if err != nil {
		p.logger.Error("failed to send image generation request", "err", err)
		return nil, err
	}

	if resp.StatusCode != http.StatusOK {
		defer resp.Body.Close()
		text, _ := io.ReadAll(resp.Body)
		p.logger.Error(
			"response of image generation request is not ok",
			"status",
			resp.StatusCode,
			"err",
			string(text),
		)
		return nil, errors.New(string(text))
	}

	if edit {
		return p.streamEdits(resp, req.Format), nil
	}

	return p.streamGenerations(resp, req.Format), nil
}

// buildImageRequestBody assembles the wire body and reports whether the edits
// endpoint is required.
func (p *Provider) buildImageRequestBody(
	req *underlying.ImageRequest,
	size string,
) ([]byte, bool, error) {
	base := imageBaseRequest{
		Model:         p.options.Catalog.ID,
		Prompt:        req.Prompt,
		N:             req.Count,
		Size:          size,
		Quality:       string(req.Quality),
		Background:    string(req.Background),
		OutputFormat:  string(req.Format),
		Moderation:    req.Moderation,
		Stream:        true,
		PartialImages: req.PartialImages,
	}

	edit := len(req.Images) > 0
	var body []byte
	var err error
	if edit {
		editReq := imageEditRequest{
			imageBaseRequest: base,
			Images:           make([]imageEditSource, 0, len(req.Images)),
			InputFidelity:    req.InputFidelity,
		}

		for i, src := range req.Images {
			dataURL, err := src.DataURL()
			if err != nil {
				return nil, false, fmt.Errorf("image request: images[%d]: %w", i, err)
			}

			editReq.Images = append(editReq.Images, imageEditSource{ImageURL: dataURL})
		}

		if req.Mask != nil {
			maskURL, err := req.Mask.DataURL()
			if err != nil {
				return nil, false, fmt.Errorf("image request: mask: %w", err)
			}

			editReq.Mask = maskURL
		}

		body, err = json.Marshal(editReq)
	} else {
		body, err = json.Marshal(imageGenerateRequest{imageBaseRequest: base})
	}

	if err != nil {
		return nil, false, err
	}

	return body, edit, nil
}

// streamGenerations converts the generations SSE events into image chunks. The terminal chunk carries the
// aggregated result; usage comes from the last completed event.
func (p *Provider) streamGenerations(
	resp *http.Response,
	fallback underlying.ImageFormat,
) underlying.ImageStream {
	return func(yield func(*underlying.ImageChunk, error) bool) {
		defer resp.Body.Close()

		result := &underlying.ImageResult{Images: make([]*underlying.ImageSource, 0)}
		for frame, err := range sseFrames(resp.Body) {
			if err != nil {
				yield(nil, err)
				return
			}

			var event openai.ImageGenStreamEventUnion
			if err := json.Unmarshal(frame.Data, &event); err != nil {
				yield(nil, fmt.Errorf("decode image generation event: %w", err))
				return
			}

			switch variant := event.AsAny().(type) {
			case openai.ImageGenPartialImageEvent:
				chunk, err := partialImageChunk(variant.B64JSON, variant.PartialImageIndex, string(variant.OutputFormat), fallback)
				if err != nil {
					yield(nil, err)
					return
				}

				if !yield(chunk, nil) {
					return
				}

			case openai.ImageGenCompletedEvent:
				image, err := decodeImage(variant.B64JSON, string(variant.OutputFormat), fallback)
				if err != nil {
					yield(nil, err)
					return
				}

				result.Images = append(result.Images, image)
				result.Usage = &underlying.UsageContent{
					Input:  variant.Usage.InputTokens,
					Output: variant.Usage.OutputTokens,
					Total:  variant.Usage.TotalTokens,
				}
			}
		}

		if len(result.Images) == 0 {
			yield(nil, errors.New("image generation stream completed without any image"))
			return
		}

		yield(&underlying.ImageChunk{Result: result}, nil)
	}
}

// streamEdits mirrors streamGenerations for the edits event union.
func (p *Provider) streamEdits(
	resp *http.Response,
	fallback underlying.ImageFormat,
) underlying.ImageStream {
	return func(yield func(*underlying.ImageChunk, error) bool) {
		defer resp.Body.Close()

		result := &underlying.ImageResult{Images: make([]*underlying.ImageSource, 0)}
		for frame, err := range sseFrames(resp.Body) {
			if err != nil {
				yield(nil, err)
				return
			}

			var event openai.ImageEditStreamEventUnion
			if err := json.Unmarshal(frame.Data, &event); err != nil {
				yield(nil, fmt.Errorf("decode image edit event: %w", err))
				return
			}

			switch variant := event.AsAny().(type) {
			case openai.ImageEditPartialImageEvent:
				chunk, err := partialImageChunk(variant.B64JSON, variant.PartialImageIndex, string(variant.OutputFormat), fallback)
				if err != nil {
					yield(nil, err)
					return
				}

				if !yield(chunk, nil) {
					return
				}

			case openai.ImageEditCompletedEvent:
				image, err := decodeImage(variant.B64JSON, string(variant.OutputFormat), fallback)
				if err != nil {
					yield(nil, err)
					return
				}

				result.Images = append(result.Images, image)
				result.Usage = &underlying.UsageContent{
					Input:  variant.Usage.InputTokens,
					Output: variant.Usage.OutputTokens,
					Total:  variant.Usage.TotalTokens,
				}
			}
		}

		if len(result.Images) == 0 {
			yield(nil, errors.New("image edit stream completed without any image"))
			return
		}

		yield(&underlying.ImageChunk{Result: result}, nil)
	}
}

func partialImageChunk(
	b64 string,
	index int64,
	format string,
	fallback underlying.ImageFormat,
) (*underlying.ImageChunk, error) {
	image, err := decodeImage(b64, format, fallback)
	if err != nil {
		return nil, err
	}

	return &underlying.ImageChunk{Index: int(index), PartialImage: image}, nil
}

func decodeImage(b64 string, format string, fallback underlying.ImageFormat) (*underlying.ImageSource, error) {
	data, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		return nil, fmt.Errorf("decode generated image: %w", err)
	}

	if format == "" {
		format = string(fallback)
	}

	return &underlying.ImageSource{MediaType: mediaTypeFromFormat(format), Data: data}, nil
}

func mediaTypeFromFormat(format string) underlying.MediaType {
	switch format {
	case string(underlying.ImageFormatJPEG):
		return underlying.MediaTypeImageJPG
	case string(underlying.ImageFormatWebP):
		return underlying.MediaTypeImageWebP
	default:
		return underlying.MediaTypeImagePNG
	}
}

var imageSizePattern = regexp.MustCompile(`^\d+x\d+$`)

// resolveImageRequest applies the OpenAI-specific rules on top of the generic invariants and returns the size
// to send. Aspect ratios are mapped onto the standard sizes because the images api reasons in pixels.
func resolveImageRequest(req *underlying.ImageRequest) (string, error) {
	if err := req.Validate(); err != nil {
		return "", err
	}

	if req.Count < 0 || req.Count > 10 {
		return "", fmt.Errorf("image request: count must be between 0 and 10, got %d", req.Count)
	}

	size := req.Size
	switch {
	case size == "" && req.AspectRatio == "":
	case size != "":
		if size != underlying.ImageSizeAuto && !imageSizePattern.MatchString(size) {
			return "", fmt.Errorf("image request: size must be \"auto\" or \"WxH\", got %q", size)
		}
	default:
		switch req.AspectRatio {
		case "1:1":
			size = underlying.ImageSizeSquare
		case "3:2":
			size = underlying.ImageSizeLandscape
		case "2:3":
			size = underlying.ImageSizePortrait
		default:
			return "", fmt.Errorf(
				"image request: unsupported aspect ratio %q; set size explicitly",
				req.AspectRatio,
			)
		}
	}

	if len(req.Images) > 16 {
		return "", fmt.Errorf("image request: at most 16 source images, got %d", len(req.Images))
	}

	if req.Mask != nil && req.Mask.MediaType != underlying.MediaTypeImagePNG {
		return "", fmt.Errorf("image request: mask must be a png image, got %q", req.Mask.MediaType)
	}

	return size, nil
}
