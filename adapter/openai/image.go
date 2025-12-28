package openai

import (
	adaptercommon "chat/adapter/common"
	"chat/globals"
	"chat/utils"
	"fmt"
	"strings"
)

type ImageProps struct {
	Model     string
	Prompt    string
	Size      ImageSize
	N         int
	Type      string
	Watermark bool
	Proxy     globals.ProxyConfig
}

func (c *ChatInstance) GetImageEndpoint() string {
	return fmt.Sprintf("%s/v1/images/generations", c.GetEndpoint())
}

// CreateImageRequest will create a dalle image from prompt, return url of image, base64 data and error
func (c *ChatInstance) CreateImageRequest(props ImageProps) ([]string, []string, error) {
	if props.N <= 0 {
		props.N = 1
	}

	res, err := utils.Post(
		c.GetImageEndpoint(),
		c.GetHeader(), ImageRequest{
			Model:     props.Model,
			Prompt:    props.Prompt,
			Size:      props.Size,
			N:         props.N,
			Type:      props.Type,
			Watermark: props.Watermark,
		}, props.Proxy)
	if err != nil || res == nil {
		return nil, nil, fmt.Errorf(err.Error())
	}

	data := utils.MapToStruct[ImageResponse](res)
	if data == nil {
		return nil, nil, fmt.Errorf("openai error: cannot parse response")
	} else if data.Error.Message != "" {
		return nil, nil, fmt.Errorf(data.Error.Message)
	}

	var urls []string
	var b64s []string

	for _, item := range data.Data {
		urls = append(urls, item.Url)
		b64s = append(b64s, item.B64Json)
	}

	return urls, b64s, nil
}

// CreateImage will create a dalle image from prompt, return markdown of image
func (c *ChatInstance) CreateImage(props *adaptercommon.ChatProps) (string, error) {
	urls, b64Jsons, err := c.CreateImageRequest(ImageProps{
		Model:  props.Model,
		Prompt: c.GetLatestPrompt(props),
		Proxy:  props.Proxy,
		Size:   ImageSize1024,
		N:      1,
	})
	if err != nil {
		if strings.Contains(err.Error(), "safety") {
			return err.Error(), nil
		}
		return "", err
	}

	if len(b64Jsons) > 0 && b64Jsons[0] != "" {
		return utils.GetBase64ImageMarkdown(b64Jsons[0]), nil
	}

	if len(urls) > 0 && urls[0] != "" {
		storedUrl := utils.StoreImage(urls[0])
		return utils.GetImageMarkdown(storedUrl), nil
	}

	return "", fmt.Errorf("no image generated")
}
