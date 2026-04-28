package imagepipeline

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/jpeg"
	"image/png"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/setting/system_setting"

	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"

	"golang.org/x/image/webp"
)

type ImagePipelineOptions struct {
	Component     string `json:"component,omitempty"`
	Input         string `json:"input,omitempty"`
	Output        string `json:"output,omitempty"`
	Storage       string `json:"storage,omitempty"`
	MimeType      string `json:"mime_type,omitempty"`
	Quality       int    `json:"quality,omitempty"`
	StripAlpha    bool   `json:"strip_alpha,omitempty"`
	KeepSize      bool   `json:"keep_size,omitempty"`
	MaxDownloadMB int    `json:"max_download_mb,omitempty"`
	DryRun        bool   `json:"-"`
}

type ImageTransformResult struct {
	Value        string `json:"value"`
	MimeType     string `json:"mime_type"`
	InputFormat  string `json:"input_format"`
	OutputFormat string `json:"output_format"`
	OriginalSize int64  `json:"original_size"`
	StoredSize   int64  `json:"stored_size"`
}

type loadedImageData struct {
	RawBytes    []byte
	MimeType    string
	Size        int64
	InputFormat string
}

func normalizeImagePipelineOptions(options ImagePipelineOptions) ImagePipelineOptions {
	options.Component = strings.TrimSpace(options.Component)
	if options.Component == "" {
		options.Component = "image_pipeline"
	}
	options.Input = strings.ToLower(strings.TrimSpace(options.Input))
	if options.Input == "" {
		options.Input = "auto"
	}
	options.Output = strings.ToLower(strings.TrimSpace(options.Output))
	if options.Output == "" {
		options.Output = "base64"
	}
	options.Storage = strings.ToLower(strings.TrimSpace(options.Storage))
	if options.Storage == "" {
		options.Storage = "none"
	}
	options.MimeType = strings.ToLower(strings.TrimSpace(options.MimeType))
	if options.MimeType == "" {
		options.MimeType = "keep"
	}
	if options.Quality <= 0 {
		options.Quality = 92
	}
	return options
}

func detectImageInputFormat(raw string) string {
	trimmed := strings.TrimSpace(raw)
	switch {
	case strings.HasPrefix(trimmed, "http://"), strings.HasPrefix(trimmed, "https://"):
		return "url"
	case strings.HasPrefix(trimmed, "data:"):
		return "data_url"
	default:
		return "base64"
	}
}

func maxDownloadBytes(maxDownloadMB int) int64 {
	limitMB := constant.MaxFileDownloadMB
	if maxDownloadMB > 0 {
		limitMB = maxDownloadMB
	}
	return int64(limitMB) * 1024 * 1024
}

func downloadImageURL(ctx context.Context, rawURL string, maxDownloadMB int) (*loadedImageData, error) {
	fetchSetting := system_setting.GetFetchSetting()
	if err := common.ValidateURLWithFetchSetting(
		rawURL,
		fetchSetting.EnableSSRFProtection,
		fetchSetting.AllowPrivateIp,
		fetchSetting.DomainFilterMode,
		fetchSetting.IpFilterMode,
		fetchSetting.DomainList,
		fetchSetting.IpList,
		fetchSetting.AllowedPorts,
		fetchSetting.ApplyIPFilterForDomain,
	); err != nil {
		return nil, fmt.Errorf("request reject: %v", err)
	}

	timeoutSeconds := common.RelayTimeout
	if timeoutSeconds <= 0 {
		timeoutSeconds = 120
	}

	client := &http.Client{
		Timeout: time.Duration(timeoutSeconds) * time.Second,
		Transport: &http.Transport{
			MaxIdleConns:        common.RelayMaxIdleConns,
			MaxIdleConnsPerHost: common.RelayMaxIdleConnsPerHost,
			ForceAttemptHTTP2:   true,
			Proxy:               http.ProxyFromEnvironment,
			TLSClientConfig:     common.InsecureTLSConfig,
		},
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			redirectURL := req.URL.String()
			if err := common.ValidateURLWithFetchSetting(
				redirectURL,
				fetchSetting.EnableSSRFProtection,
				fetchSetting.AllowPrivateIp,
				fetchSetting.DomainFilterMode,
				fetchSetting.IpFilterMode,
				fetchSetting.DomainList,
				fetchSetting.IpList,
				fetchSetting.AllowedPorts,
				fetchSetting.ApplyIPFilterForDomain,
			); err != nil {
				return fmt.Errorf("redirect blocked: %v", err)
			}
			if len(via) >= 10 {
				return fmt.Errorf("stopped after 10 redirects")
			}
			return nil
		},
	}
	if !common.TLSInsecureSkipVerify {
		client.Transport.(*http.Transport).TLSClientConfig = nil
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("failed to download file, status code: %d", resp.StatusCode)
	}

	maxBytes := maxDownloadBytes(maxDownloadMB)
	fileBytes, err := io.ReadAll(io.LimitReader(resp.Body, maxBytes+1))
	if err != nil {
		return nil, fmt.Errorf("failed to read file content: %w", err)
	}
	if int64(len(fileBytes)) > maxBytes {
		return nil, fmt.Errorf("file size exceeds maximum allowed size: %dMB", maxBytes/1024/1024)
	}

	mimeType := detectImageMimeType(resp.Header.Get("Content-Type"), rawURL, fileBytes)
	return &loadedImageData{
		RawBytes:    fileBytes,
		MimeType:    mimeType,
		Size:        int64(len(fileBytes)),
		InputFormat: "url",
	}, nil
}

func decodeInlineImage(raw string, inputFormat string) (*loadedImageData, error) {
	trimmed := strings.TrimSpace(raw)
	mimeType := ""
	base64Payload := trimmed

	if inputFormat == "data_url" || strings.HasPrefix(trimmed, "data:") {
		commaIndex := strings.Index(trimmed, ",")
		if commaIndex < 0 {
			return nil, fmt.Errorf("invalid data url")
		}
		header := trimmed[:commaIndex]
		base64Payload = trimmed[commaIndex+1:]
		if strings.HasPrefix(header, "data:") {
			meta := strings.TrimPrefix(header, "data:")
			if semiIndex := strings.Index(meta, ";"); semiIndex >= 0 {
				mimeType = strings.TrimSpace(meta[:semiIndex])
			} else {
				mimeType = strings.TrimSpace(meta)
			}
		}
	}

	fileBytes, err := decodeBase64Payload(base64Payload)
	if err != nil {
		return nil, err
	}
	if mimeType == "" {
		mimeType = detectImageMimeType("", "", fileBytes)
	}

	return &loadedImageData{
		RawBytes:    fileBytes,
		MimeType:    mimeType,
		Size:        int64(len(fileBytes)),
		InputFormat: inputFormat,
	}, nil
}

func loadImageInput(ctx context.Context, raw string, inputFormat string, maxDownloadMB int) (*loadedImageData, error) {
	switch inputFormat {
	case "url":
		return downloadImageURL(ctx, raw, maxDownloadMB)
	case "base64", "data_url":
		loaded, err := decodeInlineImage(raw, inputFormat)
		if err != nil {
			return nil, err
		}
		if maxDownloadMB > 0 && loaded.Size > int64(maxDownloadMB)*1024*1024 {
			return nil, fmt.Errorf("image exceeds max_download_mb: %d", maxDownloadMB)
		}
		return loaded, nil
	default:
		return nil, fmt.Errorf("unsupported input format: %s", inputFormat)
	}
}

func decodeBase64Payload(base64Data string) ([]byte, error) {
	trimmed := strings.TrimSpace(base64Data)
	if trimmed == "" {
		return nil, fmt.Errorf("image input is empty")
	}
	if decoded, err := base64.StdEncoding.DecodeString(trimmed); err == nil {
		return decoded, nil
	}
	if decoded, err := base64.RawStdEncoding.DecodeString(trimmed); err == nil {
		return decoded, nil
	}
	if decoded, err := base64.URLEncoding.DecodeString(trimmed); err == nil {
		return decoded, nil
	}
	return base64.RawURLEncoding.DecodeString(trimmed)
}

func cleanMimeType(raw string) string {
	mimeType := strings.ToLower(strings.TrimSpace(raw))
	if mimeType == "" {
		return ""
	}
	if index := strings.Index(mimeType, ";"); index >= 0 {
		mimeType = strings.TrimSpace(mimeType[:index])
	}
	return mimeType
}

func detectImageMimeType(headerValue string, rawURL string, fileBytes []byte) string {
	mimeType := cleanMimeType(headerValue)
	if mimeType != "" && mimeType != "application/octet-stream" {
		return mimeType
	}

	if guessed := guessMimeTypeFromURL(rawURL); guessed != "application/octet-stream" {
		return guessed
	}

	if len(fileBytes) > 0 {
		sniffed := cleanMimeType(http.DetectContentType(fileBytes))
		if sniffed != "" && sniffed != "application/octet-stream" {
			return sniffed
		}

		if _, format, err := decodeImageConfig(fileBytes); err == nil && format != "" {
			switch strings.ToLower(format) {
			case "jpg":
				return "image/jpeg"
			default:
				return "image/" + strings.ToLower(format)
			}
		}
	}

	return "application/octet-stream"
}

func guessMimeTypeFromURL(rawURL string) string {
	if strings.TrimSpace(rawURL) == "" {
		return "application/octet-stream"
	}
	parsedURL, err := url.Parse(rawURL)
	if err != nil {
		return "application/octet-stream"
	}
	pathValue := parsedURL.Path
	if pathValue == "" {
		pathValue = rawURL
	}

	lastSlash := strings.LastIndex(pathValue, "/")
	lastDot := strings.LastIndex(pathValue, ".")
	if lastDot < 0 || lastDot <= lastSlash {
		return "application/octet-stream"
	}
	return mimeTypeByExtension(pathValue[lastDot+1:])
}

func mimeTypeByExtension(ext string) string {
	switch strings.ToLower(strings.TrimSpace(ext)) {
	case "jpg", "jpeg", "jfif":
		return "image/jpeg"
	case "png":
		return "image/png"
	case "gif":
		return "image/gif"
	case "webp":
		return "image/webp"
	case "bmp":
		return "image/bmp"
	case "svg":
		return "image/svg+xml"
	case "heic":
		return "image/heic"
	case "heif":
		return "image/heif"
	default:
		return "application/octet-stream"
	}
}

func decodeImageConfig(data []byte) (image.Config, string, error) {
	reader := bytes.NewReader(data)
	config, format, err := image.DecodeConfig(reader)
	if err == nil {
		return config, format, nil
	}

	reader = bytes.NewReader(data)
	config, err = webp.DecodeConfig(reader)
	if err == nil {
		return config, "webp", nil
	}
	return image.Config{}, "", fmt.Errorf("failed to decode image config: unsupported format")
}

func decodeImage(data []byte) (image.Image, error) {
	reader := bytes.NewReader(data)
	decoded, _, err := image.Decode(reader)
	if err == nil {
		return decoded, nil
	}

	reader = bytes.NewReader(data)
	return webp.Decode(reader)
}

func encodeImage(img image.Image, mimeType string, quality int, stripAlpha bool) ([]byte, error) {
	working := img
	if mimeType == "image/jpeg" && stripAlpha {
		background := image.NewRGBA(img.Bounds())
		draw.Draw(background, background.Bounds(), &image.Uniform{C: color.White}, image.Point{}, draw.Src)
		draw.Draw(background, background.Bounds(), img, img.Bounds().Min, draw.Over)
		working = background
	}

	var buffer bytes.Buffer
	switch mimeType {
	case "image/jpeg":
		if err := jpeg.Encode(&buffer, working, &jpeg.Options{Quality: quality}); err != nil {
			return nil, err
		}
	case "image/png":
		if err := png.Encode(&buffer, working); err != nil {
			return nil, err
		}
	default:
		return nil, fmt.Errorf("unsupported mime_type: %s", mimeType)
	}
	return buffer.Bytes(), nil
}

func normalizeImageBytes(rawBytes []byte, currentMimeType string, options ImagePipelineOptions) ([]byte, string, error) {
	targetMimeType := options.MimeType
	if targetMimeType == "" || targetMimeType == "keep" {
		targetMimeType = cleanMimeType(currentMimeType)
	}
	if targetMimeType == "" {
		targetMimeType = "image/png"
	}

	currentMimeType = cleanMimeType(currentMimeType)
	needsReencode := targetMimeType != currentMimeType
	if targetMimeType == "image/jpeg" && options.Quality > 0 {
		needsReencode = true
	}
	if targetMimeType == "image/jpeg" && options.StripAlpha {
		needsReencode = true
	}
	if !needsReencode {
		return rawBytes, targetMimeType, nil
	}

	img, err := decodeImage(rawBytes)
	if err != nil {
		return nil, "", err
	}
	normalized, err := encodeImage(img, targetMimeType, options.Quality, options.StripAlpha)
	if err != nil {
		return nil, "", err
	}
	return normalized, targetMimeType, nil
}

func TransformImageValue(ctx context.Context, raw string, options ImagePipelineOptions) (*ImageTransformResult, error) {
	options = normalizeImagePipelineOptions(options)
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return nil, fmt.Errorf("image input is empty")
	}

	inputFormat := options.Input
	if inputFormat == "auto" {
		inputFormat = detectImageInputFormat(trimmed)
	}

	loaded, err := loadImageInput(ctx, trimmed, inputFormat, options.MaxDownloadMB)
	if err != nil {
		return nil, err
	}
	if !strings.HasPrefix(cleanMimeType(loaded.MimeType), "image/") {
		return nil, fmt.Errorf("only image content is supported")
	}

	finalBytes, finalMimeType, err := normalizeImageBytes(loaded.RawBytes, loaded.MimeType, options)
	if err != nil {
		return nil, err
	}
	finalBase64 := base64.StdEncoding.EncodeToString(finalBytes)

	result := &ImageTransformResult{
		MimeType:     finalMimeType,
		InputFormat:  inputFormat,
		OutputFormat: options.Output,
		OriginalSize: loaded.Size,
		StoredSize:   int64(len(finalBytes)),
	}

	switch options.Output {
	case "base64":
		result.Value = finalBase64
	case "data_url":
		result.Value = fmt.Sprintf("data:%s;base64,%s", finalMimeType, finalBase64)
	case "url":
		if options.Storage != "cos" {
			return nil, fmt.Errorf("output=url requires storage=cos")
		}
		if options.DryRun {
			result.Value, err = PreviewBytesToCOS(finalBytes, finalMimeType, "")
		} else {
			result.Value, err = UploadBytesToCOS(ctx, finalBytes, finalMimeType, "")
		}
		if err != nil {
			return nil, err
		}
	default:
		return nil, fmt.Errorf("unsupported output format: %s", options.Output)
	}

	return result, nil
}

func ProcessImageResponseAutoStore(ctx context.Context, response *dto.ImageResponse) (*dto.ImageResponse, error) {
	if response == nil {
		return nil, nil
	}

	cosSetting := system_setting.GetCOSSetting()
	mode := strings.ToLower(strings.TrimSpace(cosSetting.ImageAutoStoreMode))
	if !cosSetting.Enabled || mode == "" || mode == system_setting.ImageAutoStoreModeOff {
		return response, nil
	}

	processed := &dto.ImageResponse{
		Created:  response.Created,
		Metadata: response.Metadata,
		Data:     make([]dto.ImageData, 0, len(response.Data)),
	}

	for _, item := range response.Data {
		nextItem := item
		var err error

		switch {
		case item.B64Json != "" && (mode == system_setting.ImageAutoStoreModeB64Only || mode == system_setting.ImageAutoStoreModeURLAndB64):
			var result *ImageTransformResult
			result, err = TransformImageValue(ctx, item.B64Json, ImagePipelineOptions{
				Input:    "base64",
				Output:   "url",
				Storage:  "cos",
				MimeType: "keep",
			})
			if err == nil {
				nextItem.Url = result.Value
				nextItem.B64Json = ""
			}
		case item.Url != "" && mode == system_setting.ImageAutoStoreModeURLAndB64:
			var result *ImageTransformResult
			result, err = TransformImageValue(ctx, item.Url, ImagePipelineOptions{
				Input:    "url",
				Output:   "url",
				Storage:  "cos",
				MimeType: "keep",
			})
			if err == nil {
				nextItem.Url = result.Value
			}
		}

		if err != nil {
			if cosSetting.ImageAutoStoreStrict {
				return nil, err
			}
			nextItem = item
		}

		processed.Data = append(processed.Data, nextItem)
	}
	return processed, nil
}
