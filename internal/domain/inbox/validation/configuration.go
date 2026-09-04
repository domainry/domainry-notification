package validation

import (
	"fmt"
	"strings"
)

// Configuration contains the immutable host-composed Connector channel
// vocabulary needed to validate inbox contracts.
type Configuration struct {
	externalChannels map[string]bool
}

func NewConfiguration(externalChannels []string) (*Configuration, error) {
	configuration := &Configuration{externalChannels: map[string]bool{}}
	for _, channel := range externalChannels {
		channel = strings.TrimSpace(channel)
		if channel == "" || channel == "in_app" || configuration.externalChannels[channel] {
			return nil, fmt.Errorf("notification external channel is invalid or duplicated")
		}
		configuration.externalChannels[channel] = true
	}
	return configuration, nil
}

func (c *Configuration) SupportsExternalChannel(channel string) bool {
	return c != nil && c.externalChannels[strings.TrimSpace(channel)]
}
