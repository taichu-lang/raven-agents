package underlying

import (
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
)

// ImageSource references one image either as raw bytes or as a URI. Exactly one of Data and URI is set; use the
// constructors to enforce the invariant. A URI may be an http(s) URL or a data URL, depending on what the
// accepting provider supports.
type ImageSource struct {
	MediaType MediaType `json:"media_type"`
	Data      []byte    `json:"data,omitzero"`
	URI       string    `json:"uri,omitzero"`
}

func NewImageSourceBytes(media MediaType, data []byte) *ImageSource {
	return &ImageSource{
		MediaType: media,
		Data:      data,
	}
}

func NewImageSourceURI(media MediaType, uri string) *ImageSource {
	return &ImageSource{
		MediaType: media,
		URI:       uri,
	}
}

// Validate reports whether the source holds exactly one of Data or URI.
func (s *ImageSource) Validate() error {
	switch {
	case len(s.Data) > 0 && s.URI != "":
		return errors.New("image source: data and uri are mutually exclusive")
	case len(s.Data) == 0 && s.URI == "":
		return errors.New("image source: one of data or uri is required")
	}

	return nil
}

// DataURL renders the source as a data URL ("data:<media type>;base64,..."). A URI source is returned verbatim
// when it already is a data URL. Remote http(s) URIs cannot be embedded: download the image first and pass its
// bytes instead.
func (s *ImageSource) DataURL() (string, error) {
	if err := s.Validate(); err != nil {
		return "", err
	}

	if s.URI != "" {
		if strings.HasPrefix(s.URI, "data:") {
			return s.URI, nil
		}

		return "", fmt.Errorf("image source: remote uri %q cannot be embedded; pass image bytes", s.URI)
	}

	encoded := base64.StdEncoding.EncodeToString(s.Data)
	return fmt.Sprintf("data:%s;base64,%s", s.MediaType, encoded), nil
}

// ImageFormat is the file format of generated images. The empty value means "provider default" and is omitted
// from the wire request.
type ImageFormat string

const (
	ImageFormatPNG  ImageFormat = "png"
	ImageFormatJPEG ImageFormat = "jpeg"
	ImageFormatWebP ImageFormat = "webp"
)

// MediaType maps the format to its media type, defaulting to png. It is used to type results decoded from a
// generation response.
func (f ImageFormat) MediaType() MediaType {
	switch f {
	case ImageFormatJPEG:
		return MediaTypeImageJPG
	case ImageFormatWebP:
		return MediaTypeImageWebP
	default:
		return MediaTypeImagePNG
	}
}

// ImageQuality is the rendering effort of image generation. The empty value means "provider default" (ex:
// "auto") and is omitted from the wire request.
type ImageQuality string

const (
	ImageQualityAuto   ImageQuality = "auto"
	ImageQualityLow    ImageQuality = "low"
	ImageQualityMedium ImageQuality = "medium"
	ImageQualityHigh   ImageQuality = "high"
)

// ImageBackground controls the background of generated images. The empty value means "provider default" and is
// omitted from the wire request.
type ImageBackground string

const (
	ImageBackgroundAuto        ImageBackground = "auto"
	ImageBackgroundTransparent ImageBackground = "transparent"
	ImageBackgroundOpaque      ImageBackground = "opaque"
)

// Well-known image sizes. Size stays free-form because providers accept arbitrary dimensions (ex: gpt-image-2
// accepts any WxH) or provider-specific tokens (ex: Seedream "2K"), so an enum cannot cover the space.
const (
	ImageSizeAuto      = "auto"
	ImageSizeSquare    = "1024x1024"
	ImageSizeLandscape = "1536x1024"
	ImageSizePortrait  = "1024x1536"
)

// ImageRequest is the provider-neutral image generation request. An empty Images slice is a text-to-image
// generation; a non-empty slice is an edit of those images, so a single call covers both flows. Fields a
// provider does not support are rejected by its adapter with an error naming the field.
//
// TODO(Leo): for now, ImageRequest is only compatible with OpenAI, add provider-scoped options.
type ImageRequest struct {
	Prompt string

	// Images holds the source images of an edit request; empty means generate.
	Images []*ImageSource

	// Mask marks the area of the source image an edit may change (alpha 0 = editable). Only meaningful together
	// with Images.
	Mask *ImageSource

	// Size is a "WxH" dimension string, "auto", or a provider-specific token (ex: Seedream "2K"). Mutually
	// exclusive with AspectRatio.
	//
	// TODO(Leo): is an enum such as 'xs', 'md' better ?
	Size string

	// AspectRatio is a ratio string such as "1:1" or "3:2" for providers that reason in ratios rather than
	// pixels (ex: Stability, Gemini image).
	AspectRatio string

	// Count is the number of images to generate; 0 lets the provider default (1).
	Count int

	Format     ImageFormat
	Quality    ImageQuality
	Background ImageBackground

	// Moderation is the cross-provider content-policy level (ex: OpenAI "auto"/"low").
	Moderation string

	// InputFidelity controls how closely an edit tracks its source images (ex: OpenAI "low"/"high").
	InputFidelity string

	// PartialImages requests up to this many progressively-refined previews per image on streaming providers
	// (ex: OpenAI partial_images, 0-3). Providers without partial support simply never emit partial chunks.
	PartialImages int
}

// Validate checks the request invariants that hold for every provider; adapters
// apply their own provider-specific rules on top.
func (r *ImageRequest) Validate() error {
	if strings.TrimSpace(r.Prompt) == "" {
		return errors.New("image request: prompt is required")
	}

	if r.Background == ImageBackgroundTransparent &&
		r.Format != "" && r.Format != ImageFormatPNG && r.Format != ImageFormatWebP {
		return fmt.Errorf(
			"image request: transparent background requires png or webp format, got %q",
			r.Format,
		)
	}

	if r.Size != "" && r.AspectRatio != "" {
		return errors.New("image request: size and aspect ratio are mutually exclusive")
	}

	for i, image := range r.Images {
		if image == nil {
			return fmt.Errorf("image request: images[%d]: source is nil", i)
		}

		if err := image.Validate(); err != nil {
			return fmt.Errorf("image request: images[%d]: %w", i, err)
		}
	}

	if r.Mask != nil {
		if len(r.Images) == 0 {
			return errors.New("image request: mask requires at least one source image")
		}

		if err := r.Mask.Validate(); err != nil {
			return fmt.Errorf("image request: mask: %w", err)
		}
	}

	return nil
}

// ImageResult is what a successful generation or edit produced.
type ImageResult struct {
	// Images holds the generated images: Data sources for byte-oriented providers (ex: OpenAI b64_json), URI
	// sources for url-oriented ones. Never nil.
	Images []*ImageSource

	// Usage reports token consumption when the provider surfaces it.
	Usage *UsageContent
}

// Contents renders the result as message content blocks, one data or uri block per image, so it can be appended
// to conversation history and sent back to chat models as vision input. Feed the blocks back inside a user
// message: providers (ex: OpenAI) reject images in assistant messages. Callers add their own context text to
// the wrapping message.
func (r *ImageResult) Contents() MessageContents {
	contents := make(MessageContents, 0, len(r.Images))
	for _, image := range r.Images {
		if image.URI != "" {
			contents = append(contents, NewURIContent(image.MediaType, image.URI))
			continue
		}

		contents = append(contents, NewDataContent(image.MediaType, image.Data))
	}

	return contents
}

// ImageChunk is one item of an image generation stream.
type ImageChunk struct {
	// Index is the 0-based index of the image this chunk belongs to.
	Index int

	// PartialImage is a progressively-refined preview available while the image is still being generated. It is
	// nil on the terminal chunk.
	PartialImage *ImageSource

	// Result carries the final images and usage. It is set only on the terminal chunk of the stream.
	Result *ImageResult
}
