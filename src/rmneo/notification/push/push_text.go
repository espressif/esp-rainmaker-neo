// SPDX-FileCopyrightText: 2024-2026 Espressif Systems (Shanghai) CO LTD
//
// SPDX-License-Identifier: Apache-2.0

package push

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/espressif/esp-rainmaker-neo/src/rmneo/file"
	"github.com/espressif/esp-rainmaker-neo/src/utils/rlog"
)

// The reason to create a separate package for this is so that the init() call happens not for any users of 'notificaiton' package
// but only for the users of 'push' package.

// PushTextForEvent is the text message that the device sends on an event
type PushTextForEvent struct {
	// Title is the title of the push message
	Title string `json:"title,omitempty"`
	// Text is the text of the push message
	Text string `json:"text,omitempty"`
	// Priority is the priority of the push message: low, normal, high
	Priority string `json:"priority,omitempty"`
}

// PushTextLocale is the locale specific settings for the push text configuration
type PushTextLocale struct {
	// Title is the common title if one is not customized by the event
	Title string `json:"title,omitempty"`
	// Event are the various events that are supported by the push text configuration
	Event map[string]*PushTextForEvent `json:"event,omitempty"`
}

// PushTextConfig is the configuration for the push messages, this can be customized per deployment
type PushTextConfig struct {
	// Default (locale independent) configuration for the push text configuration
	Default PushTextLocale `json:"default,omitempty"`
	// Locale are the locale specific settings for the push text configuration
	Locale map[string]*PushTextLocale `json:"locale,omitempty"`
}

// GPushTextConfig is the global push text configuration
// Exported for testing purposes
var GPushTextConfig PushTextConfig

const PushTextConfigKey = "push_text_config.json"

// ResetToDefaults resets the global configuration to default values
// Exported for testing purposes
func LoadPushTextConfigFromDefaults(c *PushTextConfig) {
	*c = PushTextConfig{
		Default: PushTextLocale{
			Title: "ESP RainMaker",
			Event: map[string]*PushTextForEvent{
				"node_alert": {
					Title:    "Node Alert",
					Text:     "Node {nodeID} has an alert!",
					Priority: "high",
				},
				"test": {
					Text: "Test Message",
				},
			},
		},
	}
}

func init() {
	DoInit()
}

// DoInit initialises the push text configuration
// Exported for testing purposes
func DoInit() {
	// Initialise the configuration with defaults
	LoadPushTextConfigFromDefaults(&GPushTextConfig)

	// Load any customisations from S3 on top of the defaults
	if err := LoadPushTextConfigFromS3(&GPushTextConfig); err != nil {
		rlog.Warn(nil).Err(err).Msg("Failed to load push text configuration from S3, using default configuration")
	}
}

// LoadPushTextConfigFromS3 fetches the push text configuration from S3 and updates gPushTextConfig
// Exported for testing purposes
func LoadPushTextConfigFromS3(c *PushTextConfig) error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	f, err := file.NewSystemFile(PushTextConfigKey)
	if err != nil {
		return err
	}
	content, err := f.ReadContent(ctx)
	if err != nil {
		return err
	}

	var tmp PushTextConfig
	// Unmarshal directly into the global configuration
	// This will preserve existing defaults for missing fields
	if err := json.Unmarshal(content, &tmp); err != nil {
		return err
	}
	c.Locale = tmp.Locale

	// Only overwrite the parts that come from the update
	if tmp.Default.Title != "" {
		c.Default.Title = tmp.Default.Title
	}
	for k, v := range tmp.Default.Event {
		if c.Default.Event[k] == nil {
			c.Default.Event[k] = v
		} else {
			if v.Title != "" {
				c.Default.Event[k].Title = v.Title
			}
			if v.Text != "" {
				c.Default.Event[k].Text = v.Text
			}
			if v.Priority != "" {
				c.Default.Event[k].Priority = v.Priority
			}
		}
	}

	rlog.Info(nil).Msg("Successfully loaded push text configuration from S3:")
	return nil
}

// GetPushTextConfig returns a copy of the current push text configuration
// Exported for testing purposes
func GetPushTextConfig() PushTextConfig {
	return GPushTextConfig
}

// PushMessageWithEvent is the push message for an event that is being sent to the mobile push service
type PushMessageWithEvent struct {
	// Name is the name of the event
	Name string
	// Data is the data of the event
	Data map[string]string
	// PushMessage is the push message to be sent
	PushMessage *PushMessage
}

func NewPushMessageWithEvent(name string, data map[string]string) PushMessageWithEvent {
	return PushMessageWithEvent{
		Name: name,
		Data: data,
		PushMessage: &PushMessage{
			PushTextForEvent: PushTextForEvent{
				Title: "",
				Text:  "",
			},
		},
	}
}

// LoadMessage returns a push message for the given event and data
// We could probably populate other fields of the PushMessage here, but we don't do that for now
func (p *PushMessageWithEvent) LoadMessage(locale string) {
	// Default Text for the event must exist
	pushText := *GPushTextConfig.Default.Event[p.Name]
	if pushText.Title == "" {
		// At times events may not define their own title, assign the default title
		pushText.Title = GPushTextConfig.Default.Title
	}

	// Locale Overrides - we only look for title and text as overrides
	if locale != "" && GPushTextConfig.Locale[locale] != nil {
		// Override the default title with the locale specific title if it exists
		if GPushTextConfig.Locale[locale].Title != "" {
			pushText.Title = GPushTextConfig.Locale[locale].Title
		}
		// Override the push text with the locale specific push text if it exists
		if GPushTextConfig.Locale[locale].Event[p.Name] != nil {
			if GPushTextConfig.Locale[locale].Event[p.Name].Title != "" {
				pushText.Title = GPushTextConfig.Locale[locale].Event[p.Name].Title
			}
			if GPushTextConfig.Locale[locale].Event[p.Name].Text != "" {
				pushText.Text = GPushTextConfig.Locale[locale].Event[p.Name].Text
			}
		}
	}

	// Substitute the variables in the push text
	for key, value := range p.Data {
		pushText.Text = strings.ReplaceAll(pushText.Text, "{"+key+"}", value)
	}

	p.PushMessage.PushTextForEvent = pushText
}
