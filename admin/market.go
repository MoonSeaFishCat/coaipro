package admin

import (
	"chat/globals"
	"fmt"

	"github.com/spf13/viper"
)

type ModelTag []string
type MarketModel struct {
	Id               string   `json:"id" mapstructure:"id" required:"true"`
	Name             string   `json:"name" mapstructure:"name" required:"true"`
	Description      string   `json:"description" mapstructure:"description"`
	Free             bool     `json:"free" mapstructure:"free"`
	Auth             bool     `json:"auth" mapstructure:"auth"`
	Default          bool     `json:"default" mapstructure:"default"`
	HighContext      bool     `json:"high_context" mapstructure:"highcontext"`
	FunctionCalling  bool     `json:"function_calling" mapstructure:"functioncalling"`
	VisionModel      bool     `json:"vision_model" mapstructure:"visionmodel"`
	ThinkingModel    bool     `json:"thinking_model" mapstructure:"thinkingmodel"`
	AllowUserThink   bool     `json:"allow_user_think" mapstructure:"allowuserthink"`
	OCRModel         bool     `json:"ocr_model" mapstructure:"ocrmodel"`
	ReverseModel     bool     `json:"reverse_model" mapstructure:"reversemodel"`
	ImageGeneration  bool     `json:"image_generation" mapstructure:"imagegeneration"`
	Avatar           string   `json:"avatar" mapstructure:"avatar"`
	Tag              ModelTag `json:"tag" mapstructure:"tag"`
}
type MarketModelList []MarketModel

type Market struct {
	Models MarketModelList `json:"models" mapstructure:"models"`
}

func NewMarket() *Market {
	var models MarketModelList
	if err := viper.UnmarshalKey("market", &models); err != nil {
		globals.Warn(fmt.Sprintf("[market] read config error: %s, use default config", err.Error()))
		models = MarketModelList{}
	}

	return &Market{
		Models: models,
	}
}

func (m *Market) GetModels() MarketModelList {
	return m.Models
}

func (m *Market) GetModel(id string) *MarketModel {
	for _, model := range m.Models {
		if model.Id == id {
			return &model
		}
	}
	return nil
}

func (m *Market) VisionModelIDs() []string {
	var result []string
	for _, model := range m.Models {
		if model.VisionModel && len(model.Id) > 0 {
			result = append(result, model.Id)
		}
	}
	return result
}

func (m *Market) ThinkingConfigs() map[string]globals.ThinkingConfig {
	result := make(map[string]globals.ThinkingConfig)
	for _, model := range m.Models {
		if !model.ThinkingModel || len(model.Id) == 0 {
			continue
		}

		result[model.Id] = globals.ThinkingConfig{
			Enabled:          true,
			AllowUserControl: model.AllowUserThink,
		}
	}
	return result
}

func (m *Market) ImageGenerationModelIDs() []string {
	var result []string
	for _, model := range m.Models {
		if model.ImageGeneration && len(model.Id) > 0 {
			result = append(result, model.Id)
		}
	}
	return result
}

// SyncFromChannels syncs models from channel configuration
// It adds new models from channels that don't exist in market
// Existing models in market are preserved and not overwritten
func (m *Market) SyncFromChannels(channels interface{}) error {
	// channels should be a slice of channel.Channel
	// We'll use reflection to extract model IDs
	channelModels := make(map[string]bool)

	// Try to extract models from channels
	if channelSeq, ok := channels.([]interface{}); ok {
		for _, ch := range channelSeq {
			if chMap, ok := ch.(map[string]interface{}); ok {
				if models, ok := chMap["models"].([]interface{}); ok {
					for _, model := range models {
						if modelStr, ok := model.(string); ok {
							channelModels[modelStr] = true
						}
					}
				}
			}
		}
	}

	// Add new models from channels that don't exist in market
	existingIds := make(map[string]bool)
	for _, model := range m.Models {
		existingIds[model.Id] = true
	}

	for modelId := range channelModels {
		if !existingIds[modelId] {
			// Create a new market model with basic info
			newModel := MarketModel{
				Id:   modelId,
				Name: modelId, // Use model ID as default name
			}
			m.Models = append(m.Models, newModel)
		}
	}

	return nil
}

func (m *Market) SaveConfig() error {
	viper.Set("market", m.Models)
	return viper.WriteConfig()
}

func (m *Market) SetModels(models MarketModelList) error {
	m.Models = models
	if err := m.SaveConfig(); err != nil {
		return err
	}

	globals.SetVisionModels(m.VisionModelIDs())
	globals.SetThinkingConfigs(m.ThinkingConfigs())
	return nil
}
